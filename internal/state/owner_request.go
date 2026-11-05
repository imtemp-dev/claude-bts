package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// OwnerRequest is the trace a real `bts recipe create` or `bts recipe claim`
// run leaves for the PostToolUse hook, which alone sees the Claude Code
// session id.  The hook binds a session to a recipe only when a request is
// waiting for it, so a Bash call that merely contains the command's text (a
// heredoc writing a report, a quoted note) cannot move ownership: it ran no
// bts, so it left no request (trial problem #59).
type OwnerRequest struct {
	Action string `json:"action"` // "create" or "claim"
	At     string `json:"at"`     // RFC3339Nano, when the command ran
}

// OwnerRequestPath is where a recipe's pending owner request lives.  It is
// local state: never committed, consumed by the hook.
func OwnerRequestPath(root, recipeID string) string {
	return filepath.Join(LocalPath(root), "recipes", recipeID, "owner-request.json")
}

// WriteOwnerRequest records that `bts recipe <action>` just ran for recipeID.
func WriteOwnerRequest(root, recipeID, action string) error {
	p := OwnerRequestPath(root, recipeID)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(&OwnerRequest{Action: action, At: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// TakeOwnerRequest reports whether recipeID has a pending request for
// action written within maxAge, and consumes it.  A request for another
// action is left alone; a stale or unreadable one is removed.
func TakeOwnerRequest(root, recipeID, action string, maxAge time.Duration) bool {
	p := OwnerRequestPath(root, recipeID)
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	var r OwnerRequest
	if json.Unmarshal(data, &r) != nil {
		_ = os.Remove(p)
		return false
	}
	if r.Action != action {
		return false
	}
	_ = os.Remove(p)
	at, err := time.Parse(time.RFC3339Nano, r.At)
	return err == nil && time.Since(at) <= maxAge && time.Since(at) >= -time.Minute
}
