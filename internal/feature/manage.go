package feature

import (
	"fmt"
	"strings"
)

// ItemSpec is what item add and item edit take; a nil field is unchanged.
type ItemSpec struct {
	Title     *string
	Type      *string
	Express   *bool
	DependsOn *[]string
}

func checkDeps(f *Feature, self string, deps []string) error {
	for _, d := range deps {
		if d == self {
			return fmt.Errorf("%s cannot depend on itself", self)
		}
		if f.Item(d) == nil {
			return fmt.Errorf("no item %s in %s", d, f.ID)
		}
	}
	return nil
}

// AddItem appends an item, named i<N>.  Adding after a confirmation makes
// the plan pending until the next one.
func AddItem(dir string, spec ItemSpec) (string, error) {
	var id string
	_, err := Mutate(dir, "item-add", func(f *Feature) (string, error) {
		if f.Closed != nil {
			return "", fmt.Errorf("feature %s is %s", f.ID, f.Closed.Kind)
		}
		if spec.Title == nil || strings.TrimSpace(*spec.Title) == "" {
			return "", fmt.Errorf("an item needs a --title")
		}
		if spec.Type == nil || !validType(*spec.Type) {
			return "", fmt.Errorf("an item needs --type, one of %s", strings.Join(ItemTypes, ", "))
		}
		it := Item{Title: strings.TrimSpace(*spec.Title), Type: *spec.Type}
		if spec.Express != nil {
			it.Express = *spec.Express
		}
		if it.Express && it.Type != "build" {
			return "", fmt.Errorf("express is a mode of the build recipe")
		}
		max := 0
		for _, x := range f.Items {
			var k int
			if _, err := fmt.Sscanf(x.ID, "i%d", &k); err == nil && k > max {
				max = k
			}
		}
		it.ID = fmt.Sprintf("i%d", max+1)
		if spec.DependsOn != nil {
			if err := checkDeps(f, it.ID, *spec.DependsOn); err != nil {
				return "", err
			}
			it.DependsOn = *spec.DependsOn
		}
		f.Items = append(f.Items, it)
		id = it.ID
		return fmt.Sprintf("%s: %s (%s)", it.ID, it.Title, it.Type), nil
	})
	return id, err
}

func openFeature(f *Feature) error {
	if f.Closed != nil {
		return fmt.Errorf("feature %s is %s", f.ID, f.Closed.Kind)
	}
	return nil
}

// EditItem changes an item's plan fields.
func EditItem(dir, id string, spec ItemSpec) error {
	_, err := Mutate(dir, "item-edit", func(f *Feature) (string, error) {
		if err := openFeature(f); err != nil {
			return "", err
		}
		it := f.Item(id)
		if it == nil {
			return "", fmt.Errorf("no item %s in %s", id, f.ID)
		}
		if spec.Title != nil {
			it.Title = strings.TrimSpace(*spec.Title)
		}
		if spec.Type != nil {
			if !validType(*spec.Type) {
				return "", fmt.Errorf("--type must be one of %s", strings.Join(ItemTypes, ", "))
			}
			it.Type = *spec.Type
		}
		if spec.Express != nil {
			it.Express = *spec.Express
		}
		if it.Express && it.Type != "build" {
			return "", fmt.Errorf("express is a mode of the build recipe")
		}
		if spec.DependsOn != nil {
			if err := checkDeps(f, id, *spec.DependsOn); err != nil {
				return "", err
			}
			it.DependsOn = *spec.DependsOn
		}
		if _, err := stages(f.Plan()); err != nil {
			return "", err
		}
		return id, nil
	})
	return err
}

// RemoveItem drops an item no one has started and no item depends on.
func RemoveItem(dir, id string) error {
	_, err := Mutate(dir, "item-remove", func(f *Feature) (string, error) {
		if err := openFeature(f); err != nil {
			return "", err
		}
		it := f.Item(id)
		if it == nil {
			return "", fmt.Errorf("no item %s in %s", id, f.ID)
		}
		if len(Attempts(dir, it)) > 0 {
			return "", fmt.Errorf("%s has attempts; skip it instead (bts feature item skip)", id)
		}
		for _, x := range f.Items {
			for _, d := range x.DependsOn {
				if d == id {
					return "", fmt.Errorf("%s depends on %s", x.ID, id)
				}
			}
		}
		var kept []Item
		for _, x := range f.Items {
			if x.ID != id {
				kept = append(kept, x)
			}
		}
		f.Items = kept
		return id, nil
	})
	return err
}

