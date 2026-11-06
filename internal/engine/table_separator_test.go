package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Trial problem #112: neat-wolf's INV-003 held "---" in its statement, the
// invariants reader took the row for the table's separator, and INV-003
// vanished from every check that reads domain.md (the code fingerprint,
// ownership, per-task and architect checks).
func TestParseInvariantsTable_KeepsARowThatContainsDashes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "domain.md")
	_ = os.WriteFile(p, []byte("## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n"+
		"| INV-001 | One owner | A |\n"+
		"| INV-003 | A long PDF's text names its pages --- never a silent cut | B |\n"), 0644)
	invs, err := parseInvariantsTable(p)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, inv := range invs {
		ids = append(ids, inv.ID)
	}
	if want := []string{"INV-001", "INV-003"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
}

func TestParseDeviationMd_KeepsARowThatContainsDashes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "deviation.md")
	_ = os.WriteFile(p, []byte("## Spec Additions\n\n"+
		"| ID | Item | File | Driver | Severity | Description |\n"+
		"|----|------|------|--------|----------|-------------|\n"+
		"| D-001 | Path guard --- before any write | a.py | review:MIN-1 | minor | refuses |\n"), 0644)
	rows, err := ParseDeviationMd(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "D-001" {
		t.Fatalf("want the one row D-001, got %+v", rows)
	}
}
