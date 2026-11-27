package pages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// `bts pages setup` wires the pages into this machine: the user-level
// Artifact guard in ~/.claude/settings.json, the tailnet mapping, and the
// LaunchAgent.  Each step is idempotent, and teardown undoes it.

// GuardCommand is the user-level PreToolUse hook's command.  A machine
// without bts on its PATH runs it as a no-op.
const GuardCommand = "command -v bts >/dev/null 2>&1 && bts hook artifact-guard || true"

const guardMarker = "bts hook artifact-guard"

// ClaudeSettingsPath is the user's Claude Code settings file.
func ClaudeSettingsPath() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude", "settings.json"), nil
}

// orderedObject is a JSON object read with its keys in file order, values
// left as written, so editing one key rewrites nothing else.
type orderedObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseOrdered(data []byte) (*orderedObject, error) {
	o := &orderedObject{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if _, dup := o.vals[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = v
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON object")
	}
	return o, nil
}

func (o *orderedObject) set(k string, v json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *orderedObject) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// marshalPlain is json.Marshal without HTML escaping: a shell command's
// "<", ">" and "&" stay as written instead of becoming < and the like.
func marshalPlain(v interface{}) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// rawArray joins values into a JSON array as written; json.Marshal would
// HTML-escape each one.
func rawArray(items []json.RawMessage) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := json.Compact(&b, it); err != nil {
			return nil, err
		}
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

func (o *orderedObject) marshal() (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := marshalPlain(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		var c bytes.Buffer
		if err := json.Compact(&c, o.vals[k]); err != nil {
			return nil, err
		}
		b.Write(c.Bytes())
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func guardEntry() json.RawMessage {
	e := map[string]interface{}{
		"matcher": "Artifact",
		"hooks": []interface{}{map[string]interface{}{
			"type": "command", "command": GuardCommand, "timeout": 5,
		}},
	}
	b, _ := marshalPlain(e)
	return b
}

func isGuard(entry json.RawMessage) bool {
	return bytes.Contains(entry, []byte(guardMarker))
}

// editGuard adds (on) or removes (!on) the guard entry in a settings
// document and reports whether anything changed.  Everything else stays as
// written, in its order.
func editGuard(data []byte, on bool) ([]byte, bool, error) {
	top, err := parseOrdered(data)
	if err != nil {
		return nil, false, err
	}
	hooks := &orderedObject{vals: map[string]json.RawMessage{}}
	if raw, ok := top.vals["hooks"]; ok {
		if hooks, err = parseOrdered(raw); err != nil {
			return nil, false, fmt.Errorf("hooks: %w", err)
		}
	}
	var entries []json.RawMessage
	if raw, ok := hooks.vals["PreToolUse"]; ok {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, false, fmt.Errorf("hooks.PreToolUse: %w", err)
		}
	}
	kept := entries[:0:0]
	present := false
	for _, e := range entries {
		if isGuard(e) {
			present = true
			if !on {
				continue
			}
		}
		kept = append(kept, e)
	}
	if on == present {
		return data, false, nil
	}
	if on {
		kept = append(kept, guardEntry())
	}
	if len(kept) == 0 {
		hooks.del("PreToolUse")
	} else {
		arr, err := rawArray(kept)
		if err != nil {
			return nil, false, err
		}
		hooks.set("PreToolUse", arr)
	}
	hb, err := hooks.marshal()
	if err != nil {
		return nil, false, err
	}
	top.set("hooks", hb)
	compact, err := top.marshal()
	if err != nil {
		return nil, false, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil, false, err
	}
	out.WriteByte('\n')
	return out.Bytes(), true, nil
}

// GuardInstalled says whether the settings file carries the guard.
func GuardInstalled(settingsPath string) bool {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(guardMarker))
}

// SetGuard adds or removes the guard in the settings file, keeping a copy
// of the file as it was beside it.  It reports whether the file changed.
func SetGuard(settingsPath string, on bool) (bool, error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	out, changed, err := editGuard(data, on)
	if err != nil {
		return false, fmt.Errorf("%s: %w", settingsPath, err)
	}
	if !changed {
		return false, nil
	}
	mode := os.FileMode(0644)
	if fi, err := os.Stat(settingsPath); err == nil {
		mode = fi.Mode().Perm()
		backup := settingsPath + ".bts-backup-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.WriteFile(backup, data, mode); err != nil {
			return false, err
		}
	}
	return true, writeFileAtomic(settingsPath, out, mode)
}

