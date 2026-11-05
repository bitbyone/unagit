package keymap

import (
	"strings"
	"testing"
)

const oldTemplate = `# unagit's keys: every action, with the key it has, commented out.
# Uncomment a line to make it yours.

# Repositories (1): the list.
[repositories]

# Open: Open the clone.
# open = "ctrl+o"

# Pull: Fetch and fast-forward.
# pull = "p"

# Shout: Gone in the next release.
# shout = "!"
`

const newTemplate = `# unagit's keys: every action, with the key it has, commented out.
# Uncomment a line to make it yours, now with more words.

# Repositories (1): the repository under the cursor, the marked ones, and the
#   list.
[repositories]

# Open: Open the clone in your default editor.
# open = "ctrl+o"

# Clone: Clone it without opening it.
# clone = "C"

# Pull: Fetch and fast-forward.
# pull = "alt+p"

# Merge requests (2): the list.
[merge_requests]

# Review: Review the change.
# review = "ctrl+r"
`

// TestUpdateKeepsWhatIsTheUsers: a file written from last release's
// template comes out as this release's, with the line the user
// uncommented, their note beside it and their note of their own as they
// were.
func TestUpdateKeepsWhatIsTheUsers(t *testing.T) {
	t.Parallel()
	user := strings.Replace(oldTemplate, `# Open: Open the clone.
# open = "ctrl+o"`, `# Open: Open the clone.
# Zellij keeps ctrl+o.
open = "ctrl+e"  # mine`, 1) + `
# my own note, about nothing unagit knows
mystery = "f9"
`
	got, ch := Update(user, newTemplate)
	want := `# unagit's keys: every action, with the key it has, commented out.
# Uncomment a line to make it yours, now with more words.

# Repositories (1): the repository under the cursor, the marked ones, and the
#   list.
[repositories]

# Open: Open the clone in your default editor.
# Zellij keeps ctrl+o.
open = "ctrl+e"  # mine

# Clone: Clone it without opening it.
# clone = "C"

# Pull: Fetch and fast-forward.
# pull = "alt+p"

# my own note, about nothing unagit knows
mystery = "f9"

# Merge requests (2): the list.
[merge_requests]

# Review: Review the change.
# review = "ctrl+r"
`
	if got != want {
		t.Errorf("updated:\n%s\nwant:\n%s", got, want)
	}
	if ch != (Changes{Added: 2, Changed: 1, Removed: 1, Rewritten: true}) {
		t.Errorf("changes %+v", ch)
	}
	again, ch := Update(got, newTemplate)
	if again != got || ch.Rewritten {
		t.Errorf("a second update changed it again: %+v\n%s", ch, again)
	}
}

// TestUpdateOfTheTemplateIsTheTemplate: a file written today has nothing
// to bring up, and a file of the user's own, written without the template,
// gets the template's header and every action it lacks without losing a
// line of its own.
func TestUpdateOfTheTemplateIsTheTemplate(t *testing.T) {
	t.Parallel()
	if got, ch := Update(newTemplate, newTemplate); got != newTemplate || ch.Rewritten {
		t.Errorf("the template changed: %+v\n%s", ch, got)
	}
	got, ch := Update("[merge_requests]\nreview = \"f5\"\n", newTemplate)
	if !strings.Contains(got, "[merge_requests]\n\n# Review: Review the change.\nreview = \"f5\"\n") {
		t.Errorf("the user's binding lost its place:\n%s", got)
	}
	if strings.Contains(got, `# review = "ctrl+r"`) {
		t.Errorf("a commented copy beside the user's binding:\n%s", got)
	}
	if ch.Added != 3 || !strings.HasPrefix(got, "# unagit's keys") {
		t.Errorf("%+v\n%s", ch, got)
	}
}
