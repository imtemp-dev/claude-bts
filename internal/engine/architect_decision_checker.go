package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// architectDecisionBlockRe captures the architect-decision block.
// Requires both opening and closing tags on their own lines (the skill
// instructs authors to write them that way; any other shape is rejected
// so the block is unambiguously parseable later).
var architectDecisionBlockRe = regexp.MustCompile(
	`(?s)<!--\s*architect-decision\s*-->\s*\n(.*?)<!--\s*/architect-decision\s*-->`,
)

// architectSelectedRe captures the Selected: line inside the block.
var architectSelectedRe = regexp.MustCompile(`(?m)^\s*Selected:\s*(\S[^\r\n]*)`)

// architectBasisRe captures the Basis: line inside the block.
var architectBasisRe = regexp.MustCompile(`(?m)^\s*Basis:\s*(\S[^\r\n]*)`)

// architectOwnershipHeadRe locates the Invariant ownership section head.
var architectOwnershipHeadRe = regexp.MustCompile(`(?m)^\s*Invariant ownership:\s*$`)

// architectOwnershipEntryRe captures one "- INV-001: module" entry.
var architectOwnershipEntryRe = regexp.MustCompile(`^\s*-\s*([A-Za-z][\w-]*)\s*:\s*\S`)

// architectURLRe detects a URL inside an official Basis line.
var architectURLRe = regexp.MustCompile(`https?://\S+`)

// CheckArchitectDecisionHeader enforces the wireframe.md architect-decision
// contract (Phase 5.3). Per jig-architect SKILL.md Step 4, wireframe.md
// must carry a `<!-- architect-decision -->` block with a `Selected:`
// line naming the chosen decomposition.
//
// Severity is major (not critical): legacy wireframes authored before
// Phase 5 will lack the block, and we'd rather make the signal visible
// during /verify than hard-block every historical recipe. The CLI
// precondition for phase=architect advances can promote this to a
// hard gate once migration is complete.
func CheckArchitectDecisionHeader(wireframePath string) []Issue {
	data, err := os.ReadFile(wireframePath)
	if err != nil {
		return nil
	}
	content := string(data)

	matches := architectDecisionBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return []Issue{{
			Category: "architect_decision",
			Claim:    "missing_architect_decision_block",
			Severity: "major",
			Detail:   "wireframe.md has no <!-- architect-decision --> block. Run /jig-architect to propose and select a decomposition, then commit the block per jig-architect SKILL.md Step 4. Skip-architect recipes (tiny scope) still need a minimal block declaring Selected: single-path.",
		}}
	}
	if len(matches) > 1 {
		return []Issue{{
			Category: "architect_decision",
			Claim:    "duplicate_architect_decision_block",
			Severity: "major",
			Detail:   "wireframe.md has multiple <!-- architect-decision --> blocks; exactly one is required.",
		}}
	}

	body := matches[0][1]
	var issues []Issue
	selected := architectSelectedRe.FindStringSubmatch(body)
	if len(selected) < 2 || strings.TrimSpace(selected[1]) == "" {
		issues = append(issues, Issue{
			Category: "architect_decision",
			Claim:    "architect_decision_missing_selected",
			Severity: "major",
			Detail:   "architect-decision block is present but has no `Selected:` line naming the chosen alternative.",
		})
	}
	issues = append(issues, checkArchitectBasis(body)...)
	return issues
}

