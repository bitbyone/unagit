package version

import (
	"runtime/debug"
	"testing"

	"github.com/tobola/unagit/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func TestADevBuildNamesItsCommit(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		settings []debug.BuildSetting
		want     string
	}{
		{nil, "dev"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "4f0c5bd1a2b3c4d5e6f7"}}, "dev (4f0c5bd1a2b3)"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "4f0c5bd"}, {Key: "vcs.modified", Value: "true"}}, "dev (4f0c5bd-dirty)"},
	} {
		if got := dev(c.settings); got != c.want {
			t.Errorf("dev(%v) = %q, want %q", c.settings, got, c.want)
		}
	}
}
