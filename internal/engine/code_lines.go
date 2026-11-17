package engine

import (
	"path/filepath"
	"strings"
)

// Code lines: what of a source file can change behaviour.  The rescope
// rule of interrogate_clean counts a file only when its code changed, not
// its comments, docstrings or blank lines (trial problem #140: a four-line
// comment edit forced a 35-minute round).
//
// The reading is deliberately small and errs towards code: a language it
// does not know keeps every non-blank line, and so do the parts of a known
// one it cannot read with certainty (a Go cgo preamble, a JavaScript regex
// holding a quote).  Mistaking a comment for code costs a round; mistaking
// code for a comment would let a change through unattacked.

// commentSyntax is how one language family writes comments and strings.
type commentSyntax struct {
	line       []string // line comment openers
	blockOpen  string   // block comment opener, "" for none
	blockClose string
	quotes     string // characters that open a one-line string
	multiQuote string // characters that open a string spanning lines (Go raw, JS template)
	rawMulti   bool   // a backslash escapes nothing in the multi-line string (Go raw strings)
	triple     bool   // Python's triple-quoted strings
	docstrings bool   // a bare string statement is documentation
	sqlQuotes  bool   // '' inside a string is an escaped quote, and \ is not an escape
	regex      bool   // a slash where a value may start opens a regex literal (JavaScript)
}

var (
	pythonSyntax = commentSyntax{line: []string{"#"}, quotes: `"'`, triple: true, docstrings: true}
	cSyntax      = commentSyntax{line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"'`}
	goSyntax     = commentSyntax{line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"'`, multiQuote: "`", rawMulti: true}
	jsSyntax     = commentSyntax{line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"'`, multiQuote: "`", regex: true}
	sqlSyntax    = commentSyntax{line: []string{"--"}, blockOpen: "/*", blockClose: "*/", quotes: `'"`, sqlQuotes: true}
)

// syntaxFor picks the comment syntax for a path, nil when bts does not
// read the language's comments.
func syntaxFor(path string) *commentSyntax {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".pyi", ".pyw":
		return &pythonSyntax
	case ".go":
		return &goSyntax
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return &jsSyntax
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".cs", ".java", ".kt", ".kts", ".scala", ".swift", ".rs", ".dart":
		return &cSyntax
	case ".sql":
		return &sqlSyntax
	}
	return nil
}

// CodeEquivalent reports whether a and b, two versions of the file at
// path, hold the same code: they differ at most in comments, docstrings,
// blank lines and whitespace between tokens.  Indentation is code.
func CodeEquivalent(path string, a, b []byte) bool {
	la, lb := codeOnly(codeLines(path, a)), codeOnly(codeLines(path, b))
	if len(la) != len(lb) {
		return false
	}
	for i := range la {
		if la[i] != lb[i] {
			return false
		}
	}
	return true
}

func codeOnly(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// codeLines returns, line by line, the code of a file with comments and
// docstrings removed, whitespace between tokens collapsed and trailing
// whitespace trimmed; "" for a line that holds no code.  Line i of the
// result is line i+1 of the file.
func codeLines(path string, content []byte) []string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	syn := syntaxFor(path)
	if syn == nil {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = strings.TrimRight(l, " \t")
		}
		return out
	}
	return (&codeScanner{syn: syn, path: path}).scan(lines)
}

// codeScanner walks a file character by character, keeping code and
// strings and dropping comments.
type codeScanner struct {
	syn  *commentSyntax
	path string
	out  []strings.Builder
	// inStr marks a line that starts or ends inside a string: its text,
	// blank or trailing whitespace included, is the string's value.
	inStr []bool
	// doc marks a line a docstring covered, which holds no code.
	doc []bool

	inBlock   bool   // inside a block comment
	inString  string // the closing quote of the open string, "" outside one
	multiline bool   // the open string may span lines
	depth     int    // bracket depth, for Python's docstring test
	cont      bool   // the previous line ended in a backslash
	prevTail  string // the code of the last line that held any, for the regex test

	// A bare string statement that started this logical line, for
	// Python: the line it started on and how much code that line held
	// before it.  docLine is -1 when none is open.
	docLine, docMark int
}