// checkArchitectBasis enforces the Basis: contract (Phase 5.3b): the
// line must exist, must declare `official:` or `custom:`, and an
// official basis must cite a URL — an unsourced "official" claim is
// exactly the drift the official-grounding rule exists to prevent.
func checkArchitectBasis(body string) []Issue {
	basis := architectBasisRe.FindStringSubmatch(body)
	if len(basis) < 2 || strings.TrimSpace(basis[1]) == "" {
		return []Issue{{
			Category: "architect_decision",
			Claim:    "architect_decision_missing_basis",
			Severity: "major",
			Detail:   "architect-decision block has no `Basis:` line. State `official: {pattern} for {framework}@{major} (Source: {URL})` or `custom: {rationale}` (skip-architect recipes use `custom: scope too small to warrant alternatives`).",
		}}
	}
	value := strings.TrimSpace(basis[1])
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "official"):
		if !architectURLRe.MatchString(value) {
			return []Issue{{
				Category: "architect_decision",
				Claim:    "architect_decision_official_unsourced",
				Severity: "major",
				Detail:   "Basis declares an official pattern but cites no URL. Official bases require a `Source:` URL on the vendor's own domain (jig-evidence-policy.md) — unsourced official claims are invalid.",
			}}
		}
	case strings.HasPrefix(lower, "custom"):
		// custom: {rationale} — nothing further to enforce mechanically.
	default:
		return []Issue{{
			Category: "architect_decision",
			Claim:    "architect_decision_basis_format",
			Severity: "major",
			Detail:   "Basis line must start with `official:` (vendor-guidance alternative, with Source URL) or `custom:` (with rationale); got: " + value,
		}}
	}
	return nil
}

// CheckArchitectInvariantCoverage cross-checks the decision block's
// `Invariant ownership:` mapping against domain.md § 2 (Phase 5.3c).
// The mapping is the ownership contract the wireframe must honor — an
// alternative that silently omits invariants defeats the purpose of
// forcing the mapping at decision time. Skips quietly when the block or
// a sibling domain.md is absent (header/domain checks own those cases).
func CheckArchitectInvariantCoverage(wireframePath string) []Issue {
	data, err := os.ReadFile(wireframePath)
	if err != nil {
		return nil
	}
	matches := architectDecisionBlockRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) != 1 {
		return nil
	}
	body := matches[0][1]

	domainPath := filepath.Join(filepath.Dir(wireframePath), "domain.md")
	invariants, err := parseInvariantsTable(domainPath)
	if err != nil || len(invariants) == 0 {
		return nil
	}

	mapped := parseOwnershipEntries(body)
	if mapped == nil {
		return []Issue{{
			Category: "architect_decision",
			Claim:    "architect_decision_missing_invariant_ownership",
			Severity: "major",
			Detail:   "domain.md defines invariants but the architect-decision block has no `Invariant ownership:` section. Map every domain.md § 2 invariant to its owning module — skipping the debate does not skip the ownership contract.",
		}}
	}

	var missing []string
	for _, inv := range invariants {
		if _, ok := mapped[strings.ToUpper(inv.ID)]; !ok {
			missing = append(missing, inv.ID)
		}
	}
	known := make(map[string]bool, len(invariants))
	for _, inv := range invariants {
		known[strings.ToUpper(inv.ID)] = true
	}
	var unknown []string
	for id, display := range mapped {
		if !known[id] {
			unknown = append(unknown, display)
		}
	}

	var issues []Issue
	if len(missing) > 0 {
		issues = append(issues, Issue{
			Category: "architect_decision",
			Claim:    "architect_decision_invariant_unmapped",
			Severity: "major",
			Detail:   "architect-decision block's Invariant ownership mapping omits domain.md invariant(s): " + strings.Join(missing, ", ") + ". Every invariant needs exactly one owning module in the selected decomposition.",
		})
	}
	if len(unknown) > 0 {
		issues = append(issues, Issue{
			Category: "architect_decision",
			Claim:    "architect_decision_invariant_unknown",
			Severity: "minor",
			Detail:   "Invariant ownership maps ID(s) not present in domain.md § 2: " + strings.Join(unknown, ", ") + ". Stale mapping — update the block or domain.md.",
		})
	}
	return issues
}

// parseOwnershipEntries collects "- ID: module" entries following the
// `Invariant ownership:` head, keyed by upper-cased ID with the original
// spelling as value. Returns nil when the section head is absent.
// Scanning is scoped to lines AFTER the head so Rejected-list entries
// (same "- name: reason" shape) are never miscounted.
func parseOwnershipEntries(body string) map[string]string {
	loc := architectOwnershipHeadRe.FindStringIndex(body)
	if loc == nil {
		return nil
	}
	entries := make(map[string]string)
	for _, line := range strings.Split(body[loc[1]:], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := architectOwnershipEntryRe.FindStringSubmatch(line)
		if m == nil {
			break
		}
		entries[strings.ToUpper(m[1])] = m[1]
	}
	return entries
}
