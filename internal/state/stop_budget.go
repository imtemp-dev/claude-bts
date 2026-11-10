package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Stop-hook block budget.
//
// A Stop hook that blocks has no natural termination: the model gets the
// block reason, tries again, and if it cannot satisfy the gate it stops
// again, blocks again. Claude Code sets `stop_hook_active` on every stop
// after any stop-hook continuation, so that field cannot be used as a
// one-shot latch — a second block is indistinguishable from the tenth.
// Claude's own override fires at 8 consecutive blocks, which is both slow
// and opaque to the user.
//
// The budget bounds it explicitly: the same reason may block at most
// DefaultStopBlockBudget times in a row, after which the stop is allowed
// with a visible message. Any allow clears the counter, so a recipe that
// recovers gets a full budget again. Keyed by reason, so a genuinely new
// problem is not silenced by an earlier one's exhausted budget.

// DefaultStopBlockBudget is how many consecutive blocks one reason gets.
// Kept below Claude Code's 8-block override so bts, not the harness,
// decides when to give up — the harness's override carries no explanation.
const DefaultStopBlockBudget = 3

// StopBudget is the persisted block counter.
type StopBudget struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason"`
	Count     int    `json:"count"`
	// StoodDown is set when the budget for (SessionID, Reason) ran out.
	// That session is not blocked on that reason again: it has heard it,
	// and on cool-crow every later turn of an unrelated conversation met
	// two more blocks and another stand-down (trial problem #118).
	// Another reason, another session, or an allowed stop arms the gate
	// again, so it is never disarmed for good.
	StoodDown bool `json:"stood_down,omitempty"`
}

// StoodDown reports whether the gate already stood down on this reason
// for this session.
func StoodDown(root, sessionID, reason string) bool {
	b := LoadStopBudget(root)
	return b.StoodDown && b.SessionID == sessionID && b.Reason == reason
}

// MarkStoodDown records that the budget for (sessionID, reason) ran out.
func MarkStoodDown(root, sessionID, reason string) {
	saveStopBudget(root, &StopBudget{SessionID: sessionID, Reason: reason, StoodDown: true})
}

func saveStopBudget(root string, b *StopBudget) {
	if data, err := json.Marshal(b); err == nil {
		_ = os.MkdirAll(filepath.Dir(stopBudgetPath(root)), 0755)
		_ = os.WriteFile(stopBudgetPath(root), data, 0644)
	}
}

func stopBudgetPath(root string) string {
	return filepath.Join(LocalPath(root), "stop-budget.json")
}

// LoadStopBudget reads the current counter. A missing, unreadable, or
// malformed file is an empty budget — this is loop control, not a gate,
// so it fails open toward "no blocks recorded yet".
func LoadStopBudget(root string) *StopBudget {
	data, err := os.ReadFile(stopBudgetPath(root))
	if err != nil {
		return &StopBudget{}
	}
	var b StopBudget
	if err := json.Unmarshal(data, &b); err != nil {
		return &StopBudget{}
	}
	return &b
}

// ChargeStopBlock records one block against (sessionID, reason) and
// reports the resulting count and whether the budget is now exhausted.
// A different session or a different reason restarts the count at 1.
func ChargeStopBlock(root, sessionID, reason string, budget int) (count int, exhausted bool) {
	if budget <= 0 {
		budget = DefaultStopBlockBudget
	}
	b := LoadStopBudget(root)
	if b.SessionID != sessionID || b.Reason != reason {
		b = &StopBudget{SessionID: sessionID, Reason: reason}
	}
	b.Count++
	// Best-effort persistence: if the counter cannot be written the block
	// still happens, it just is not counted. That degrades toward Claude's
	// own override rather than toward an unbounded silent loop.
	saveStopBudget(root, b)
	return b.Count, b.Count >= budget
}

// ClearStopBudget resets the counter. Called on every allowed stop so a
// recovered recipe starts its next episode with a full budget.
func ClearStopBudget(root string) {
	_ = os.Remove(stopBudgetPath(root))
}
