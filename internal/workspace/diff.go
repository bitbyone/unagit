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

// CommitLine is one commit as a list shows it.
type CommitLine struct {
	SHA     string
	Subject string
	When    string // relative, as git says it
}

// Commits lists the commits of a working tree's branch since from, newest
// first; without from, the latest ones. At most limit.
func (m *Manager) Commits(dir, from string, limit int) []CommitLine {
	args := []string{"log", fmt.Sprintf("-%d", limit), "--format=%H%x1f%s%x1f%cr"}
	if from != "" && from != "HEAD" {
		args = append(args, from+"..HEAD")
	}
	return m.commitLines(dir, args)
}

// CommitsIn lists the commits of a range, newest first, at most limit.
func (m *Manager) CommitsIn(dir, revRange string, limit int) []CommitLine {
	return m.commitLines(dir, []string{"log", fmt.Sprintf("-%d", limit), "--format=%H%x1f%s%x1f%cr", revRange})
}

func (m *Manager) commitLines(dir string, args []string) []CommitLine {
	out, err := m.trimmed(dir, args...)
	if err != nil || out == "" {
		return nil
	}
	var lines []CommitLine
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\x1f", 3)
		if len(f) == 3 {
			lines = append(lines, CommitLine{SHA: f[0], Subject: f[1], When: f[2]})
		}
	}
	return lines
}
