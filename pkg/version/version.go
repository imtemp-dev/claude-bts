package version

import "fmt"

// Set by LDFLAGS at build time
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func GetVersion() string {
	return Version
}

func GetFullVersion() string {
	return fmt.Sprintf("%s (commit: %s, date: %s)", Version, Commit, Date)
}
