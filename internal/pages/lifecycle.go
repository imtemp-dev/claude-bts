package pages

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// The server runs under a LaunchAgent when `bts pages setup` installed one
// (D2: a link opens on the phone whenever the Mac is awake); otherwise the
// first command that needs it starts it in the background, and it exits
// after twelve idle hours.

// AgentLabel is the LaunchAgent's label.
const AgentLabel = "dev.imtemp.bts.pages"

// AgentPlistPath is where the LaunchAgent's plist lives.
func AgentPlistPath() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "Library", "LaunchAgents", AgentLabel+".plist"), nil
}

func launchdDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// AgentInstalled says whether the LaunchAgent's plist is in place.
func AgentInstalled() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	p, err := AgentPlistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// AgentLoaded says whether launchd has the agent loaded.
func AgentLoaded() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return exec.Command("launchctl", "print", launchdDomain()+"/"+AgentLabel).Run() == nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DefaultHome is ~/.bts/pages, the home the LaunchAgent serves.
func DefaultHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".bts", "pages")
}

// agentServes says whether the LaunchAgent runs the server for home: it
// serves the default home only, so a development build or a test with its
// own BTS_PAGES_HOME never kicks it.
func agentServes(home string) bool {
	return home == DefaultHome() && AgentLoaded()
}

// AgentPlist is the LaunchAgent that keeps `bts pages serve --launchd`
// running: started at login, restarted when it exits.
func AgentPlist(exe, home string) []byte {
	log := xmlEscape(filepath.Join(home, "server.log"))
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + AgentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(exe) + `</string>
		<string>pages</string>
		<string>serve</string>
		<string>--launchd</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>` + log + `</string>
	<key>StandardErrorPath</key>
	<string>` + log + `</string>
</dict>
</plist>
`)
}

// InstallAgent writes the LaunchAgent for exe and loads it, replacing one
// already loaded.
func InstallAgent(exe, home string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("a LaunchAgent is macOS only; elsewhere the server starts on demand")
	}
	if home != DefaultHome() {
		return fmt.Errorf("the LaunchAgent serves %s only; unset %s to install it", DefaultHome(), HomeEnv)
	}
	p, err := AgentPlistPath()
	if err != nil {
		return err
	}
	if AgentLoaded() {
		_ = exec.Command("launchctl", "bootout", launchdDomain()+"/"+AgentLabel).Run()
	}
	if err := writeFileAtomic(p, AgentPlist(exe, home), 0644); err != nil {
		return err
	}
	if out, err := exec.Command("launchctl", "bootstrap", launchdDomain(), p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// UninstallAgent unloads the LaunchAgent and removes its plist.  It
// reports whether there was one.
func UninstallAgent() (bool, error) {
	p, err := AgentPlistPath()
	if err != nil {
		return false, err
	}
	had := AgentInstalled()
	if AgentLoaded() {
		if out, err := exec.Command("launchctl", "bootout", launchdDomain()+"/"+AgentLabel).CombinedOutput(); err != nil {
			return had, fmt.Errorf("launchctl bootout: %v: %s", err, bytes.TrimSpace(out))
		}
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return had, err
	}
	return had, nil
}

// kickstart has launchd start the agent's server, killing a running one
// first when kill is set.
func kickstart(kill bool) error {
	args := []string{"kickstart"}
	if kill {
		args = append(args, "-k")
	}
	args = append(args, launchdDomain()+"/"+AgentLabel)
	if out, err := exec.Command("launchctl", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// EnsureOptions configure Ensure.
type EnsureOptions struct {
	Home    string
	Version string
	// Exe is this bts binary; only the binary a running server was started
	// from (or the LaunchAgent runs) replaces it with another version.
	Exe string
}

// Ensure returns the running page server, starting it (or having launchd
// start it) when none answers, and replacing it when it runs another
// version of this same binary.  notice says what it did not do and why.
func Ensure(o EnsureOptions) (info *ServerInfo, notice string, err error) {
	// One command at a time decides: two sessions publishing at once would
	// otherwise both start a server, and the one that loses the port would
	// wait for a server that never comes.
	unlock, err := lockFile(o.Home, ".ensure.lock")
	if err != nil {
		return nil, "", err
	}
	defer unlock()
	token, err := Token(o.Home)
	if err != nil {
		return nil, "", err
	}
	info, err = ReadServerInfo(o.Home)
	if err != nil {
		return nil, "", err
	}
	if info.Healthy(token) {
		if info.Version == o.Version {
			return info, "", nil
		}
		if !sameFile(info.Exe, o.Exe) {
			return info, fmt.Sprintf("the page server runs bts %s from %s; this is bts %s from %s, which leaves it running", info.Version, info.Exe, o.Version, o.Exe), nil
		}
		if info.Managed == "launchd" && agentServes(o.Home) {
			if err := kickstart(true); err != nil {
				return nil, "", err
			}
			return waitFor(o.Home, token, func(i *ServerInfo) bool { return i.PID != info.PID })
		}
		_ = syscall.Kill(info.PID, syscall.SIGTERM)
		for i := 0; i < 50 && pidAlive(info.PID); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if agentServes(o.Home) {
		if err := kickstart(false); err != nil {
			return nil, "", err
		}
		return waitFor(o.Home, token, nil)
	}
	logPath := filepath.Join(o.Home, "server.log")
	if err := os.MkdirAll(o.Home, 0755); err != nil {
		return nil, "", err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, "", err
	}
	defer logf.Close()
	cmd := exec.Command(o.Exe, "pages", "serve")
	cmd.Stdout, cmd.Stderr = logf, logf
	// Its own session: the server outlives the command that started it
	// and the terminal's job control.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	_ = cmd.Process.Release()
	info, notice, err = waitFor(o.Home, token, nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w; see %s", err, logPath)
	}
	return info, notice, nil
}

func waitFor(home, token string, ok func(*ServerInfo) bool) (*ServerInfo, string, error) {
	for i := 0; i < 150; i++ {
		time.Sleep(50 * time.Millisecond)
		if info, _ := ReadServerInfo(home); info != nil && (ok == nil || ok(info)) && info.Healthy(token) {
			return info, "", nil
		}
	}
	return nil, "", errors.New("the page server did not start")
}

// Stop stops the running server.  Under a LaunchAgent launchd starts it
// again; `bts pages teardown` removes the agent first.  It reports whether
// one was running.
func Stop(home string) (bool, error) {
	info, err := ReadServerInfo(home)
	if err != nil || info == nil {
		return false, err
	}
	token, err := Token(home)
	if err != nil {
		return false, err
	}
	running := info.Healthy(token)
	if running {
		if err := syscall.Kill(info.PID, syscall.SIGTERM); err != nil {
			return false, err
		}
		for i := 0; i < 50 && pidAlive(info.PID); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Remove(serverInfoPath(home))
	return running, nil
}
