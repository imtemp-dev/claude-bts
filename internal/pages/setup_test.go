package pages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A settings file as Claude Code writes it: two-space indent, keys in the
// user's order, hooks of other tools already there.
const sampleSettings = `{
  "model": "opus",
  "permissions": {
    "allow": [
      "Bash(git status:*)"
    ],
    "deny": []
  },
  "hooks": {
    "SessionStart": [
      {
        "matcher": "^(startup|resume)$",
        "hooks": [
          {
            "type": "command",
            "command": "bash '/x/herdr.sh' session \"quoted\" \u0026 more",
            "timeout": 10
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/x/guard.sh 2>/dev/null && <true>"
          }
        ]
      }
    ]
  },
  "statusLine": {
    "type": "command",
    "command": "/x/status.sh"
  }
}
`

func TestEditGuard_AddsOnceKeepsEverythingElseAndRemovesCleanly(t *testing.T) {
	out, changed, err := editGuard([]byte(sampleSettings), true)
	if err != nil || !changed {
		t.Fatalf("add: %v, changed %v", err, changed)
	}
	s := string(out)
	if !strings.Contains(s, `"matcher": "Artifact"`) || !strings.Contains(s, "bts hook artifact-guard") {
		t.Fatalf("guard not added:\n%s", s)
	}
	// Order kept: model, permissions, hooks, statusLine; and the Bash
	// guard stays ahead of ours.
	idx := func(sub string) int { return strings.Index(s, sub) }
	if idx(`"model"`) >= idx(`"permissions"`) || idx(`"permissions"`) >= idx(`"hooks"`) || idx(`"hooks"`) >= idx(`"statusLine"`) {
		t.Errorf("key order changed:\n%s", s)
	}
	if idx(`"matcher": "Bash"`) > idx(`"matcher": "Artifact"`) {
		t.Error("the guard went ahead of an existing entry")
	}
	// The other tool's command is byte for byte the same, escapes included.
	if !strings.Contains(s, `"command": "bash '/x/herdr.sh' session \"quoted\" \u0026 more"`) {
		t.Errorf("an existing value was re-encoded:\n%s", s)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	again, changed, err := editGuard(out, true)
	if err != nil || changed || string(again) != string(out) {
		t.Errorf("a second add changed the file: %v", err)
	}
	back, changed, err := editGuard(out, false)
	if err != nil || !changed {
		t.Fatalf("remove: %v, changed %v", err, changed)
	}
	if string(back) != sampleSettings {
		t.Errorf("add then remove did not restore the file:\n%s", back)
	}
}

func TestEditGuard_EmptyOrMissingFile(t *testing.T) {
	out, changed, err := editGuard(nil, true)
	if err != nil || !changed {
		t.Fatalf("%v, %v", err, changed)
	}
	var v struct {
		Hooks map[string][]map[string]interface{} `json:"hooks"`
	}
	if err := json.Unmarshal(out, &v); err != nil || len(v.Hooks["PreToolUse"]) != 1 {
		t.Errorf("settings from nothing: %s, %v", out, err)
	}
	if _, _, err := editGuard([]byte("[1,2]"), true); err == nil {
		t.Error("a settings file that is not an object was edited")
	}
	back, _, _ := editGuard(out, false)
	if strings.Contains(string(back), "PreToolUse") {
		t.Errorf("an emptied PreToolUse stayed: %s", back)
	}
}

func TestSetGuard_KeepsACopy(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "settings.json")
	writeFile(t, sp, sampleSettings)
	if err := os.Chmod(sp, 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := SetGuard(sp, true)
	if err != nil || !changed || !GuardInstalled(sp) {
		t.Fatalf("set: %v, %v", changed, err)
	}
	backups, _ := filepath.Glob(sp + ".bts-backup-*")
	if len(backups) != 1 || readFile(t, backups[0]) != sampleSettings {
		t.Errorf("backups %v", backups)
	}
	if fi, _ := os.Stat(sp); fi.Mode().Perm() != 0600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	if changed, _ := SetGuard(sp, true); changed {
		t.Error("a second set changed the file")
	}
}

func TestPublicURLFor(t *testing.T) {
	if got := PublicURLFor("mac.ts.net", 443); got != "https://mac.ts.net" {
		t.Error(got)
	}
	if got := PublicURLFor("mac.ts.net", 7337); got != "https://mac.ts.net:7337" {
		t.Error(got)
	}
}

func TestAgentPlist_RunsServeUnderLaunchd(t *testing.T) {
	p := string(AgentPlist("/Users/x/.local/bin/bts", "/Users/x/.bts/pages"))
	for _, want := range []string{"<string>" + AgentLabel + "</string>", "<string>/Users/x/.local/bin/bts</string>", "<string>--launchd</string>", "<key>KeepAlive</key>\n\t<true/>", "<key>RunAtLoad</key>\n\t<true/>", "/Users/x/.bts/pages/server.log"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q:\n%s", want, p)
		}
	}
}
