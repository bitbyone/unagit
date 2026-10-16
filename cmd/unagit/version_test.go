package main

import (
	"bytes"
	"testing"

	"github.com/tobola/unagit/internal/version"
)

// Both ways of asking print the version and nothing else, so a script or the
// formula's test can compare it with the tag.
func TestVersionPrintsTheVersionAlone(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--version"}, {"version"}} {
		var out bytes.Buffer
		root := rootCmd()
		root.SetOut(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("unagit %v: %v", args, err)
		}
		if got, want := out.String(), version.String()+"\n"; got != want {
			t.Errorf("unagit %v printed %q, want %q", args, got, want)
		}
	}
}
