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

// RepositoryTags are the tags one repository wears, by name.
type RepositoryTags struct {
	Instance string   `yaml:"instance"`
	Path     string   `yaml:"path"`
	Tags     []string `yaml:"tags"`
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
	}
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

// SetTag adds a tag, or changes the one called old - its repositories and
// the filter follow a new name. It reports false when the name is empty or
// another tag has it already.
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
		c.renameTag(old, t.Name)
	}
	return true
}

func (c *Config) renameTag(old, name string) {
	for i := range c.RepositoryTags {
		for j, n := range c.RepositoryTags[i].Tags {
			if n == old {
				c.RepositoryTags[i].Tags[j] = name
			}
		}
	}
	for i, n := range c.Filters.Tags {
		if n == old {
			c.Filters.Tags[i] = name
		}
	}
}

// RemoveTag deletes a tag, from the repositories wearing it and from the
// filter too.
func (c *Config) RemoveTag(name string) {
	tags := slices.DeleteFunc(slices.Clone(c.TagList()), func(t Tag) bool { return t.Name == name })
	if tags == nil {
		tags = []Tag{}
	}
	c.Tags = tags
	kept := c.RepositoryTags[:0]
	for _, r := range c.RepositoryTags {
		r.Tags = slices.DeleteFunc(r.Tags, func(n string) bool { return n == name })
		if len(r.Tags) > 0 {
			kept = append(kept, r)
		}
	}
	c.RepositoryTags = kept
	c.Filters.Tags = slices.DeleteFunc(c.Filters.Tags, func(n string) bool { return n == name })
}

// TagsOf lists the tags a repository wears, in the order of the tag list, so
// every row shows them the same way round. Names no longer in the list are
// left out.
func (c *Config) TagsOf(instance, path string) []string {
	var worn []string
	for _, r := range c.RepositoryTags {
		if r.Instance == instance && r.Path == path {
			worn = r.Tags
			break
		}
	}
	var out []string
	for _, t := range c.TagList() {
		if slices.Contains(worn, t.Name) {
			out = append(out, t.Name)
		}
	}
	return out
}

// ToggleTag puts a tag on a repository or takes it off, and reports whether
// the repository wears it now.
func (c *Config) ToggleTag(instance, path, name string) bool {
	for i, r := range c.RepositoryTags {
		if r.Instance != instance || r.Path != path {
			continue
		}
		if slices.Contains(r.Tags, name) {
			c.RepositoryTags[i].Tags = slices.DeleteFunc(r.Tags, func(n string) bool { return n == name })
			if len(c.RepositoryTags[i].Tags) == 0 {
				c.RepositoryTags = slices.Delete(c.RepositoryTags, i, i+1)
			}
			return false
		}
		c.RepositoryTags[i].Tags = append(r.Tags, name)
		return true
	}
	c.RepositoryTags = append(c.RepositoryTags, RepositoryTags{Instance: instance, Path: path, Tags: []string{name}})
	return true
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

// PassesTags reports whether a repository wearing these tags is shown: any
// tag of the filter will do, and no filter shows everything.
func (f *Filters) PassesTags(worn []string) bool {
	if len(f.Tags) == 0 {
		return true
	}
	for _, n := range worn {
		if slices.Contains(f.Tags, n) {
			return true
		}
	}
	return false
}
