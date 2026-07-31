package engine

// HardGate is a deterministic enforcement point for a mandatory rule.
// Each entry pairs a stable ID used in rule frontmatter (e.g., "{gate:
// hard}" inline tags) with the source location that actually enforces it.
// `bts validate --gates` cross-checks that every declared hard gate maps
// to a real code path.
type HardGate struct {
	ID          string // stable identifier cited in rule docs
	Rule        string // rule source (file:rule-number)
	Enforcement string // code location that enforces it
	Summary     string // one-line explanation
}

// HardGates is the authoritative registry of machine-enforced rules.
// Adding a "{gate: hard}" tag in a rule document without registering it
// here will fail `bts validate --gates` once that subcommand ships.
var HardGates = []HardGate{
	{
		ID:          "verify_after_modification",
		Rule:        "bts-recipe-protocol.md §Mandatory Rules rule 3",
		Enforcement: "internal/hook/stop.go:handleSpecDone",
		Summary:     "Block <bts>DONE</bts> unless verify-log last entry has critical=0 major=0 minor_resolvable=0",
	},
	{
		ID:          "log_every_action",
		Rule:        "bts-recipe-protocol.md §Mandatory Rules rule 4",
		Enforcement: "internal/cli/recipe.go:recipeLogCmd",
		Summary:     "Validator flags changelog gaps; log subcommand is the only way to append canonical entries",
	},
	{
		ID:          "simulate_at_least_once",
		Rule:        "bts-recipe-protocol.md §Mandatory Rules rule 5",
		Enforcement: "internal/hook/stop.go:handleSpecDone (blueprint: changelog must contain a simulate action) + internal/cli/recipe.go:checkPhasePreConditions(review) warn",
		Summary:     "Block <bts>DONE</bts> for blueprint recipes whose changelog has no simulate action",
	},
	{
		ID:          "adjudicate_every_debate",
		Rule:        "bts-recipe-protocol.md §Mandatory Rules rule 7",
		Enforcement: "internal/engine/validator.go:validateDebateMetaJSON",
		Summary:     "Debate meta.json must carry a decided boolean and, when decided, a string conclusion",
	},
	{
		ID:          "sync_check_before_final",
		Rule:        "bts-recipe-protocol.md §Mandatory Rules rule 8",
		Enforcement: "internal/cli/sync_check.go (changelog append) + internal/hook/stop.go:handleSpecDone (pass-after-last-modification ordering gate, blueprint)",
		Summary:     "Block <bts>DONE</bts> unless a passing sync-check changelog entry postdates the last draft/improve/comment-apply action",
	},
	{
		ID:          "deferred_minors_declared",
		Rule:        "bts-recipe-blueprint SKILL.md §Quality Rules 3b",
		Enforcement: "internal/hook/stop.go:handleSpecDone (via engine.CheckKnownUncertainties)",
		Summary:     "Block <bts>DONE</bts> when the last verify entry has minor_deferred>0 but the spec carries no ## Known Uncertainties (### U-NNN) entries",
	},
	{
		ID:          "status_at_finalization",
		Rule:        "bts-recipe-protocol.md §Mandatory Rules rule 9",
		Enforcement: "internal/hook/stop.go:handleSpecDone",
		Summary:     "Completion sets phase=finalize which triggers the status flow",
	},
	{
		ID:          "spec_before_code",
		Rule:        "bts-implement-protocol.md §Execution Rules rule 1",
		Enforcement: "internal/cli/recipe.go:checkPhasePreConditions(implement)",
		Summary:     "Phase transition to implement warns if final.md missing; stop hook blocks IMPLEMENT DONE without tasks.json",
	},
	{
		ID:          "build_verification",
		Rule:        "bts-implement-protocol.md §Execution Rules rule 2",
		Enforcement: "bts-implement/SKILL.md Step 3 + stop hook via test-results.json",
		Summary:     "Implementation skill runs build per task; stop hook requires test-results.status=pass",
	},
	{
		ID:          "test_after_implement",
		Rule:        "bts-implement-protocol.md §Execution Rules rule 4",
		Enforcement: "internal/hook/stop.go:handleImplementDone",
		Summary:     "IMPLEMENT DONE blocked unless test-results.json exists with status=pass",
	},
	{
		ID:          "sync_after_test",
		Rule:        "bts-implement-protocol.md §Execution Rules rule 5",
		Enforcement: "internal/hook/stop.go:handleImplementDone",
		Summary:     "IMPLEMENT DONE blocked unless deviation.md exists",
	},
	{
		ID:          "review_before_done",
		Rule:        "bts-implement-protocol.md (implied by handleImplementDone)",
		Enforcement: "internal/hook/stop.go:handleImplementDone",
		Summary:     "IMPLEMENT DONE blocked unless review.md exists",
	},
	{
		ID:          "phase_transitions_logged",
		Rule:        "bts-implement-protocol.md §Execution Rules rule 7",
		Enforcement: "internal/cli/recipe.go:recipeLogCmd phase flag",
		Summary:     "Phase updates flow through the log command; metrics append captures every change",
	},
	{
		ID:          "tracked_status_writes",
		Rule:        "bts-implement-protocol.md §Execution Rules rule 6",
		Enforcement: "internal/engine/validator.go:validateTasksJSON",
		Summary:     "tasks.json schema validated; status enum forces the state machine",
	},
	{
		ID:          "uncertainties_resolved",
		Rule:        "bts-implement/SKILL.md §Step 5.7 + §Completion",
		Enforcement: "internal/hook/stop.go:handleImplementDone",
		Summary:     "IMPLEMENT DONE blocked unless every `## Known Uncertainties` entry carries Resolved:/Diverged:/Still-unknown:",
	},
	{
		ID:          "task_anchor_coverage",
		Rule:        "bts-implement/SKILL.md §Step 1 + bts-schema.md tasks.json",
		Enforcement: "internal/engine/task_anchor_checker.go:CheckTaskAnchors",
		Summary:     "tasks.json and final.md must share a 1:1 `<!-- task-anchor: path action -->` ↔ Task.anchor mapping",
	},
	{
		ID:          "modify_scope_declared",
		Rule:        "bts-implement/SKILL.md §Step 3 IMPLEMENT (Phase 14)",
		Enforcement: "internal/engine/task_anchor_checker.go:CheckModifyScope",
		Summary:     "Action=modify tasks must declare ModifyScope and the final.md anchor must carry a matching scope= suffix",
	},
	{
		ID:          "modify_scope_respected",
		Rule:        "bts-implement/SKILL.md §Step 3 IMPLEMENT (Phase 14)",
		Enforcement: "internal/hook/stop.go:handleImplementDone (via CheckModifyScope with projectRoot)",
		Summary:     "IMPLEMENT DONE blocked when declared scope symbols do not exist in the target file",
	},
	{
		ID:          "deviation_driver_required",
		Rule:        "bts-sync/SKILL.md §Step 5 (Phase 16)",
		Enforcement: "internal/engine/deviation_checker.go:CheckDeviationSchema + stop.go",
		Summary:     "Every deviation.md row must carry a unique ID, at least one Driver from the vocabulary, and a Severity",
	},
	{
		ID:          "sim_deviation_consumed",
		Rule:        "bts-sync/SKILL.md §Step 2.5 (Phase 12)",
		Enforcement: "internal/engine/simulation_deviation.go:CheckSimDeviationConsumption",
		Summary:     "Every DEVIATION entry in simulations/*.md must land in deviation.md with a matching simulate:{id} Driver",
	},
	{
		ID:          "test_scenario_link_required",
		Rule:        "bts-test/SKILL.md §Step 2 + §Step 3 ASSESS (Phase 13)",
		Enforcement: "internal/engine/test_scenario_map.go:CheckTestScenarioCoverage",
		Summary:     "Every simulation scenario must be linked via `bts:scenario {id}` to at least one test; failing results must carry a `category`",
	},
	{
		ID:          "recipe_state_normalized",
		Rule:        "bts-recipe-protocol.md §Completion (Phase 21)",
		Enforcement: "internal/hook/stop.go:handleSpecDone + internal/cli/recipe.go:recipeReconcileCmd",
		Summary:     "On <bts>DONE</bts> recipe.json level→3.0 and iteration←max(curr, last_verify); `bts recipe reconcile` recovers missed DONE",
	},
	{
		ID:          "verification_log_consistency",
		Rule:        "bts-verify/SKILL.md §Output + Phase 22",
		Enforcement: "internal/engine/validator.go:validateVerificationLogConsistency",
		Summary:     "verification.md <bts-findings> counts must match the last verify-log.jsonl entry",
	},
	{
		ID:          "findings_array_consistency",
		Rule:        "bts-verification-protocol.md §Finding Identity",
		Enforcement: "internal/engine/validator.go:ParseFindingsBlock",
		Summary:     "A <bts-findings> findings array must match the block's counts per severity and carry non-empty titles; a mismatch fails `bts recipe log` so a round cannot be recorded with an unusable ledger",
	},
	{
		ID:          "convergence_budget",
		Rule:        "bts-verification-protocol.md §Convergence",
		Enforcement: "internal/engine/convergence.go:EvaluateConvergence + internal/cli/recipe.go:recipeLogCmd",
		Summary:     "verify.max_iterations consecutive rounds without progress on (critical, major, minor_resolvable) marks the entry failed and stops the loop",
	},
	{
		ID:          "full_pass_before_final",
		Rule:        "bts-verification-protocol.md §Verification Scope",
		Enforcement: "internal/hook/stop.go:handleSpecDone",
		Summary:     "Block <bts>DONE</bts> when the spec's last verify entry is a scoped delta pass rather than a full-document pass",
	},
	{
		ID:          "per_document_verify_state",
		Rule:        "bts-verification-protocol.md §Convergence",
		Enforcement: "internal/state/recipe.go:VerifyEntriesForDoc + internal/hook/stop.go:handleSpecDone",
		Summary:     "Convergence is evaluated per verified document; a wireframe round can no longer satisfy or reopen draft.md's completion gate",
	},
}