func (s *codeScanner) scan(lines []string) []string {
	s.out = make([]strings.Builder, len(lines))
	s.inStr = make([]bool, len(lines))
	s.doc = make([]bool, len(lines))
	s.docLine = -1
	for i, line := range lines {
		started := s.inString != ""
		s.scanLine(i, line)
		if started || s.inString != "" {
			s.inStr[i] = true
		}
	}
	res := make([]string, len(lines))
	for i := range s.out {
		switch {
		case s.inStr[i] && !s.doc[i]:
			// Never empty: a blank line inside a string is part of it.
			res[i] = "\x00" + s.out[i].String()
		default:
			res[i] = strings.TrimRight(s.out[i].String(), " \t")
		}
	}
	return res
}

func (s *codeScanner) scanLine(i int, line string) {
	b := &s.out[i]
	// A Go build or compiler directive and a shebang are comments that
	// change behaviour: keep them as code.
	trimmed := strings.TrimSpace(line)
	if s.inString == "" && !s.inBlock {
		if strings.HasPrefix(trimmed, "//go:") || strings.HasPrefix(trimmed, "// +build") || (i == 0 && strings.HasPrefix(trimmed, "#!")) {
			b.WriteString(trimmed)
			return
		}
	}
	lineStart := s.inString == "" && !s.inBlock && s.depth == 0 && !s.cont
	s.cont = false
	j := 0
	// Indentation is code in Python and harmless elsewhere: keep it.
	if s.inString == "" && !s.inBlock {
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		b.WriteString(line[:j])
	}
	space := false // whitespace seen since the last code character
	emit := func(str string) {
		if space && b.Len() > 0 {
			last := b.String()[b.Len()-1]
			if last != ' ' && last != '\t' {
				b.WriteByte(' ')
			}
		}
		space = false
		b.WriteString(str)
	}
	for j < len(line) {
		c := line[j]
		switch {
		case s.inBlock:
			if strings.HasPrefix(line[j:], s.syn.blockClose) {
				s.inBlock = false
				j += len(s.syn.blockClose)
				space = true
				continue
			}
			j++
		case s.inString != "":
			if strings.HasPrefix(line[j:], s.inString) {
				if s.syn.sqlQuotes && len(s.inString) == 1 && strings.HasPrefix(line[j+1:], s.inString) {
					b.WriteString(s.inString + s.inString)
					j += 2
					continue
				}
				b.WriteString(s.inString)
				j += len(s.inString)
				s.closeString(i, line[j:])
				continue
			}
			if c == '\\' && !s.syn.sqlQuotes && (!s.multiline || !s.syn.rawMulti) && j+1 < len(line) {
				b.WriteString(line[j : j+2])
				j += 2
				continue
			}
			b.WriteByte(c)
			j++
		case c == ' ' || c == '\t':
			space = true
			j++
		case c == '\\' && j+1 < len(line):
			// Outside a string a backslash escapes the next character only
			// in a regex literal (`/\//g`): keep the pair whole, so the
			// escaped slash cannot open a comment.
			emit(line[j : j+2])
			j += 2
		case s.lineComment(line[j:]) && (j == 0 || line[j-1] != ':'):
			// `//` right after a colon is a URL (`https://` in JSX text),
			// not a comment.
			j = len(line)
		case s.syn.blockOpen != "" && strings.HasPrefix(line[j:], s.syn.blockOpen):
			s.inBlock = true
			j += len(s.syn.blockOpen)
			space = true
		case s.syn.regex && c == '/' && s.regexMayStart(b.String()):
			end := regexEnd(line, j)
			if end < 0 {
				emit("/")
				j++
				continue
			}
			// A regex literal is code; reading it whole keeps a quote or a
			// backtick inside it from opening a string.
			emit(line[j:end])
			j = end
		case s.syn.triple && (strings.HasPrefix(line[j:], `"""`) || strings.HasPrefix(line[j:], `'''`)):
			s.openString(i, line[:j], lineStart)
			emit(line[j : j+3])
			s.inString, s.multiline = line[j:j+3], true
			j += 3
		case strings.IndexByte(s.syn.quotes, c) >= 0 || strings.IndexByte(s.syn.multiQuote, c) >= 0:
			s.openString(i, line[:j], lineStart)
			emit(string(c))
			s.inString, s.multiline = string(c), strings.IndexByte(s.syn.multiQuote, c) >= 0
			j++
		default:
			switch c {
			case '(', '[', '{':
				s.depth++
			case ')', ']', '}':
				if s.depth > 0 {
					s.depth--
				}
			}
			emit(string(c))
			j++
		}
	}
	if s.inString != "" && !s.multiline {
		// A one-line string cannot run past its line: the file is not
		// what this reading expects, so stop treating what follows as a
		// string.
		s.inString = ""
		s.docLine = -1
	}
	if t := strings.TrimSpace(b.String()); t != "" {
		s.prevTail = t
	}
	if s.inString == "" && !s.inBlock && strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") {
		s.cont = true
	}
}

