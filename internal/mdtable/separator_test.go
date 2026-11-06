package mdtable

import "testing"

// A separator row holds nothing but pipes, dashes, colons and spaces.  A
// data row whose text contains "---" is not one (trial problem #112).
func TestIsSeparatorRow(t *testing.T) {
	cases := map[string]bool{
		"|---|---|":              true,
		"| --- | :---: | ---: |": true,
		"|:--|--:|":              true,
		"  |----|------|  ":      true,
		"| INV-003 | pages 1 --- 15 are read | Tool |": false,
		"| D-002 | Guard --- path | a.py |":            false,
		"| --- note |":                                 false,
		"|  |  |":                                      false,
		"":                                             false,
		"---":                                          false,
	}
	for line, want := range cases {
		if got := IsSeparatorRow(line); got != want {
			t.Errorf("IsSeparatorRow(%q) = %v, want %v", line, got, want)
		}
	}
}
