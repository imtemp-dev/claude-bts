package template

import (
	"io/fs"
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
