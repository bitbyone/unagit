package index

import (
	"testing"
	"time"
)

// TestUsersAskOnlyForWhatIsUnknownOrOld: a name learnt lately is not asked
// for again, one older than NameAge is, an empty name counts as learnt, and
// a clone learns apart from its original.
func TestUsersAskOnlyForWhatIsUnknownOrOld(t *testing.T) {
	now := time.Now()
	var u Users
	u.Learn("gl", "bob", "Bob Ross", now)
	u.Learn("gl", "jane", "", now)
	u.Learn("gl", "old", "Old Name", now.Add(-NameAge-time.Hour))
	got := u.Unknown("gl", []string{"bob", "jane", "old", "new", "new", ""}, now)
	if len(got) != 2 || got[0] != "old" || got[1] != "new" {
		t.Errorf("Unknown = %v, want [old new]", got)
	}
	if u.Name("gl", "bob") != "Bob Ross" || u.Name("gh", "bob") != "" {
		t.Error("a name belongs to its server")
	}
	c := u.Clone()
	c.Learn("gl", "new", "New", now)
	if u.Name("gl", "new") != "" {
		t.Error("learning in a clone changed the original")
	}
}
