// Package pages is the user's page server and page store: every page a
// Claude session makes on this machine, a recipe's or feature's review
// page as well as a general page (an architecture explainer, a PR brief),
// served by bts from 127.0.0.1 on one fixed port, tied to no claude.ai
// account, and reachable from the owner's other devices through
// `tailscale serve` (docs/research/pages-everywhere-design.md).
//
// State lives in Home(): config.json (port, public URL), token, server.json
// (the running server), projects.json (the projects whose review pages it
// serves), and one directory per general page.
package pages

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// HomeEnv overrides where the pages state lives, as BTS_FEATURE_STORE does
// for features: a development build or a test uses its own.
const HomeEnv = "BTS_PAGES_HOME"

// DefaultPort is the port the server listens on unless config.json says
// otherwise.  One fixed port is what `tailscale serve` points at.
const DefaultPort = 7337

// Home is the pages state directory: $BTS_PAGES_HOME, else ~/.bts/pages.
func Home() (string, error) {
	if d := strings.TrimSpace(os.Getenv(HomeEnv)); d != "" {
		return filepath.Abs(d)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no home directory for the page store; set %s", HomeEnv)
	}
	return filepath.Join(h, ".bts", "pages"), nil
}

// Config is config.json.
type Config struct {
	// Port is the local port; 0 means DefaultPort.
	Port int `json:"port,omitempty"`
	// PublicURL is the tailnet origin `tailscale serve` forwards to the
	// server (https://<machine>.<tailnet>.ts.net:<port>); "" when the
	// pages are reachable from this machine only.
	PublicURL string `json:"public_url,omitempty"`
}

// ListenPort is the port the server uses.
func (c Config) ListenPort() int {
	if c.Port > 0 {
		return c.Port
	}
	return DefaultPort
}

// PublicHost is the host (with its port, when not 443) of PublicURL; "" if
// none is set.
func (c Config) PublicHost() string {
	if c.PublicURL == "" {
		return ""
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func configPath(home string) string { return filepath.Join(home, "config.json") }

// LoadConfig reads config.json; the zero Config when there is none.
func LoadConfig(home string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(configPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("%s: %w", configPath(home), err)
	}
	return c, nil
}

// SaveConfig writes config.json.
func SaveConfig(home string, c Config) error {
	return writeJSON(configPath(home), c, 0644)
}

func tokenPath(home string) string { return filepath.Join(home, "token") }

// Token returns the server's token, generating it on first use.  It is kept
// across restarts, so a link already on a phone keeps working until
// RotateToken replaces it.
func Token(home string) (string, error) {
	raw, err := os.ReadFile(tokenPath(home))
	if err == nil {
		if t := strings.TrimSpace(string(raw)); len(t) >= 32 {
			return t, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return RotateToken(home)
}

// RotateToken replaces the token: every link printed before stops working,
// and every browser has to open a newly printed one.
func RotateToken(home string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.MkdirAll(home, 0755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(tokenPath(home), []byte(t+"\n"), 0600); err != nil {
		return "", err
	}
	return t, nil
}

// LocalBase is the server's origin on this machine.
func (c Config) LocalBase() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.ListenPort())
}

// Links are a path's links: on this machine, and on the tailnet when it is
// set up.  The token goes on the links a person opens, never on what bts
// records.
type Links struct {
	Local  string // with the token
	Public string // with the token; "" without a tailnet
	Record string // the local link without the token, for records
	Path   string
}

// LinksFor builds the links to path (which starts with "/").
func LinksFor(c Config, token, path string) Links {
	q := "?t=" + url.QueryEscape(token)
	l := Links{Local: c.LocalBase() + path + q, Record: c.LocalBase() + path, Path: path}
	if c.PublicURL != "" {
		l.Public = strings.TrimRight(c.PublicURL, "/") + path + q
	}
	return l
}

func writeJSON(path string, v interface{}, mode os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), mode)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func readJSON(path string, v interface{}) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
