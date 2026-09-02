package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// MergeFindingsBlocks joins the <bts-findings> blocks of several
// instrument outputs into the primary document's single block.
//
// One verify round is supposed to run verify, audit and simulate at once
// and be recorded once, with all three dimensions. The instruments are
// separate forks, so they return three findings blocks in three files —
// verification.md, audit.md, simulations/NNN.md — and joining them was
// left to the orchestrator by hand. A measured recipe (r-026-issue-204)
// ran all three concurrently and then logged them as three rounds of one
// dimension each: three of its six-round cap spent, and no two rounds of
// a comparable class to judge convergence by. This function is the
// mechanical join so the orchestrator never has to re-type counts.
//
// The primary block keeps every key it already had (paths_total,
// evidence_resolved, …); only the five counts and the findings array are
// replaced by the union. Each extra must carry a findings array whenever
// it reports anything, because counts without titles cannot enter the
// ledger and a merged block whose array disagrees with its counts is
// rejected by ParseFindingsBlock — the check exists so the ledger can
// never quietly disagree with the gate.
//
// The returned bytes are the primary with its block rewritten and a
// `<!-- bts-merged: ... -->` marker appended naming the sources, so a
// second merge of the same files is refused instead of double-counted.
func MergeFindingsBlocks(primary []byte, extras [][]byte, sourceNames []string) ([]byte, *FindingsCounts, error) {
	if len(extras) != len(sourceNames) {
		return nil, nil, fmt.Errorf("merge: %d sources but %d names", len(extras), len(sourceNames))
	}
	if already := mergedMarkerRe.FindSubmatch(primary); already != nil {
		return nil, nil, fmt.Errorf("merge: the primary block already merged %s — re-run without --merge, or restore the un-merged file first",
			strings.TrimSpace(string(already[1])))
	}
	base, err := ParseFindingsBlock(primary)
	if err != nil {
		return nil, nil, fmt.Errorf("primary: %w", err)
	}
	raw := findingsBlockRe.FindSubmatch(primary)
	var obj map[string]any
	if err := json.Unmarshal(raw[1], &obj); err != nil {
		return nil, nil, fmt.Errorf("primary block: %w", err)
	}

	merged := *base
	merged.Findings = append([]state.ReportedFinding(nil), base.Findings...)
	arrayMissing := base.Total() > 0 && len(base.Findings) == 0
	for i, ex := range extras {
		c, err := ParseFindingsBlock(ex)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", sourceNames[i], err)
		}
		merged.Critical += c.Critical
		merged.Major += c.Major
		merged.MinorResolvable += c.MinorResolvable
		merged.MinorDeferred += c.MinorDeferred
		merged.Info += c.Info
		merged.Findings = append(merged.Findings, c.Findings...)
		if c.Total() > 0 && len(c.Findings) == 0 {
			arrayMissing = true
		}
	}
	if arrayMissing && merged.Total() > 0 {
		return nil, nil, fmt.Errorf("merge: every merged block must carry a findings array — counts alone cannot be joined into one ledger, and a block whose array disagrees with its counts is refused")
	}

	obj["critical"] = merged.Critical
	obj["major"] = merged.Major
	obj["minor_resolvable"] = merged.MinorResolvable
	obj["minor_deferred"] = merged.MinorDeferred
	obj["info"] = merged.Info
	if merged.Findings == nil {
		merged.Findings = []state.ReportedFinding{}
	}
	obj["findings"] = merged.Findings

	enc, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("merge: encode block: %w", err)
	}
	block := "<bts-findings>\n" + string(enc) + "\n</bts-findings>"
	out := findingsBlockRe.ReplaceAllLiteral(primary, []byte(block))
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, []byte("\n<!-- bts-merged: "+strings.Join(sourceNames, ", ")+" -->\n")...)
	return out, &merged, nil
}

// mergedMarkerRe finds the marker a previous merge left behind.
var mergedMarkerRe = regexp.MustCompile(`<!--\s*bts-merged:\s*([^>]*?)\s*-->`)
