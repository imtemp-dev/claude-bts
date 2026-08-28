package engine

import "testing"

// verification.md files written before the jig rebrand carry <bts-findings>.
// Re-verifying such a recipe must still see its findings — reading zero would
// report a clean document and let the completion gate pass on unresolved work.
func TestFindingsAndDecisionBlocksAcceptLegacySpelling(t *testing.T) {
	body := `{"critical":1,"major":0,"minor":2}`

	for _, tag := range []string{"jig", "bts"} {
		t.Run("findings/"+tag, func(t *testing.T) {
			doc := "prose\n<" + tag + "-findings>\n" + body + "\n</" + tag + "-findings>\nmore"
			m := findingsBlockRe.FindAllStringSubmatch(doc, -1)
			if len(m) != 1 {
				t.Fatalf("matched %d blocks, want 1", len(m))
			}
			if m[0][1] != body {
				t.Errorf("captured %q, want %q", m[0][1], body)
			}
		})
		t.Run("decision/"+tag, func(t *testing.T) {
			doc := "<" + tag + "-decision>\n" + body + "\n</" + tag + "-decision>"
			m := decisionBlockRe.FindAllStringSubmatch(doc, -1)
			if len(m) != 1 {
				t.Fatalf("matched %d blocks, want 1", len(m))
			}
		})
	}

	t.Run("unrelated tag is not a findings block", func(t *testing.T) {
		if findingsBlockRe.MatchString("<other-findings>" + body + "</other-findings>") {
			t.Error("matched a tag that is not one of ours")
		}
	})
}
