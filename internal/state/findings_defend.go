package state

import "sort"

// DefendBatch selects the findings /bts-defend argues against in one
// pass: the open critical and major findings, highest severity first,
// then the ones that have survived the most rounds, capped at limit
// (0 = no cap). The remainder comes back as rest so the caller can name
// what was left undefended instead of letting it read as defended.
//
// Minor findings are never in the batch. A [resolvable] minor is
// cheaper to fix than to argue about, and a [deferred] one is a runtime
// watch-item, not a claim a defense could settle.
//
// Doing the selection here rather than in the defender's prompt is the
// point: the defender is a sonnet fork that reads the ledger itself, so
// no spawn prompt carries the batch for the pre-tool-use hook to count.
// A cap the agent applies to its own reading is prose; a cap on what
// the command prints is not.
func DefendBatch(states []*FindingState, limit int) (batch, rest []*FindingState) {
	var eligible []*FindingState
	for _, st := range states {
		if st == nil || !NotClosed(st.Status) {
			continue
		}
		if st.Severity != "critical" && st.Severity != "major" {
			continue
		}
		eligible = append(eligible, st)
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.Severity != b.Severity {
			return a.Severity == "critical"
		}
		if a.OpenRounds != b.OpenRounds {
			return a.OpenRounds > b.OpenRounds
		}
		if a.FirstIteration != b.FirstIteration {
			return a.FirstIteration < b.FirstIteration
		}
		return a.ID < b.ID
	})
	if limit <= 0 || len(eligible) <= limit {
		return eligible, nil
	}
	return eligible[:limit], eligible[limit:]
}
