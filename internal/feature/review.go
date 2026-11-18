package feature

import (
	"encoding/json"

	"github.com/imtemp-dev/claude-bts/internal/review"
)

// ReviewSubject is the feature's review page as the review package records
// it: the record beside intent.md and contracts.md in the store, the page
// and its uploads in root, the bts project of the session that reviews it.
// The plan is fingerprinted as it stands at each read, so an item added or
// edited after the page was built shows as a new version.
func ReviewSubject(root, dir string) (review.Subject, error) {
	f, err := Load(dir)
	if err != nil {
		return review.Subject{}, err
	}
	plan := func() (string, error) {
		cur, err := Load(dir)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(cur.Plan())
		return string(b), err
	}
	return review.FeatureSubject(root, dir, f.ID, plan), nil
}

// NeedsConfirmation reports whether the feature's intent, contracts and
// plan as they stand still wait for the user: never confirmed, or changed
// since the latest confirmation.
func NeedsConfirmation(dir string, f *Feature) bool {
	return len(f.Confirmations) == 0 || len(Pending(dir, f)) > 0
}
