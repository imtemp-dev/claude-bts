package feature

// ItemBranch is what a dispatcher decided and saw about an item's branch,
// whichever backend runs the item (Multica or Orca): the base it started
// from, the unmerged item it is stacked on, its merge, and the restack it
// was asked for.  It is embedded in each backend's item record, so its
// fields keep their JSON names there.
type ItemBranch struct {
	Base       string       `json:"base,omitempty"`        // the branch the item started from
	BaseItem   string       `json:"base_item,omitempty"`   // the unmerged item it is stacked on
	Merged     *MergeRecord `json:"merged,omitempty"`      // its pull request merged
	RestackFor string       `json:"restack_for,omitempty"` // the merged item a restack was asked for
}

// branchOf is item id's branch record under the backend that dispatched
// it, or nil when none did.  Each dispatcher refuses a feature the other
// backend runs, so an item has one record at most.
func branchOf(f *Feature, id string) *ItemBranch {
	if f == nil {
		return nil
	}
	if f.Orca != nil {
		if oi := f.Orca.Items[id]; oi != nil {
			return &oi.ItemBranch
		}
	}
	if f.Multica != nil {
		if mi := f.Multica.Items[id]; mi != nil {
			return &mi.ItemBranch
		}
	}
	return nil
}

// branchIDs lists, sorted, the items any backend dispatched.
func branchIDs(f *Feature) []string {
	if f == nil {
		return nil
	}
	seen := map[string]bool{}
	if f.Orca != nil {
		for id := range f.Orca.Items {
			seen[id] = true
		}
	}
	if f.Multica != nil {
		for id := range f.Multica.Items {
			seen[id] = true
		}
	}
	return sortedKeys(seen)
}

// reachedBase reports whether item id's work is on the feature's base
// branch: its pull request merged into the base, or into the branch of the
// item it was stacked on, once that item's work reached the base.
func reachedBase(f *Feature, id string) bool {
	for seen := map[string]bool{}; id != "" && !seen[id]; {
		seen[id] = true
		b := branchOf(f, id)
		if b == nil || b.Merged == nil {
			return false
		}
		if b.Merged.Into == "" || b.Merged.Into == f.BaseBranch {
			return true
		}
		if b.BaseItem == "" || b.Merged.Into != b.Base {
			return false
		}
		id = b.BaseItem
	}
	return false
}

// stackDepth is how many items not yet on the base branch an item and those
// under it make.
func stackDepth(f *Feature, id string) int {
	depth := 0
	for seen := map[string]bool{}; id != "" && !seen[id]; {
		seen[id] = true
		b := branchOf(f, id)
		if b == nil || reachedBase(f, id) {
			break
		}
		depth++
		id = b.BaseItem
	}
	return depth
}
