package engine

import "testing"

// Trial problem #140: a change to comments, docstrings or blank lines is
// not a change to the code; anything bts cannot read with certainty is.
func TestCodeEquivalent(t *testing.T) {
	cases := []struct {
		name, path, a, b string
		same             bool
	}{
		{"python line comment", "m.py", "x = 1\n# old note\ny = 2\n", "x = 1\n# new note, longer\n\ny = 2\n", true},
		{"python trailing comment", "m.py", "x = 1  # old\n", "x = 1    # new\n", true},
		{"python module docstring", "m.py", "\"\"\"Old summary.\"\"\"\nimport os\n", "\"\"\"New summary.\n\nMore.\n\"\"\"\nimport os\n", true},
		{"python function docstring", "m.py", "def f():\n    '''Old.'''\n    return 1\n", "def f():\n    '''New,\n    two lines.'''\n    return 1\n", true},
		{"python field comment block", "m.py", "class Data:\n    # Warnings the\n    # validator found.\n    warnings: list\n", "class Data:\n    # Warnings, in order.\n    warnings: list\n", true},
		{"python hash inside a string", "m.py", "s = \"a # b\"\n", "s = \"a # c\"\n", false},
		{"python assigned triple string", "m.py", "q = \"\"\"select 1\"\"\"\n", "q = \"\"\"select 2\"\"\"\n", false},
		{"python string inside a call", "m.py", "f(\n    \"\"\"one\"\"\"\n)\n", "f(\n    \"\"\"two\"\"\"\n)\n", false},
		{"python continued string", "m.py", "q = \\\n    \"\"\"one\"\"\"\n", "q = \\\n    \"\"\"two\"\"\"\n", false},
		{"python string then a call", "m.py", "\"\"\"a{}\"\"\".format(x)\n", "\"\"\"b{}\"\"\".format(x)\n", false},
		{"python indentation", "m.py", "if x:\n    y()\nz()\n", "if x:\n    y()\n    z()\n", false},
		{"python code", "m.py", "x = 1\n", "x = 2\n", false},
		{"go comments", "m.go", "// A is a.\nfunc A() int { return 1 } // one\n/* block\n note */\n", "// A returns a.\nfunc A() int { return 1 } // the one\n", true},
		{"go alignment", "m.go", "type T struct {\n\tA int // a\n\tBB string\n}\n", "type T struct {\n\tA  int    // a, longer\n\tBB string // b\n}\n", true},
		{"go build directive", "m.go", "//go:build linux\npackage m\n", "//go:build darwin\npackage m\n", false},
		{"go slashes in a string", "m.go", "var u = \"http://a\"\n", "var u = \"http://b\"\n", false},
		{"go raw string", "m.go", "var r = `a\\` + x // c\n", "var r = `a\\` + y // c\n", false},
		{"js regex with an escaped slash", "m.js", "s.replace(/\\//g, \"-\")\n", "s.replace(/\\//g, \"+\")\n", false},
		{"js template with slashes", "m.ts", "const u = `//${a}`\n", "const u = `//${b}`\n", false},
		{"js comment", "m.ts", "const a = 1 // one\n", "const a = 1 // uno\n", true},
		{"js regex holding a backtick", "m.ts", "const re = /`(x)`/;\nconst u = `a // ${b}`;\n", "const re = /`(x)`/;\nconst u = `a // ${c}`;\n", false},
		{"js regex holding a quote, then a comment", "m.ts", "s.replace(/'/g, \"\") // one\n", "s.replace(/'/g, \"\") // two\n", true},
		{"js division is not a regex", "m.ts", "const r = a / b; // c / d\n", "const r = a / b; // e / f\n", true},
		{"jsx url text", "m.tsx", "<a>https://example.com/one</a>\n", "<a>https://example.com/two</a>\n", false},
		{"python blank line inside a string", "m.py", "P = \"\"\"a\n\nb\"\"\"\n", "P = \"\"\"a\nb\"\"\"\n", false},
		{"sql comment", "m.sql", "select 1 -- one\n", "select 1 -- uno\n", true},
		{"sql quote", "m.sql", "select 'it''s -- a'\n", "select 'it''s -- b'\n", false},
		{"yaml is not read", "m.yaml", "a: 1 # one\n", "a: 1 # uno\n", false},
		{"unknown blank lines", "m.txt2", "a\n\nb\n", "a\nb\n", true},
	}
	for _, c := range cases {
		if got := CodeEquivalent(c.path, []byte(c.a), []byte(c.b)); got != c.same {
			t.Errorf("%s: CodeEquivalent = %v, want %v\n  a: %q\n  b: %q\n  code a: %q\n  code b: %q", c.name, got, c.same,
				c.a, c.b, codeLines(c.path, []byte(c.a)), codeLines(c.path, []byte(c.b)))
		}
	}
}
