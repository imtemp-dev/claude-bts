package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A verify fork in flight.  Rule 3 ("re-verify after every modification")
// used to block every turn end from the moment a spec document changed
// until a round was logged, so an agent that had started the /bts-verify
// fork in the background could not end its turn to wait for the fork's
// notification; it waited inside the turn or repeated an "unverified" note
// on each stop (trial problem #8, seen on r-002, r-003 and true-shark).
//
// The SubagentStart hook records a verifier or auditor fork here, the
// SubagentStop hook removes it, and logging a round clears them all.  A
// record older than VerifyInFlightMaxAge no longer counts, so a fork that
// died without a SubagentStop cannot hold rule 3 open forever.

// VerifyInFlightMaxAge bounds how long a started fork counts as running.
// Measured review rounds took about 16 minutes.
const VerifyInFlightMaxAge = 60 * time.Minute

// IsVerifyAgent reports whether a subagent type runs a verify round.
func IsVerifyAgent(agentType string) bool {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(agentType)), "bts-") {
	case "verifier", "auditor":
		return true
	}
	return false
}

func verifyInFlightDir(root string) string {
	return filepath.Join(LocalPath(root), "verify-inflight")
}

type verifyInFlight struct {
	AgentType string `json:"agent_type"`
	StartedAt string `json:"started_at"`
}

// MarkVerifyStarted records a verify fork that just started.
func MarkVerifyStarted(root, agentID, agentType string) error {
	if agentID == "" {
		return nil
	}
	dir := verifyInFlightDir(root)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(&verifyInFlight{AgentType: agentType, StartedAt: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filepath.Base(agentID)+".json"), data, 0644)
}

// MarkVerifyStopped removes a finished fork's record; a missing one is fine.
func MarkVerifyStopped(root, agentID string) {
	if agentID != "" {
		_ = os.Remove(filepath.Join(verifyInFlightDir(root), filepath.Base(agentID)+".json"))
	}
}

// ClearVerifyInFlight removes every record: a logged round ends them.
func ClearVerifyInFlight(root string) {
	_ = os.RemoveAll(verifyInFlightDir(root))
}

// VerifyInFlight reports whether a verify fork started within
// VerifyInFlightMaxAge is still running, and when the oldest one started.
func VerifyInFlight(root string) (bool, time.Time) {
	entries, err := os.ReadDir(verifyInFlightDir(root))
	if err != nil {
		return false, time.Time{}
	}
	var oldest time.Time
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(verifyInFlightDir(root), e.Name()))
		if err != nil {
			continue
		}
		var v verifyInFlight
		if json.Unmarshal(data, &v) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, v.StartedAt)
		if err != nil || time.Since(at) > VerifyInFlightMaxAge {
			continue
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	return !oldest.IsZero(), oldest
}