// DecideItem records skip or defer, with the user's words.  A skipped item
// counts as settled for its dependents, so it is refused while an attempt
// at the item is still open.
func DecideItem(dir, id, kind, answer string) error {
	if strings.TrimSpace(answer) == "" {
		return fmt.Errorf("--answer must carry the user's words")
	}
	_, err := Mutate(dir, "item-"+kind, func(f *Feature) (string, error) {
		if err := openFeature(f); err != nil {
			return "", err
		}
		it := f.Item(id)
		if it == nil {
			return "", fmt.Errorf("no item %s in %s", id, f.ID)
		}
		if atts := Attempts(dir, it); len(atts) > 0 && atts[len(atts)-1].Open() {
			return "", fmt.Errorf("%s has an open attempt (a%d); let it finish, or release it first (bts feature item release)", id, atts[len(atts)-1].N)
		}
		it.Decision = &Decision{Kind: kind, Answer: strings.TrimSpace(answer), At: now()}
		return fmt.Sprintf("%s: %s", id, answer), nil
	})
	return err
}

// RestoreItem undoes a skip or defer, with the user's words.
func RestoreItem(dir, id, answer string) error {
	if strings.TrimSpace(answer) == "" {
		return fmt.Errorf("--answer must carry the user's words")
	}
	_, err := Mutate(dir, "item-restore", func(f *Feature) (string, error) {
		if err := openFeature(f); err != nil {
			return "", err
		}
		it := f.Item(id)
		if it == nil {
			return "", fmt.Errorf("no item %s in %s", id, f.ID)
		}
		if it.Decision == nil {
			return "", fmt.Errorf("%s is neither skipped nor deferred", id)
		}
		was := it.Decision.Kind
		it.Decision = nil
		return fmt.Sprintf("%s restored from %s: %s", id, was, answer), nil
	})
	return err
}

// ReleaseItem frees the item's open attempt, whose worktree or run is gone,
// so the item can be claimed again.
func ReleaseItem(dir, id, answer string) (int, error) {
	if strings.TrimSpace(answer) == "" {
		return 0, fmt.Errorf("--answer must carry the user's words")
	}
	var n int
	_, err := Mutate(dir, "item-release", func(f *Feature) (string, error) {
		it := f.Item(id)
		if it == nil {
			return "", fmt.Errorf("no item %s in %s", id, f.ID)
		}
		atts := Attempts(dir, it)
		if len(atts) == 0 || !atts[len(atts)-1].Open() {
			return "", fmt.Errorf("%s has no open attempt to release", id)
		}
		n = atts[len(atts)-1].N
		it.Released = append(it.Released, Release{Attempt: n, Answer: strings.TrimSpace(answer), At: now()})
		return fmt.Sprintf("%s a%d: %s", id, n, answer), nil
	})
	return n, err
}

// Close ends the feature: "done" once every confirmed item is done, skipped
// or deferred; "cancelled" at any time.
func Close(dir, kind, answer string) error {
	if strings.TrimSpace(answer) == "" {
		return fmt.Errorf("--answer must carry the user's words")
	}
	_, err := Mutate(dir, kind, func(f *Feature) (string, error) {
		if f.Closed != nil {
			return "", fmt.Errorf("feature %s is already %s", f.ID, f.Closed.Kind)
		}
		if kind == "done" {
			s, err := Derive(dir, f)
			if err != nil {
				return "", err
			}
			if s.State != FeatureReady {
				return "", fmt.Errorf("feature %s is %s, not ready for review: every confirmed item must be done, skipped or deferred", f.ID, s.State)
			}
		}
		f.Closed = &Decision{Kind: kind, Answer: strings.TrimSpace(answer), At: now()}
		return answer, nil
	})
	return err
}

// Change is one item's news since the manager's last reconcile.
type Change struct {
	Item    string
	Attempt int
	Text    string
}

// Reconcile reports what changed in each item's latest attempt since the
// last reconcile, and moves the cursor.  It records nothing else: item state
// is derived; the manager's judgement goes to the documents and decisions.
func Reconcile(dir string) ([]Change, error) {
	var changes []Change
	_, err := Mutate(dir, "reconcile", func(f *Feature) (string, error) {
		if f.Seen == nil {
			f.Seen = map[string]Seen{}
		}
		for i := range f.Items {
			it := &f.Items[i]
			atts := Attempts(dir, it)
			if len(atts) == 0 {
				continue
			}
			a := atts[len(atts)-1]
			hash := ""
			text := fmt.Sprintf("a%d claimed", a.N)
			if a.Manifest != nil {
				hash = a.Manifest.ContentHash
				text = fmt.Sprintf("a%d %s", a.N, a.Manifest.Phase)
				if a.Manifest.Outcome != "" {
					text += " (" + a.Manifest.Outcome
					if a.Manifest.CompletedVia != "" {
						text += " via " + a.Manifest.CompletedVia
					}
					text += ")"
				}
			}
			if a.Released != nil {
				text = fmt.Sprintf("a%d released", a.N)
			}
			prev, ok := f.Seen[it.ID]
			if ok && prev.Attempt == a.N && prev.ContentHash == hash {
				continue
			}
			changes = append(changes, Change{Item: it.ID, Attempt: a.N, Text: text})
			f.Seen[it.ID] = Seen{Attempt: a.N, ContentHash: hash, At: now()}
		}
		if len(changes) == 0 {
			return "", ErrNoChange
		}
		return fmt.Sprintf("%d change(s)", len(changes)), nil
	})
	return changes, err
}
