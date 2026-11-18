package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/review"
)

// A coordinator settles a feature with no recipe of its own: while the
// feature's page waits for the user, the prompt carries a reminder of it,
// and once the user has confirmed what the page shows, nothing.
func TestFeatureReviewReminders_WhileTheFeatureWaitsForTheUser(t *testing.T) {
	root := setupPreCompactRoot(t)
	t.Setenv(feature.StoreEnv, t.TempDir())
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "attach files", Repo: &feature.Repo{Key: "local/rm-1"}, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nAttach files on GPT.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	title, typ := "existing chat on GPT", "debug"
	if _, err := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &typ}); err != nil {
		t.Fatal(err)
	}
	if got := featureReviewReminders(root); len(got) != 0 {
		t.Errorf("no page, no reminder; got %v", got)
	}
	sub, err := feature.ReviewSubject(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub.Work, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := review.Attach(sub, "https://claude.ai/artifact/f", ""); err != nil {
		t.Fatal(err)
	}
	got := featureReviewReminders(root)
	if len(got) != 1 || !strings.Contains(got[0], f.ID) || !strings.Contains(got[0], "https://claude.ai/artifact/f") ||
		!strings.Contains(got[0], "/bts-review-page § Feature") {
		t.Fatalf("an unconfirmed feature with a page reminds of it, got %v", got)
	}
	out, err := NewUserPromptSubmitHandler().Handle(&HookInput{CWD: root, HookEventName: string(EventUserPromptSubmit)})
	if err != nil || out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, f.ID) {
		t.Errorf("a session with no active recipe still gets the feature's reminder, got %+v %v", out, err)
	}
	if _, err := feature.Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	if got := featureReviewReminders(root); len(got) != 0 {
		t.Errorf("confirmed with nothing open, no reminder; got %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nAttach images and PDFs on GPT.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := featureReviewReminders(root); len(got) != 1 {
		t.Errorf("an intent changed since the confirmation waits for the user again, got %v", got)
	}
}
