package comment

// CommentSummary is the manifest-shaped aggregate of parsed comments.
type CommentSummary struct {
	OpenByDoc     map[string]int
	BlockingByDoc map[string]int
	TotalOpen     int
	TotalBlocking int
	ByKind        map[Kind]int
}

// Summarize aggregates a list of comments into per-doc counts.
// Free-form comments are counted in OpenByDoc but never in BlockingByDoc.
func Summarize(comments []Comment) CommentSummary {
	s := CommentSummary{
		OpenByDoc:     map[string]int{},
		BlockingByDoc: map[string]int{},
		ByKind:        map[Kind]int{},
	}
	for _, c := range comments {
		s.OpenByDoc[c.File]++
		s.TotalOpen++
		s.ByKind[c.Kind]++
		if c.Kind == KindBlock {
			s.BlockingByDoc[c.File]++
			s.TotalBlocking++
		}
	}
	return s
}

// CountBlockingComments is the cheap path used by the stop hook — parses
// once and returns just the blocking count without persisting anything.
//
// Returns the parse error (rather than swallowing as 0) so callers can
// surface "we couldn't check" instead of silently treating an
// unreadable recipe directory as all-clear.
func CountBlockingComments(recipeDir string) (int, error) {
	cs, err := ParseRecipe(recipeDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range cs {
		if c.Kind == KindBlock {
			n++
		}
	}
	return n, nil
}