// InvariantGates lists domain-level checks enforced via `bts verify`.
// They surface as critical findings but operate through the standard
// verification pipeline rather than a dedicated hook.
var InvariantGates = []HardGate{
	{
		ID:          "domain_before_wireframe",
		Rule:        "bts-recipe-protocol.md §Mandatory Rules rule 10",
		Enforcement: "internal/cli/recipe.go:checkPhasePreConditions(wireframe)",
		Summary:     "domain.md must exist for blueprint/design recipes before phase=wireframe (strict)",
	},
	{
		ID:          "invariant_single_owner",
		Rule:        "bts-domain-model/SKILL.md §Quality Gate",
		Enforcement: "internal/engine/domain_checker.go:CheckInvariantOwnership",
		Summary:     "Every invariant in domain.md §2 must have exactly one owner; duplicates raise critical",
	},
	{
		ID:          "midrun_review_scheduled",
		Rule:        "bts-implement/SKILL.md §Step 3 MID-RUN REVIEW + settings.implement.midrun_review_every",
		Enforcement: "bts-implement/SKILL.md orchestrator (advisory — not hook-blocked)",
		Summary:     "Implementations above the configured task threshold should produce at least one reviews/midrun-*.md; monitored in Phase 17",
	},
	{
		ID:          "retry_ladder_respected",
		Rule:        "bts-implement/SKILL.md §Step 3 VERIFY retry block (Phase 15)",
		Enforcement: "engine.NextRetryDecision + `bts retry next` CLI (advisory — monitored in Phase 17)",
		Summary:     "Blocked tasks should have escalated through the ladder; tasks ending at tier<5 are flagged in monitoring as skipped escalations",
	},
}
