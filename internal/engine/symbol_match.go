package engine

import "regexp"

// ContainsSymbol reports whether content names sym as a whole token: not
// as part of a longer identifier. A boundary is required only on a side
// where sym starts or ends with a word character, so a path (".venv/"), a
// flag ("--dry-run") or a dotted name matches too; `\b` on both sides
// never matched those, because `\b` needs a word character next to it.
func ContainsSymbol(content, sym string) bool {
	if sym == "" {
		return false
	}
	return symbolRegexp(sym).MatchString(content)
}

// symbolRegexp is ContainsSymbol's pattern for a non-empty sym, compiled
// once for a caller that matches it against many texts.
func symbolRegexp(sym string) *regexp.Regexp {
	pattern := regexp.QuoteMeta(sym)
	if isWordByte(sym[0]) {
		pattern = `(?:^|[^A-Za-z0-9_])` + pattern
	}
	if isWordByte(sym[len(sym)-1]) {
		pattern += `(?:$|[^A-Za-z0-9_])`
	}
	return regexp.MustCompile(`(?m)` + pattern)
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
