package config

import (
	"slices"
	"strings"
)

// Tag is a label of the user's own, worn by repositories, in a colour named
// from the interface's palette.
type Tag struct {
	Name  string `yaml:"name"`
	Color string `yaml:"color"`
}

// TagSet is what a group or a repository says about its tags: the ones it
// adds, and Off, the ones it takes away from what it inherits.
type TagSet struct {
	Instance string   `yaml:"instance"`
	Path     string   `yaml:"path"`
	Tags     []string `yaml:"tags,omitempty"`
	Off      []string `yaml:"off,omitempty"`
}

// How a tag's pill ends.
const (
	TagEndsRounded = "rounded" // Powerline's half circles, from a Nerd Font
	TagEndsCircles = "circles" // Unicode half circles, in any font
	TagEndsSquare  = "square"  // no ends at all
)

// DefaultTags are the tags a configuration starts with.
func DefaultTags() []Tag {
	return []Tag{
		{Name: "oss", Color: "mint"},
		{Name: "personal", Color: "lavender"},
		{Name: "work", Color: "sky"},
		{Name: "private", Color: "rose"},
		{Name: "fork", Color: "peach"},
		{Name: "hobby", Color: "lilac"},
		{Name: "tooling", Color: "butter"},
	}
}

// firstDefaultTags is how many default tags there were before anyone kept
// count: a configuration that does not say has seen those.
const firstDefaultTags = 4

// offerNewDefaultTags adds the default tags that came after the ones this
// configuration has seen. A configuration with no tags at all chose that.
func (c *Config) offerNewDefaultTags() {
	defaults := DefaultTags()
	seen := c.DefaultTagsSeen
	if seen == 0 {
		seen = firstDefaultTags
	}
	if len(c.Tags) > 0 {
		for _, t := range defaults[min(seen, len(defaults)):] {
			if _, ok := c.Tag(t.Name); !ok {
				c.Tags = append(c.Tags, t)
			}
		}
	}
	c.DefaultTagsSeen = len(defaults)
}

// TagList is the tags, in the order they were made. A configuration made
// without Default, and so without any, has the default ones; one whose tags
// were all removed has none.
func (c *Config) TagList() []Tag {
	if c.Tags == nil {
		return DefaultTags()
	}
	return c.Tags
}

// Tag finds a tag by name.
func (c *Config) Tag(name string) (Tag, bool) {
	for _, t := range c.TagList() {
		if t.Name == name {
			return t, true
		}
	}
	return Tag{}, false
}

// Ends is the pill style, normalised.
func (c *Config) Ends() string {
	switch c.TagEnds {
	case TagEndsCircles, TagEndsSquare:
		return c.TagEnds
	}
	return TagEndsRounded
}

// SetTag adds a tag, or changes the one called old - groups, repositories
// and the filter follow a new name. It reports false when the name is empty
// or another tag has it already.
func (c *Config) SetTag(old string, t Tag) bool {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return false
	}
	tags := slices.Clone(c.TagList())
	at := -1
	for i, existing := range tags {
		if existing.Name == old && old != "" {
			at = i
		} else if existing.Name == t.Name {
			return false
		}
	}
	if at < 0 {
		c.Tags = append(tags, t)
		return true
	}
	tags[at] = t
	c.Tags = tags
	if old != t.Name {
		rename := func(names []string) {
			for i, n := range names {
				if n == old {
					names[i] = t.Name
				}
			}
		}
		for _, sets := range [][]TagSet{c.GroupTags, c.RepositoryTags} {
			for i := range sets {
				rename(sets[i].Tags)
				rename(sets[i].Off)
			}
		}
		rename(c.Filters.Tags)
	}
	return true
}

// RemoveTag deletes a tag, from the groups and repositories wearing it and
// from the filter too.
func (c *Config) RemoveTag(name string) {
	tags := slices.DeleteFunc(slices.Clone(c.TagList()), func(t Tag) bool { return t.Name == name })
	if tags == nil {
		tags = []Tag{}
	}
	c.Tags = tags
	drop := func(n string) bool { return n == name }
	c.GroupTags = pruneSets(c.GroupTags, drop)
	c.RepositoryTags = pruneSets(c.RepositoryTags, drop)
	c.Filters.Tags = slices.DeleteFunc(c.Filters.Tags, drop)
}

// pruneSets takes names out of every set and drops the sets left empty.
func pruneSets(sets []TagSet, drop func(string) bool) []TagSet {
	kept := sets[:0]
	for _, s := range sets {
		s.Tags = slices.DeleteFunc(s.Tags, drop)
		s.Off = slices.DeleteFunc(s.Off, drop)
		if len(s.Tags)+len(s.Off) > 0 {
			kept = append(kept, s)
		}
	}
	return kept
}

