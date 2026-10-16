// Package version says which unagit this is.
package version

import "runtime/debug"

// Version is the release, set at build time from the tag without its v
// (-X github.com/tobola/unagit/internal/version.Version=1.2): the Homebrew
// formula and make build both pass it. A build from anywhere else is dev.
var Version = "dev"

// String is what unagit --version and unagit version print: the release
// alone, or for a development build the commit it was built from, so two
// dev binaries can still be told apart.
func String() string {
	if Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	return dev(info.Settings)
}

func dev(settings []debug.BuildSetting) string {
	var revision, modified string
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified == "true" {
		revision += "-dirty"
	}
	return "dev (" + revision + ")"
}