// Tailscale

// tailscaleBin finds the tailscale CLI.
func tailscaleBin() (string, error) {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p, nil
	}
	const app = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if _, err := os.Stat(app); err == nil {
		return app, nil
	}
	return "", errors.New("tailscale is not installed")
}

// TailnetName is this machine's MagicDNS name, without the trailing dot.
func TailnetName() (string, error) {
	bin, err := tailscaleBin()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(bin, "status", "--self", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale status: %w", err)
	}
	var st struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(st.Self.DNSName, ".")
	if name == "" {
		return "", errors.New("this machine has no tailnet name (MagicDNS off, or tailscale logged out)")
	}
	return name, nil
}

// serveStatus is the part of `tailscale serve status --json` bts reads.
type serveStatus struct {
	TCP map[string]json.RawMessage `json:"TCP"`
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy string `json:"Proxy"`
		} `json:"Handlers"`
	} `json:"Web"`
}

func readServeStatus(bin string) (*serveStatus, error) {
	out, err := exec.Command(bin, "serve", "status", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale serve status: %w", err)
	}
	var st serveStatus
	if len(bytes.TrimSpace(out)) == 0 {
		return &st, nil
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// ServeMapping says what the tailnet's HTTPS port maps to: "" when nothing,
// else the proxy target of its root.
func ServeMapping(port int) (string, error) {
	bin, err := tailscaleBin()
	if err != nil {
		return "", err
	}
	st, err := readServeStatus(bin)
	if err != nil {
		return "", err
	}
	p := strconv.Itoa(port)
	if _, ok := st.TCP[p]; !ok {
		return "", nil
	}
	for hostport, web := range st.Web {
		if strings.HasSuffix(hostport, ":"+p) {
			if h, ok := web.Handlers["/"]; ok {
				return h.Proxy, nil
			}
			return "(handlers other than /)", nil
		}
	}
	return "(a TCP forward)", nil
}

// LocalTarget is what the tailnet port forwards to.
func LocalTarget(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// EnableServe maps the tailnet's HTTPS port to the local server with
// `tailscale serve --bg`, which persists across reboots.  It refuses a port
// that already maps elsewhere.  It reports whether it changed anything.
func EnableServe(port int) (bool, error) {
	cur, err := ServeMapping(port)
	if err != nil {
		return false, err
	}
	want := LocalTarget(port)
	if cur == want {
		return false, nil
	}
	if cur != "" {
		return false, fmt.Errorf("the tailnet's HTTPS port %d already maps to %s; set another \"port\" in the pages config", port, cur)
	}
	bin, _ := tailscaleBin()
	out, err := exec.Command(bin, "serve", "--bg", "--https="+strconv.Itoa(port), want).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("tailscale serve: %v: %s", err, bytes.TrimSpace(out))
	}
	return true, nil
}

// DisableServe removes the mapping when it is bts's own.  It reports
// whether it removed one.
func DisableServe(port int) (bool, error) {
	cur, err := ServeMapping(port)
	if err != nil {
		return false, err
	}
	if cur != LocalTarget(port) {
		return false, nil
	}
	bin, _ := tailscaleBin()
	out, err := exec.Command(bin, "serve", "--https="+strconv.Itoa(port), "off").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("tailscale serve off: %v: %s", err, bytes.TrimSpace(out))
	}
	return true, nil
}

// PublicURLFor is the tailnet origin for a name and port.
func PublicURLFor(name string, port int) string {
	if port == 443 {
		return "https://" + name
	}
	return fmt.Sprintf("https://%s:%d", name, port)
}
