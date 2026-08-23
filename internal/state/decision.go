package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Decision holds — the durable record of a question only the user can answer.
//
// The findings ledger already gives verification findings identity across
// rounds, and `jig recipe log` records that the convergence budget was
// exhausted. What neither records is the handoff that follows: the loop
// stops, the skill says "ask the user for guidance", and the question and
// the answer live only in the conversation. A compaction, a new session,
// or simply the next day loses both, and the recipe's own state cannot
// tell "waiting on a person" apart from "still working".
//
// decisions.jsonl closes that. It is an append-only event log folded to
// one current state per key, in the recipe's tracked directory next to
// findings.jsonl — a decision that shaped a spec is part of that spec's
// provenance and must survive a fresh clone.
//
// Identity is the caller-supplied key, not derived from the question text:
// the same decision has to stay the same decision across rounds even when
// it is rephrased. Reusing a key with a different question is rejected
// rather than silently rewriting history.

// Decision statuses.
const (
	DecisionOpen     = "open"     // waiting on the user
	DecisionResolved = "resolved" // the user answered; Answer carries it
	DecisionDropped  = "dropped"  // no longer needed, never answered
)

// DecisionEvent is one append-only observation about one decision.
type DecisionEvent struct {
	Key       string   `json:"key"`
	Doc       string   `json:"doc,omitempty"`     // document the decision blocks
	Question  string   `json:"question"`          // what the user must decide
	Options   []string `json:"options,omitempty"` // candidate answers, if enumerable
	Blocks    []string `json:"blocks,omitempty"`  // finding or task IDs held by this
	Status    string   `json:"status"`            // open, resolved, dropped
	Answer    string   `json:"answer,omitempty"`  // set when resolved
	Reason    string   `json:"reason,omitempty"`  // set when dropped
	Timestamp string   `json:"timestamp"`
}

// DecisionState is the folded current state of one decision.
type DecisionState struct {
	Key      string   `json:"key"`
	Doc      string   `json:"doc,omitempty"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Blocks   []string `json:"blocks,omitempty"`
	Status   string   `json:"status"`
	Answer   string   `json:"answer,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Raised   string   `json:"raised"`  // first-seen timestamp
	Updated  string   `json:"updated"` // latest event timestamp
}

// DecisionsPath returns the ledger path for a recipe.
func DecisionsPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "decisions.jsonl")
}

// ReadDecisionEvents returns every recorded event in order. A missing
// ledger is an empty history, not an error.
func ReadDecisionEvents(root, recipeID string) ([]DecisionEvent, error) {
	f, err := os.Open(DecisionsPath(root, recipeID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []DecisionEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e DecisionEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// A corrupt line must not hide the rest of the ledger.
			continue
		}
		events = append(events, e)
	}
	return events, scanner.Err()
}

