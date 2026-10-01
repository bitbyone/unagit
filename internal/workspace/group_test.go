package workspace

import (
	"reflect"
	"testing"
)

func TestMemberDirNamesKeepNamesApart(t *testing.T) {
	got := MemberDirNames([]string{"acme/api", "acme/web", "tools/api"})
	want := []string{"acme-api", "web", "tools-api"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGroupRoundTrips(t *testing.T) {
	dir := t.TempDir()
	g := Group{Name: "multi", Branch: "feat/multi", Members: []GroupMember{
		{Instance: "gl", Project: "acme/api", Dir: "api", Branch: "feat/multi", Base: "main"}}}
	if err := WriteGroup(dir, g); err != nil {
		t.Fatal(err)
	}
	back, err := ReadGroup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name != g.Name || back.Branch != g.Branch || !reflect.DeepEqual(back.Members, g.Members) {
		t.Errorf("read back %+v, wrote %+v", back, g)
	}
	if extras := GroupExtras(dir, back); len(extras) != 0 {
		t.Errorf("an empty group holds %v", extras)
	}
}
