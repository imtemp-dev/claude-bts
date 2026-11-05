package engine

import (
	"os"
	"path/filepath"
	"reflect"
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
