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

// GetTemplateVersion returns the version string used for template tracking.
// Format: "Version-CommitShort" (e.g., "dev-65b0629")
func GetTemplateVersion() string {
	if Commit != "none" && len(Commit) >= 7 {
		return Version + "-" + Commit[:7]
	}
	return Version
}
