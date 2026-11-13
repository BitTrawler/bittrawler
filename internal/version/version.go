// Package version reports the build the binary was made from.
package version

// Version, Commit and Date are replaced at build time with -ldflags. When the
// binary was not built by the release process they keep their development
// values.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info identifies a build.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Current returns the build information.
func Current() Info {
	return Info{Version: Version, Commit: Commit, Date: Date}
}
