package config

import (
	"slices"
	"testing"
)

// TestTagsStartWithTheDefaultsAndCanAllGo: a configuration that never touched
// its tags has the default four; one that removed them all keeps none, also
// across a save and a load.
func TestTagsStartWithTheDefaultsAndCanAllGo(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", t.TempDir())
	c := Default()
	if got := len(c.TagList()); got != 4 {
		t.Fatalf("%d default tags", got)
	}
	// Saved untouched, they are still there.
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(); err != nil || len(c.TagList()) != 4 {
		t.Fatalf("after a save and a load: %v, %v", c.TagList(), err)
	}
	for _, tag := range DefaultTags() {
		c.RemoveTag(tag.Name)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.TagList(); len(got) != 0 {
		t.Errorf("removed tags came back: %v", got)
	}
}

// TestRenamingATagFollowsItEverywhere, and removing one takes it off the
// repositories and out of the filter.
func TestRenamingATagFollowsItEverywhere(t *testing.T) {
	c := Default()
	c.ToggleTag("i", "a/b", "work")
	c.ToggleTag("i", "a/b", "oss")
	c.Filters.ToggleTagFilter("work")

	if !c.SetTag("work", Tag{Name: "job", Color: "peach"}) {
		t.Fatal("rename refused")
	}
	if c.SetTag("job", Tag{Name: "oss"}) {
		t.Error("a rename onto another tag's name went through")
	}
	if got := c.TagsOf("i", "a/b"); !slices.Equal(got, []string{"oss", "job"}) {
		t.Errorf("tags of a/b = %v, in the order of the list", got)
	}
	if !slices.Equal(c.Filters.Tags, []string{"job"}) {
		t.Errorf("filter = %v", c.Filters.Tags)
	}

	c.RemoveTag("job")
	if got := c.TagsOf("i", "a/b"); !slices.Equal(got, []string{"oss"}) {
		t.Errorf("after removing: %v", got)
	}
	if len(c.Filters.Tags) != 0 {
		t.Errorf("the filter still has %v", c.Filters.Tags)
	}
	if !c.Filters.PassesTags(nil) {
		t.Error("no filter should show everything")
	}
	c.ToggleTag("i", "a/b", "oss")
	if len(c.RepositoryTags) != 0 {
		t.Errorf("an untagged repository is still listed: %v", c.RepositoryTags)
	}
}
