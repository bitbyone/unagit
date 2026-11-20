package draft

import (
	"testing"
)

func TestDraftsAreKeptByMergeRequestAndCounted(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a := Key{"gl", "acme/api", 7}
	b := Key{"gl", "acme/web", 7}
	first, err := s.Add(a, Draft{Path: "x.go", Line: 3, Body: "why?"})
	if err != nil || first.ID == "" || first.Created.IsZero() {
		t.Fatalf("added %+v, %v", first, err)
	}
	if _, err := s.Add(a, Draft{Path: "y.go", Line: 1, Body: "and this"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(b, Draft{Path: "z.go", Line: 9, Body: "here"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Counts(); got[a] != 2 || got[b] != 1 || len(got) != 2 {
		t.Errorf("counts = %v", got)
	}
	if err := s.Remove(a, first.ID); err != nil {
		t.Fatal(err)
	}
	left, err := s.List(a)
	if err != nil || len(left) != 1 || left[0].Path != "y.go" {
		t.Errorf("left = %+v, %v", left, err)
	}
	if err := s.Remove(b, mustOnly(t, s, b).ID); err != nil {
		t.Fatal(err)
	}
	if got := s.Counts(); got[b] != 0 {
		t.Errorf("an emptied merge request still counts: %v", got)
	}
}

func mustOnly(t *testing.T, s Store, k Key) Draft {
	t.Helper()
	drafts, err := s.List(k)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("drafts = %+v, %v", drafts, err)
	}
	return drafts[0]
}
