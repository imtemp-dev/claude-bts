package cli

import (
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
)

// `bts recipe log --action <a> --output <f>` used to write the action name
// as the manifest type whenever actionToDocType had no case for it, and
// `bts validate` then rejected the manifest (skeleton and midrun-review on
// the AGEN-35 trial). Every action the changelog accepts must either map to
// a type the manifest accepts or be refused before anything is written.
func TestOutputDocType_EveryActionMapsOrIsRefused(t *testing.T) {
	for action := range engine.ChangelogActions {
		typ, err := outputDocType(action, "")
		if err != nil {
			continue
		}
		if !engine.ManifestDocTypes[typ] {
			t.Errorf("action %q maps to %q, which bts validate rejects", action, typ)
		}
	}
}

// The pairs the shipped skills log with --output must keep working.
func TestOutputDocType_SkillPairsMapToValidTypes(t *testing.T) {
	for _, action := range []string{
		"adjudicate", "architect", "audit", "discover", "draft", "implement", "midrun-review",
		"research", "review", "simulate", "sync", "test", "wireframe",
	} {
		typ, err := outputDocType(action, "")
		if err != nil || !engine.ManifestDocTypes[typ] {
			t.Errorf("action %q: type %q, err %v", action, typ, err)
		}
	}
}

func TestOutputDocType_EvidenceActionsAndBadDocTypesAreRefused(t *testing.T) {
	for _, action := range []string{"skeleton", "falsify", "falsifier-check", "mutate"} {
		if typ, err := outputDocType(action, ""); err == nil {
			t.Errorf("%q writes evidence, not a document; --output must be refused, got type %q", action, typ)
		}
	}
	if _, err := outputDocType("draft", "notes"); err == nil {
		t.Error("an explicit --doc-type the manifest does not accept must be refused")
	}
	if typ, err := outputDocType("comment-apply", "draft"); err != nil || typ != "draft" {
		t.Errorf("a valid explicit --doc-type must be kept: %q %v", typ, err)
	}
}
