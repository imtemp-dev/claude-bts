package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The settings template is what every `bts init` writes, and
// engine.LoadSettings reads it with gopkg.in/yaml.v3 — which rejects a
// duplicate mapping key outright rather than taking the last one. So a
// key added at the top of `verify:` while an older copy stayed further
// down does not quietly override the new default; it makes the whole
// file unparseable for every new project.
//
// That is exactly what shipped: max_section_lines and
// section_span_severity were each defined twice under `verify:`. The
// engine package cannot see the embedded FS, so the guard lives here,
// next to the file it guards.
func TestEmbeddedSettingsYAMLHasNoDuplicateKeys(t *testing.T) {
	tmplFS, err := EmbeddedTemplates()
	if err != nil {
		t.Fatalf("embedded templates: %v", err)
	}
	data, err := fs.ReadFile(tmplFS, ".bts/config/settings.yaml")
	if err != nil {
		t.Fatalf("read settings.yaml from the embedded FS: %v", err)
	}
	// KnownFields is irrelevant here; the duplicate-key check is
	// unconditional in yaml.v3's decoder.
	var into map[string]any
	if err := yaml.Unmarshal(data, &into); err != nil {
		t.Fatalf("the shipped settings.yaml does not parse: %v", err)
	}
}

// A key Go never declared cannot be enforced by Go — there is no field
// to read it into — so it can only ever be an instruction the skill
// reads out of this file and an agent chooses to follow. The file now
// says which kind each key is, because the two were indistinguishable
// on the page: an operator lowering `finding_batch` and an operator
// lowering `max_rounds` were doing different things, and only one of
// them changed what the tool does.
//
// This guards the honest half mechanically. A new skill-only key added
// without the tag fails here rather than joining the file as an
// apparent guarantee.
func TestAdvisorySettingsAreLabelled(t *testing.T) {
	tmplFS, err := EmbeddedTemplates()
	if err != nil {
		t.Fatalf("embedded templates: %v", err)
	}
	data, err := fs.ReadFile(tmplFS, ".bts/config/settings.yaml")
	if err != nil {
		t.Fatalf("read settings.yaml: %v", err)
	}

	// Every yaml tag engine.Settings declares. Anything absent from this
	// set has no Go field at all.
	src, err := os.ReadFile(filepath.Join("..", "engine", "settings.go"))
	if err != nil {
		t.Fatalf("read settings.go: %v", err)
	}
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`yaml:"([a-z_]+)"`).FindAllStringSubmatch(string(src), -1) {
		declared[m[1]] = true
	}
	if len(declared) < 10 {
		t.Fatalf("only %d yaml tags found — the scan broke, not the file", len(declared))
	}

	keyLine := regexp.MustCompile(`^\s{2,4}([a-z_]+):\s*\S`)
	var untagged []string
	for _, line := range strings.Split(string(data), "\n") {
		m := keyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		if declared[key] {
			continue // Go has a field; enforcement is a separate question
		}
		if !strings.Contains(line, "[advisory]") {
			untagged = append(untagged, key)
		}
	}
	if len(untagged) > 0 {
		t.Errorf("settings.yaml keys with no Go field must be marked [advisory]: %v\n"+
			"Either give the key a field in engine.Settings and read it somewhere, "+
			"or label it so nobody reads it as a guarantee.", untagged)
	}
}
