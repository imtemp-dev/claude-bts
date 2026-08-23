package metrics

import (
	"os"
	"path/filepath"
	"time"

	"github.com/imtemp-dev/jig/internal/state"
)

const tokenSnapshotInterval = 30 * time.Second

// ShouldEmitTokenSnapshot returns true if enough time has passed since
// the last token snapshot. Uses mtime of a sentinel file as a clock.
func ShouldEmitTokenSnapshot(root string) bool {
	sentinel := filepath.Join(state.LocalPath(root), ".metrics-token-ts")
	info, err := os.Stat(sentinel)
	if err != nil {
		return true // no sentinel = first time
	}
	return time.Since(info.ModTime()) > tokenSnapshotInterval
}

// TouchTokenSentinel updates the sentinel file mtime.
func TouchTokenSentinel(root string) {
	sentinel := filepath.Join(state.LocalPath(root), ".metrics-token-ts")
	f, err := os.Create(sentinel)
	if err == nil {
		f.Close()
	}
}
