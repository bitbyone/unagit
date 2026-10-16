package gitx

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Tag is a tag on a commit. An annotated one has a message, a tagger and a
// date of its own; a lightweight one is only a name.
type Tag struct {
	Name      string
	Annotated bool
	Subject   string
	Tagger    string
	When      time.Time
}

// TagsAt lists the tags pointing at a commit, an annotated one by the
// commit it was made on.
func (g *Git) TagsAt(dir, sha string) ([]Tag, error) {
	out, err := g.out(dir, "for-each-ref", "--points-at", sha, "--sort=refname",
		"--format=%(refname:strip=2)%1f%(objecttype)%1f%(contents:subject)%1f%(taggername)%1f%(taggerdate:unix)",
		"refs/tags")
	if err != nil {
		return nil, err
	}
	var tags []Tag
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) != 5 || f[0] == "" {
			continue
		}
		t := Tag{Name: f[0], Annotated: f[1] == "tag"}
		if t.Annotated {
			t.Subject, t.Tagger = f[2], f[3]
			if unix, err := strconv.ParseInt(f[4], 10, 64); err == nil {
				t.When = time.Unix(unix, 0)
			}
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// CheckTagName says what is wrong with a name for a new tag, "" when it
// will do: git's rules for a ref, no leading dash (it would read as an
// option), and no tag of that name already.
func (g *Git) CheckTagName(dir, name string) string {
	switch {
	case name == "":
		return "enter a name for the tag"
	case strings.HasPrefix(name, "-"):
		return name + " cannot be a tag: it starts with a dash"
	}
	if _, err := g.out(dir, "check-ref-format", "refs/tags/"+name); err != nil {
		return name + " cannot be a tag: no spaces, no .. or ~^:?*[\\, and no / at either end"
	}
	if _, err := g.out(dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+name); err == nil {
		return name + " already exists - delete it first (T on its commit)"
	}
	return ""
}

// CreateTag puts a tag on a commit: annotated with a message, lightweight
// without one. It never moves a tag that exists.
func (g *Git) CreateTag(dir, name, sha, message string) error {
	if why := g.CheckTagName(dir, name); why != "" {
		return fmt.Errorf("%s", why)
	}
	args := []string{"tag"}
	if strings.TrimSpace(message) != "" {
		args = append(args, "--annotate", "--cleanup=strip", "--message", message)
	}
	_, err := g.Run(dir, append(args, name, sha)...)
	return err
}

// PushTag sends a tag to origin. It never forces: a tag origin has on
// another commit is refused.
func (g *Git) PushTag(dir, name string) error {
	_, err := g.Run(dir, "push", "origin", "refs/tags/"+name)
	return err
}

// DeleteTag removes a tag here; origin keeps its own.
func (g *Git) DeleteTag(dir, name string) error {
	_, err := g.Run(dir, "tag", "--delete", name)
	return err
}

// DeleteRemoteTag removes a tag from origin.
func (g *Git) DeleteRemoteTag(dir, name string) error {
	_, err := g.Run(dir, "push", "origin", "--delete", "refs/tags/"+name)
	return err
}

// RemoteTags asks origin which tags it has and the commit each is on, an
// annotated one by the commit it was made on.
func (g *Git) RemoteTags(dir string) (map[string]string, error) {
	out, err := g.out(dir, "ls-remote", "--tags", "origin")
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		name := strings.TrimPrefix(ref, "refs/tags/")
		// An annotated tag comes twice: the tag object, then with ^{} the
		// commit it peels to, which is the one that counts.
		if peeled, ok := strings.CutSuffix(name, "^{}"); ok {
			tags[peeled] = sha
			continue
		}
		if _, seen := tags[name]; !seen {
			tags[name] = sha
		}
	}
	return tags, nil
}