func findSet(sets []TagSet, instance, path string) *TagSet {
	for i := range sets {
		if sets[i].Instance == instance && sets[i].Path == path {
			return &sets[i]
		}
	}
	return nil
}

// apply lays a set over what was inherited.
func apply(worn map[string]bool, s *TagSet) {
	if s == nil {
		return
	}
	for _, n := range s.Off {
		delete(worn, n)
	}
	for _, n := range s.Tags {
		worn[n] = true
	}
}

// inherited is what the server and the groups above path pass down to it:
// the server's set first, then each group from the top laying its own over
// its parent's. The server itself, path "", inherits nothing.
func (c *Config) inherited(instance, path string) map[string]bool {
	worn := map[string]bool{}
	if path == "" {
		return worn
	}
	apply(worn, findSet(c.GroupTags, instance, ""))
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		apply(worn, findSet(c.GroupTags, instance, strings.Join(parts[:i], "/")))
	}
	return worn
}

// inOrder lists the worn tags in the order of the tag list, so every row
// shows them the same way round. Names no longer in the list are left out.
func (c *Config) inOrder(worn map[string]bool) []string {
	var out []string
	for _, t := range c.TagList() {
		if worn[t.Name] {
			out = append(out, t.Name)
		}
	}
	return out
}

// TagsOf lists the tags a repository wears: what its groups pass down, with
// its own changes laid over.
func (c *Config) TagsOf(instance, path string) []string {
	worn := c.inherited(instance, path)
	apply(worn, findSet(c.RepositoryTags, instance, path))
	return c.inOrder(worn)
}

// GroupTagsOf lists the tags a group wears and passes down.
func (c *Config) GroupTagsOf(instance, path string) []string {
	worn := c.inherited(instance, path)
	apply(worn, findSet(c.GroupTags, instance, path))
	return c.inOrder(worn)
}

// ServerTagsOf lists the tags a server wears and passes down to everything
// on it. They are kept with the groups', under the empty path.
func (c *Config) ServerTagsOf(instance string) []string {
	return c.GroupTagsOf(instance, "")
}

// ToggleServerTag is ToggleTag for a whole server.
func (c *Config) ToggleServerTag(instance, name string) bool {
	return c.ToggleGroupTag(instance, "", name)
}

// InheritedTags lists the tags the groups above path pass down to it, before
// its own changes.
func (c *Config) InheritedTags(instance, path string) []string {
	return c.inOrder(c.inherited(instance, path))
}

// ToggleTag puts a tag on a repository or takes it off, and reports whether
// the repository wears it now. An inherited tag is taken off by saying so, and
// put back by forgetting that.
func (c *Config) ToggleTag(instance, path, name string) bool {
	return toggleIn(&c.RepositoryTags, c.inherited(instance, path), instance, path, name)
}

// ToggleGroupTag is ToggleTag for a group, whose subgroups and repositories
// follow.
func (c *Config) ToggleGroupTag(instance, path, name string) bool {
	return toggleIn(&c.GroupTags, c.inherited(instance, path), instance, path, name)
}

func toggleIn(sets *[]TagSet, inherited map[string]bool, instance, path, name string) bool {
	s := findSet(*sets, instance, path)
	if s == nil {
		*sets = append(*sets, TagSet{Instance: instance, Path: path})
		s = &(*sets)[len(*sets)-1]
	}
	worn := map[string]bool{}
	for n := range inherited {
		worn[n] = true
	}
	apply(worn, s)
	is := func(n string) bool { return n == name }
	s.Tags = slices.DeleteFunc(s.Tags, is)
	s.Off = slices.DeleteFunc(s.Off, is)
	on := !worn[name]
	switch {
	case on && !inherited[name]:
		s.Tags = append(s.Tags, name)
	case !on && inherited[name]:
		s.Off = append(s.Off, name)
	}
	*sets = pruneSets(*sets, func(string) bool { return false })
	return on
}

// ToggleTagFilter adds a tag to the filter or takes it out, and reports
// whether it is in now.
func (f *Filters) ToggleTagFilter(name string) bool {
	if slices.Contains(f.Tags, name) {
		f.Tags = slices.DeleteFunc(f.Tags, func(n string) bool { return n == name })
		return false
	}
	f.Tags = append(f.Tags, name)
	return true
}

// PassesTags reports whether a repository wearing these tags is shown: it has
// to wear every tag of the filter, so each one picked narrows the list
// further, and no filter shows everything.
func (f *Filters) PassesTags(worn []string) bool {
	for _, n := range f.Tags {
		if !slices.Contains(worn, n) {
			return false
		}
	}
	return true
}
