package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TestTagsStartWithTheDefaultsAndCanAllGo: a configuration that never touched
// its tags has the default ones; one that removed them all keeps none, also
// across a save and a load.
func TestTagsStartWithTheDefaultsAndCanAllGo(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", t.TempDir())
	c := Default()
	if got := len(c.TagList()); got != len(DefaultTags()) {
		t.Fatalf("%d default tags", got)
	}
	// Saved untouched, they are still there.
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(); err != nil || len(c.TagList()) != len(DefaultTags()) {
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

// TestGroupTagsPassDown: a group's tags reach its subgroups and their
// repositories; a subgroup or a repository can take one away again, and
// putting it back on only forgets that.
func TestGroupTagsPassDown(t *testing.T) {
	c := Default()
	c.ToggleGroupTag("i", "acme", "work")
	c.ToggleGroupTag("i", "acme/tools", "oss")

	if got := c.TagsOf("i", "acme/tools/cli"); !slices.Equal(got, []string{"oss", "work"}) {
		t.Errorf("acme/tools/cli wears %v", got)
	}
	if got := c.TagsOf("i", "acme/api"); !slices.Equal(got, []string{"work"}) {
		t.Errorf("acme/api wears %v", got)
	}
	if got := c.TagsOf("i", "other/api"); len(got) != 0 {
		t.Errorf("a repository outside the group wears %v", got)
	}
	if got := c.TagsOf("j", "acme/api"); len(got) != 0 {
		t.Errorf("the same path on another server wears %v", got)
	}

	// The subgroup takes work away; its repositories follow.
	if c.ToggleGroupTag("i", "acme/tools", "work") {
		t.Error("taking an inherited tag away reported it on")
	}
	if got := c.TagsOf("i", "acme/tools/cli"); !slices.Equal(got, []string{"oss"}) {
		t.Errorf("after the subgroup took work away: %v", got)
	}
	// A repository takes oss away, and puts it back.
	c.ToggleTag("i", "acme/tools/cli", "oss")
	if got := c.TagsOf("i", "acme/tools/cli"); len(got) != 0 {
		t.Errorf("after the repository took oss away: %v", got)
	}
	if !c.ToggleTag("i", "acme/tools/cli", "oss") {
		t.Error("putting oss back reported it off")
	}
	if len(c.RepositoryTags) != 0 {
		t.Errorf("putting an inherited tag back left a set behind: %+v", c.RepositoryTags)
	}

	c.RemoveTag("work")
	if len(c.GroupTags) != 1 || c.GroupTags[0].Path != "acme/tools" {
		t.Errorf("removing work left the group sets %+v", c.GroupTags)
	}
}

// TestNewDefaultTagsReachAnOlderConfiguration: a configuration written when
// there were four default tags gets the ones added since, once; a tag the user
// removes afterwards stays removed.
func TestNewDefaultTagsReachAnOlderConfiguration(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", t.TempDir())
	old := "root_dir: /tmp/x\ninstances: []\ntags:\n  - name: oss\n    color: mint\n  - name: personal\n    color: lavender\n  - name: work\n    color: sky\n  - name: private\n    color: rose\n  - name: mine\n    color: peach\n"
	if err := os.WriteFile(Path(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tag := range c.TagList() {
		names = append(names, tag.Name)
	}
	if got := strings.Join(names, " "); got != "oss personal work private mine fork hobby tooling" {
		t.Fatalf("tags = %s", got)
	}

	c.RemoveTag("hobby")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Tag("hobby"); ok {
		t.Error("a removed default tag came back")
	}
}

// TestServerTagsPassDownToEverything: a server's tags reach its groups and
// every repository on it, and nothing on another server; a group can take one
// off for what is below it.
func TestServerTagsPassDownToEverything(t *testing.T) {
	c := Default()
	c.ToggleServerTag("i", "work")
	if got := c.TagsOf("i", "acme/tools/cli"); !slices.Equal(got, []string{"work"}) {
		t.Errorf("a repository wears %v", got)
	}
	if got := c.GroupTagsOf("i", "acme"); !slices.Equal(got, []string{"work"}) {
		t.Errorf("a group wears %v", got)
	}
	if got := c.TagsOf("j", "acme/api"); len(got) != 0 {
		t.Errorf("a repository on another server wears %v", got)
	}
	c.ToggleGroupTag("i", "acme/tools", "work")
	if got := c.TagsOf("i", "acme/tools/cli"); len(got) != 0 {
		t.Errorf("after the subgroup took it off: %v", got)
	}
	if got := c.TagsOf("i", "acme/api"); !slices.Equal(got, []string{"work"}) {
		t.Errorf("a sibling lost the server's tag: %v", got)
	}
	if !slices.Equal(c.ServerTagsOf("i"), []string{"work"}) {
		t.Errorf("server tags = %v", c.ServerTagsOf("i"))
	}
}

// TestTagFilterWantsEveryTag: each tag picked narrows the list further - a
// repository is shown only when it wears all of them.
func TestTagFilterWantsEveryTag(t *testing.T) {
	f := Filters{Tags: []string{"work", "oss"}}
	for _, c := range []struct {
		worn []string
		want bool
	}{
		{[]string{"work", "oss"}, true},
		{[]string{"oss", "work", "tooling"}, true},
		{[]string{"work"}, false},
		{[]string{"oss", "hobby"}, false},
		{nil, false},
	} {
		if got := f.PassesTags(c.worn); got != c.want {
			t.Errorf("wearing %v: shown = %v, want %v", c.worn, got, c.want)
		}
	}
}
