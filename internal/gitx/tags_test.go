package gitx

import (
	"strings"
	"testing"
)

func TestTagsAreMadePushedAndDeleted(t *testing.T) {
	origin, clone := repos(t)
	g := New("", nil)
	first := sh(t, clone, "rev-parse", "HEAD")
	commit(t, clone, "b.txt", "second")
	second := sh(t, clone, "rev-parse", "HEAD")

	if err := g.CreateTag(clone, "light", first, ""); err != nil {
		t.Fatal(err)
	}
	if err := g.CreateTag(clone, "v1.0", first, "The first release\n\nWith a body."); err != nil {
		t.Fatal(err)
	}
	tags, err := g.TagsAt(clone, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].Name != "light" || tags[0].Annotated || tags[1].Name != "v1.0" || !tags[1].Annotated {
		t.Fatalf("tags at the first commit: %+v", tags)
	}
	if tags[1].Subject != "The first release" || tags[1].Tagger == "" || tags[1].When.IsZero() {
		t.Errorf("the annotated tag does not say what it is: %+v", tags[1])
	}
	if tags, _ := g.TagsAt(clone, second); len(tags) != 0 {
		t.Errorf("the second commit has tags: %+v", tags)
	}

	// A tag is never moved by making it again.
	if err := g.CreateTag(clone, "v1.0", second, ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("making v1.0 again: %v", err)
	}
	if got := sh(t, clone, "rev-parse", "v1.0^{commit}"); got != first {
		t.Errorf("v1.0 moved to %s", got)
	}

	if err := g.PushTag(clone, "v1.0"); err != nil {
		t.Fatal(err)
	}
	remote, err := g.RemoteTags(clone)
	if err != nil {
		t.Fatal(err)
	}
	if remote["v1.0"] != first {
		t.Errorf("origin has v1.0 at %q, want the commit %s: %v", remote["v1.0"], first, remote)
	}
	if _, ok := remote["light"]; ok {
		t.Error("an unpushed tag is on origin")
	}

	if err := g.DeleteTag(clone, "light"); err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteRemoteTag(clone, "v1.0"); err != nil {
		t.Fatal(err)
	}
	if out := sh(t, origin, "tag", "--list"); out != "" {
		t.Errorf("origin still has tags: %q", out)
	}
	if out := sh(t, clone, "tag", "--list"); out != "v1.0" {
		t.Errorf("tags here: %q, want v1.0 alone (deleting on origin keeps it here)", out)
	}
}

func TestCheckTagName(t *testing.T) {
	_, clone := repos(t)
	g := New("", nil)
	sh(t, clone, "tag", "taken")
	for name, want := range map[string]string{
		"v0.2":         "",
		"release/2026": "",
		"":             "enter a name",
		"-f":           "starts with a dash",
		"two words":    "cannot be a tag",
		"a..b":         "cannot be a tag",
		"taken":        "already exists",
	} {
		got := g.CheckTagName(clone, name)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("CheckTagName(%q) = %q, want %q", name, got, want)
		}
	}
}
