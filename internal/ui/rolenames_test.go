package ui

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestEveryRoleDrawnIsARole: role() answers the text colour for a name it
// does not know, so a base colour asked for by its name - surface.raised,
// state.good - came out as the text's, a white fill on a dim row. Every
// role("…") written in the code must name a role the theme resolves.
func TestEveryRoleDrawnIsARole(t *testing.T) {
	known := map[string]bool{}
	for _, r := range colourRoles {
		known[r.key] = true
	}
	// Some roles are made from others as a theme comes on (toastRoles).
	for key := range roleColours {
		known[key] = true
	}
	literal := regexp.MustCompile(`role\("([a-z_.]+)"\)`)
	entries, err := os.ReadDir(".")
	must(t, err)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		must(t, err)
		for _, m := range literal.FindAllStringSubmatch(string(b), -1) {
			if !known[m[1]] {
				t.Errorf("%s: role(%q) is no role - name the role, or add one with a fallback", e.Name(), m[1])
			}
		}
	}
}
