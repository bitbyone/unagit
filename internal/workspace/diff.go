package workspace

import (
	"fmt"
	"strings"
)

// ChangeBase is the commit a directory's change is measured from: where its
// branch left base, when base is known and still there; HEAD otherwise, so
// that what is not committed is the change.
func (m *Manager) ChangeBase(dir, base string) string {
	if base != "" {
		if onto := m.git.BaseRef(dir, base); onto != "" {
			if mb, err := m.trimmed(dir, "merge-base", "HEAD", onto); err == nil && mb != "" {
				return mb
			}
		}
	}
	return "HEAD"
}

// ChangePatch is everything a working tree holds against from - committed or
// not, untracked files included - as one patch whose paths start with prefix,
// so that the patches of several repositories can be read as one.
func (m *Manager) ChangePatch(dir, prefix, from string) (string, error) {
	prefixes := []string{"--src-prefix=a/" + prefix, "--dst-prefix=b/" + prefix}
	var out strings.Builder
	diff, err := m.git.Run(dir, append(append([]string{"diff", "--no-color", "--no-ext-diff"}, prefixes...), from)...)
	if err != nil {
		return "", err
	}
	out.WriteString(diff)
	untracked, err := m.trimmed(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	for _, file := range strings.Split(untracked, "\n") {
		if file == "" {
			continue
		}
		// --no-index answers 1 when the files differ, which they always do here.
		added, _ := m.git.Run(dir, append(append([]string{"diff", "--no-color", "--no-ext-diff", "--no-index"}, prefixes...), "/dev/null", file)...)
		if !strings.HasPrefix(added, "diff --git") {
			return "", fmt.Errorf("cannot read the new file %s", file)
		}
		out.WriteString(added)
	}
	return out.String(), nil
}
