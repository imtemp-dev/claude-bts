package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// `bts validate` read illegal cells loosely (trial problem #68, Multica
// AGEN-49): a case-blind ILLEGAL anywhere in § 4 opened a label that ran to
// the end of the cell.  On true-shark that made a prose sentence containing
// "illegal" a cell and kept a trailing parenthetical in a label (two wrong
// errors), and the domain-model skill's own example row,
// `**ILLEGAL: double-fill** — prevented by …`, yielded a label that ran on
// into its prevention.  An earlier recipe tagged `[illegal-cell: ** — PHI
// in server logs]` because that was the label validate printed.
//
// A cell is declared only in a § 4 table row, by the uppercase marker
// followed by its label (`ILLEGAL: label`, or `**ILLEGAL** — label`).  The
// label ends where its bold ends or at a spaced dash, and a trailing
// parenthetical is a note, not part of the name: on both sides of the
// comparison.
const looseDomain = `## 4. Combinatorial State Space

Axes: contact filter, number filter.  Every illegal cell below names its prevention.
A refused cell is marked ILLEGAL: the row names what prevents it.

| # | Cell | Status | Outcome or prevention |
| --- | --- | --- | --- |
| 1 | list; live C | legal | only C's items |
| 2 | **ILLEGAL: token from an unfiltered walk pages a filtered request (or the reverse)** | ILLEGAL | prevented by INV-006 |
| 3 | dropping | legal | **ILLEGAL: double-fill** — prevented by Arrangement.place() atomic swap |
| 4 | the label becomes part of the unit id | **ILLEGAL** — PHI in server logs | prevented by deriving the id from the row |
| 5 | repeated filter | ILLEGAL: second filter value — refused at the binder | the first value binds |
| 6 | repeated number | legal (accepted risk) | the first value binds |

Every refused cell above names its prevention, so none is a spec gap.

## 5. Invariant Test Matrix

| INV-001 | ILLEGAL: not a section 4 cell | x |
`

func writeLooseRecipe(t *testing.T, final string) (domainPath, finalPath string) {
	t.Helper()
	dir := t.TempDir()
	domainPath = filepath.Join(dir, "domain.md")
	finalPath = filepath.Join(dir, "final.md")
	if err := os.WriteFile(domainPath, []byte(looseDomain), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(finalPath, []byte(final), 0644); err != nil {
		t.Fatal(err)
	}
	return domainPath, finalPath
}

func TestParseIllegalCells_ReadsOnlyDeclaredLabels(t *testing.T) {
	domainPath, _ := writeLooseRecipe(t, "")
	got, err := parseIllegalCells(domainPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"token from an unfiltered walk pages a filtered request",
		"double-fill",
		"PHI in server logs",
		"second filter value",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cells\n got %q\nwant %q", got, want)
	}
}

func TestCheckIllegalCellFalsifiers_TagsMatchDeclaredLabels(t *testing.T) {
	domainPath, finalPath := writeLooseRecipe(t, "## 2. Invariants and their falsifiers\n\n"+
		"| Invariant | Test |\n|---|---|\n"+
		"| INV-006 [illegal-cell: token from an unfiltered walk pages a filtered request] | t1 |\n"+
		"| INV-010 [illegal-cell: Double-fill] | t2 |\n"+
		"| INV-011 [illegal-cell: PHI in server logs] | t3 |\n"+
		"| INV-012 [illegal-cell: second  filter value (refused at the binder)] | t4 |\n")
	if issues := CheckIllegalCellFalsifiers(domainPath, finalPath); len(issues) != 0 {
		var claims []string
		for _, i := range issues {
			claims = append(claims, i.Claim)
		}
		t.Fatalf("every declared cell is tagged, so validate must be clean; got %q", claims)
	}
}

// A § 4 row marked ILLEGAL with no label declared no cell, so validate
// never asked for its falsifier (trial problem #74, Multica AGEN-51): relay
// r-001 rows 11, 12 and 15 were `**ILLEGAL**` alone, and their authors'
// tags, by row id or description, were read by nothing.  Validate names
// such a row as soon as domain.md exists.  A row whose label sits in one
// cell while its status column says ILLEGAL is labelled.
func TestValidate_ReportsAnIllegalRowWithNoLabel(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("recipe.json", `{"id":"r-1","type":"build","phase":"skeleton","topic":"x","created":"2026-11-05T00:00:00Z","updated":"2026-11-05T00:00:00Z"}`)
	write("domain.md", "## 4. Combinatorial State Space\n\n"+
		"| # | Cell | Status | Prevention |\n|---|---|---|---|\n"+
		"| 5 | **ILLEGAL: second issue for one PR** | ILLEGAL | INV-003 |\n"+
		"| 11 | any Effect with DRY_RUN on | **ILLEGAL** | INV-005 |\n"+
		"| 12 | a dry run left rows | legal | rows are read-only |\n")
	errs, err := ValidateRecipeDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range errs {
		if strings.Contains(e.Message, "unlabelled_illegal_cell") {
			got = append(got, e.Message)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0], "any Effect with DRY_RUN on") {
		t.Fatalf("exactly row 11 must be named, before final.md exists; got %q", got)
	}
}
