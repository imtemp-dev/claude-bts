package cli

import (
	"reflect"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #100: swift-eagle's delta round printed "14 confirmed
// fixed" and then "not judged by this round's instruments (state
// unchanged): 14" with the same IDs.  A confirmed fix leaves every pending
// list, Unjudged included.
func TestAbsorbConfirmedFixed_LeavesNoPendingListHoldingAFixedID(t *testing.T) {
	sync := &state.SyncResult{
		Unjudged:   []string{"F-a", "F-b"},
		Unreported: []string{"F-c"},
		Restated:   []string{"F-d"},
	}
	absorbConfirmedFixed(sync, []string{"F-a", "F-c", "F-d"})
	if !reflect.DeepEqual(sync.Unjudged, []string{"F-b"}) {
		t.Errorf("Unjudged = %v, want [F-b]", sync.Unjudged)
	}
	if len(sync.Unreported) != 0 || len(sync.Restated) != 0 {
		t.Errorf("Unreported %v, Restated %v: want both empty", sync.Unreported, sync.Restated)
	}
	if !reflect.DeepEqual(sync.Fixed, []string{"F-a", "F-c", "F-d"}) {
		t.Errorf("Fixed = %v", sync.Fixed)
	}
	absorbConfirmedFixed(nil, []string{"F-a"}) // no ledger summary: nothing to do, no panic
}