// FoldDecisions collapses the event log to current state per key, sorted
// by key for stable output. The latest event for a key wins; the first
// event's timestamp is retained as Raised so "how long has this been
// waiting" survives later updates.
func FoldDecisions(events []DecisionEvent) []DecisionState {
	byKey := map[string]*DecisionState{}
	for i := range events {
		e := events[i]
		cur, ok := byKey[e.Key]
		if !ok {
			byKey[e.Key] = &DecisionState{
				Key: e.Key, Doc: e.Doc, Question: e.Question, Options: e.Options,
				Blocks: e.Blocks, Status: e.Status, Answer: e.Answer, Reason: e.Reason,
				Raised: e.Timestamp, Updated: e.Timestamp,
			}
			continue
		}
		cur.Status = e.Status
		cur.Updated = e.Timestamp
		if e.Answer != "" {
			cur.Answer = e.Answer
		}
		if e.Reason != "" {
			cur.Reason = e.Reason
		}
		if e.Doc != "" {
			cur.Doc = e.Doc
		}
		if len(e.Blocks) > 0 {
			cur.Blocks = e.Blocks
		}
	}
	out := make([]DecisionState, 0, len(byKey))
	for _, v := range byKey {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// LoadDecisions reads and folds in one step.
func LoadDecisions(root, recipeID string) ([]DecisionState, error) {
	events, err := ReadDecisionEvents(root, recipeID)
	if err != nil {
		return nil, err
	}
	return FoldDecisions(events), nil
}

// OpenDecisions returns only the decisions still waiting on the user.
func OpenDecisions(root, recipeID string) ([]DecisionState, error) {
	all, err := LoadDecisions(root, recipeID)
	if err != nil {
		return nil, err
	}
	var open []DecisionState
	for _, d := range all {
		if d.Status == DecisionOpen {
			open = append(open, d)
		}
	}
	return open, nil
}

// appendDecisionEvent writes one event, stamping the timestamp.
func appendDecisionEvent(root, recipeID string, e *DecisionEvent) error {
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	path := DecisionsPath(root, recipeID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// HoldDecision records a question for the user.
//
// Idempotent for an exact repeat: re-holding an already-open key with the
// same question is a no-op, so a retried skill step does not multiply the
// ledger. Reusing a key with a DIFFERENT question is an error — that is a
// second decision wearing the first one's identity, and silently
// overwriting it would lose the original question. Re-opening an already
// resolved key is likewise refused: the answer is history.
func HoldDecision(root, recipeID string, e *DecisionEvent) (created bool, err error) {
	if strings.TrimSpace(e.Key) == "" {
		return false, fmt.Errorf("decision key is required")
	}
	if strings.TrimSpace(e.Question) == "" {
		return false, fmt.Errorf("decision question is required")
	}
	existing, err := LoadDecisions(root, recipeID)
	if err != nil {
		return false, err
	}
	for _, d := range existing {
		if d.Key != e.Key {
			continue
		}
		switch d.Status {
		case DecisionOpen:
			if d.Question != e.Question {
				return false, fmt.Errorf(
					"decision %q is already open with a different question (%q). "+
						"Use a new key for a new decision, or resolve the existing one first",
					e.Key, d.Question)
			}
			return false, nil // exact repeat
		case DecisionResolved:
			return false, fmt.Errorf(
				"decision %q was already resolved (%q). Reopening would lose that answer; use a new key",
				e.Key, d.Answer)
		}
	}
	e.Status = DecisionOpen
	if err := appendDecisionEvent(root, recipeID, e); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveDecision records the user's answer. The decision must exist and
// be open — resolving an unknown key would create an answer to a question
// that was never asked.
func ResolveDecision(root, recipeID, key, answer string) error {
	if strings.TrimSpace(answer) == "" {
		return fmt.Errorf("an answer is required to resolve a decision")
	}
	all, err := LoadDecisions(root, recipeID)
	if err != nil {
		return err
	}
	for _, d := range all {
		if d.Key != key {
			continue
		}
		if d.Status != DecisionOpen {
			return fmt.Errorf("decision %q is %s, not open", key, d.Status)
		}
		return appendDecisionEvent(root, recipeID, &DecisionEvent{
			Key: key, Doc: d.Doc, Question: d.Question,
			Status: DecisionResolved, Answer: answer,
		})
	}
	return fmt.Errorf("no decision with key %q in recipe %s", key, recipeID)
}

// DropDecision retires a question that turned out not to need an answer.
// Separate from resolve so the ledger never records a fabricated answer.
func DropDecision(root, recipeID, key, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("a reason is required to drop a decision")
	}
	all, err := LoadDecisions(root, recipeID)
	if err != nil {
		return err
	}
	for _, d := range all {
		if d.Key != key {
			continue
		}
		if d.Status != DecisionOpen {
			return fmt.Errorf("decision %q is %s, not open", key, d.Status)
		}
		return appendDecisionEvent(root, recipeID, &DecisionEvent{
			Key: key, Doc: d.Doc, Question: d.Question,
			Status: DecisionDropped, Reason: reason,
		})
	}
	return fmt.Errorf("no decision with key %q in recipe %s", key, recipeID)
}
