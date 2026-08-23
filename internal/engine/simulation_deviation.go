package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SimDeviation represents a DEVIATION tagged in a simulations/*.md
// file. Phase 12 promotes these into structured tokens so /jig-sync
// can ingest them directly instead of re-discovering the same
// differences in its file-by-file pass.
type SimDeviation struct {
	ID       string
	File     string // source simulation file (basename)
	Driver   string // always "simulate" — kept as a field for symmetry with DeviationRow
	Severity string
	Detail   string
}

// Preferred (new) grammar:
//   DEVIATION {id=sim-003.s2} {driver=simulate} {severity=major}: body
var simDeviationStructuredRe = regexp.MustCompile(
	`(?m)^DEVIATION\s*\{id=([^}]+)\}\s*\{driver=([^}]+)\}\s*\{severity=([^}]+)\}\s*:\s*(.+)$`,
)

// Legacy grammar (pre-Phase-12):
//   DEVIATION: body
// or
//   - [DEVIATION-001] body
// We parse both so historical simulations stay consumable. Callers get
// synthesized ids (sim-{file}.s{n}) and severity="major" defaults so
// the data flows into deviation.md without manual cleanup.
var (
	simDeviationLegacyBulletRe = regexp.MustCompile(
		`(?m)^\s*-\s*\[DEVIATION-0*(\d+)\]\s*(.+)$`,
	)
	simDeviationLegacyLineRe = regexp.MustCompile(
		`(?m)^DEVIATION:\s*(.+)$`,
	)
)

// ExtractSimulationDeviations scans simulations/*.md under the recipe
// directory and returns all DEVIATION entries. Structured entries are
// parsed verbatim; legacy prose forms receive synthesized ids and a
// default severity of "major". Results are deterministic (sorted by
// file then by id) so downstream comparisons against deviation.md stay
// reproducible.
func ExtractSimulationDeviations(recipeDir string) ([]SimDeviation, error) {
	simsDir := filepath.Join(recipeDir, "simulations")
	entries, err := os.ReadDir(simsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []SimDeviation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(simsDir, e.Name()))
		if err != nil {
			continue
		}
		content := string(data)
		// Strip trailing extensions when synthesizing ids so
		// `001-scenarios.md` → `sim-001-scenarios`.
		simBase := strings.TrimSuffix(e.Name(), ".md")

		// Structured form first.
		structuredSeen := map[string]bool{}
		for _, m := range simDeviationStructuredRe.FindAllStringSubmatch(content, -1) {
			if len(m) < 5 {
				continue
			}
			id := strings.TrimSpace(m[1])
			sev := strings.ToLower(strings.TrimSpace(m[3]))
			out = append(out, SimDeviation{
				ID:       id,
				File:     e.Name(),
				Driver:   "simulate",
				Severity: sev,
				Detail:   strings.TrimSpace(m[4]),
			})
			structuredSeen[id] = true
		}

		// Legacy bulleted form with explicit DEVIATION-NNN number.
		for _, m := range simDeviationLegacyBulletRe.FindAllStringSubmatch(content, -1) {
			if len(m) < 3 {
				continue
			}
			id := "sim-" + simBase + ".d" + strings.TrimLeft(m[1], "0")
			if id == "sim-"+simBase+".d" {
				id = "sim-" + simBase + ".d1"
			}
			if structuredSeen[id] {
				continue
			}
			out = append(out, SimDeviation{
				ID:       id,
				File:     e.Name(),
				Driver:   "simulate",
				Severity: "major",
				Detail:   strings.TrimSpace(m[2]),
			})
		}

		// Legacy bare-line form. Counter-based id avoids collision when
		// a single file carries multiple DEVIATION: lines.
		counter := 0
		for _, m := range simDeviationLegacyLineRe.FindAllStringSubmatch(content, -1) {
			if len(m) < 2 {
				continue
			}
			counter++
			id := "sim-" + simBase + ".s" + itoa(counter)
			if structuredSeen[id] {
				continue
			}
			out = append(out, SimDeviation{
				ID:       id,
				File:     e.Name(),
				Driver:   "simulate",
				Severity: "major",
				Detail:   strings.TrimSpace(m[1]),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// CheckSimDeviationConsumption cross-references simulation DEVIATIONs
// against deviation.md. Each SimDeviation must appear in deviation.md
// with Driver=simulate:{id}. A missing id surfaces as
// sim_deviation_unconsumed (major) — not critical, because sync may
// intentionally collapse multiple sim findings into a single
// deviation.md row, and the reviewer's judgement should drive the
// final call.
func CheckSimDeviationConsumption(recipeDir string) []Issue {
	sims, err := ExtractSimulationDeviations(recipeDir)
	if err != nil || len(sims) == 0 {
		return nil
	}
	devPath := filepath.Join(recipeDir, "deviation.md")
	if _, err := os.Stat(devPath); err != nil {
		return nil // deviation.md not yet generated — sync runs later
	}
	rows, err := ParseDeviationMd(devPath)
	if err != nil {
		return nil
	}

	covered := map[string]bool{}
	for _, r := range rows {
		for _, d := range r.Drivers {
			if strings.HasPrefix(d, "simulate:") {
				covered[strings.TrimPrefix(d, "simulate:")] = true
			}
		}
	}

	var issues []Issue
	for _, s := range sims {
		if !covered[s.ID] {
			issues = append(issues, Issue{
				Category: "sim_deviation",
				Claim:    "sim_deviation_unconsumed: " + s.ID,
				Severity: "major",
				Detail:   "simulations/" + s.File + " declares DEVIATION " + s.ID + " but deviation.md has no row citing `simulate:" + s.ID + "`. Either sync the finding into deviation.md or amend an existing row's Driver column.",
			})
		}
	}
	return issues
}
