// Package version holds build-time identification, set with -ldflags.
package version

var (
	// Version is the git describe output of the build.
	Version = "dev"
	// Commit is the short git SHA of the build.
	Commit = "none"
)