// regexKeywords are the words after which a slash starts a regex literal.
var regexKeywords = map[string]bool{
	"return": true, "typeof": true, "case": true, "do": true, "else": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "instanceof": true, "yield": true, "await": true,
}

// regexMayStart reports whether a slash after the code emitted so far on
// the line (or, when there is none, the last line that had code) starts
// a regex literal rather than a division: the usual test on the token
// before it.
func (s *codeScanner) regexMayStart(emitted string) bool {
	tail := strings.TrimRight(emitted, " \t")
	if strings.TrimSpace(tail) == "" {
		tail = s.prevTail
	}
	if tail == "" {
		return true
	}
	last := tail[len(tail)-1]
	if strings.IndexByte("(,=:[!&|?{};+-*%<>~^}", last) >= 0 {
		return true
	}
	if isIdentByte(last) {
		k := len(tail)
		for k > 0 && isIdentByte(tail[k-1]) {
			k--
		}
		return regexKeywords[tail[k:]]
	}
	return false
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// regexEnd returns the index just past the slash that closes a regex
// literal opening at j, or -1 when none closes it on the line.
func regexEnd(line string, j int) int {
	inClass := false
	for k := j + 1; k < len(line); k++ {
		switch line[k] {
		case '\\':
			k++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				return k + 1
			}
		}
	}
	return -1
}

// lineComment reports whether a line comment opens at the start of rest.
func (s *codeScanner) lineComment(rest string) bool {
	for _, open := range s.syn.line {
		if strings.HasPrefix(rest, open) {
			return true
		}
	}
	return false
}

// openString notes a string that may be a docstring: in Python, a string
// that opens a logical line, with only its prefix letters before it.
func (s *codeScanner) openString(i int, before string, lineStart bool) {
	if !s.syn.docstrings || !lineStart {
		return
	}
	lead := strings.TrimLeft(before, " \t")
	if len(lead) > 2 || strings.Trim(lead, "rRbBuUfF") != "" {
		return
	}
	s.docLine = i
	s.docMark = s.out[i].Len() - len(lead)
	if s.docMark < 0 {
		s.docMark = 0
	}
}

// closeString ends a string at line i; rest is what follows it on the
// line.  A docstring candidate is documentation when nothing but a
// comment follows it, and its text is dropped from the code.
func (s *codeScanner) closeString(i int, rest string) {
	s.inString, s.multiline = "", false
	if s.docLine < 0 {
		return
	}
	start := s.docLine
	s.docLine = -1
	tail := strings.TrimSpace(rest)
	if tail != "" && !s.lineComment(tail) {
		return
	}
	kept := s.out[start].String()[:s.docMark]
	s.out[start].Reset()
	s.out[start].WriteString(kept)
	s.doc[start] = true
	for k := start + 1; k <= i; k++ {
		s.out[k].Reset()
		s.doc[k] = true
	}
}
