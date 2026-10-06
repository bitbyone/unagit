package ui

import (
	"strings"
	"testing"
)

// TestShortenRepo: the groups give way before the name, from the top down,
// cut in the middle while that leaves something and collapsed after; the
// name is cut last, and nothing comes out wider than asked.
func TestShortenRepo(t *testing.T) {
	t.Parallel()
	e := elided()
	repo := "my2n/ai-transformation/building-access-analysis"
	for n, want := range map[int]string{
		60: repo,
		47: repo,
		46: e + "/ai-transformation/building-access-analysis",
		44: e + "/ai-transformation/building-access-analysis",
		40: e + "/ai-tra…mation/building-access-analysis",
		30: e + "/building-access-analysis",
		25: e + "building-access-analysis",
		24: "building-access-analysis",
		12: "buildi…lysis",
		0:  "",
	} {
		if got := shortenRepo(repo, n); got != want {
			t.Errorf("shortenRepo(%d) = %q, want %q", n, got, want)
		}
	}
	for n := 0; n <= 50; n++ {
		if got := shortenRepo(repo, n); cells(got) > n {
			t.Errorf("shortenRepo(%d) = %q is %d wide", n, got, cells(got))
		}
	}
	if got := shortenRepo("gateway", 5); got != "ga…ay" {
		t.Errorf("a name without groups is cut in the middle: %q", got)
	}
}

// TestShortenPath: the folders between the start and the directory are cut
// to one length, the longest first, then collapsed from the directory
// upwards, and the start and the directory stay whole to the end.
func TestShortenPath(t *testing.T) {
	t.Parallel()
	e := elided()
	path := "~/workspace/jantobola/websites/tobolovi-com"
	for n, want := range map[int]string{
		43: path,
		40: "~/work…ace/jant…ola/web…tes/tobolovi-com",
		32: "~/wo…ce/ja…la/we…es/tobolovi-com",
		26: "~/workspace/" + e + "/tobolovi-com",
		22: "~/wo…ce/" + e + "/tobolovi-com",
		16: "~/" + e + "/tobolovi-com",
		14: e + "/tobolovi-com",
	} {
		if got := shortenPath(path, n); got != want {
			t.Errorf("shortenPath(%d) = %q, want %q", n, got, want)
		}
	}
	for n := 0; n <= 45; n++ {
		got := shortenPath(path, n)
		if cells(got) > n {
			t.Errorf("shortenPath(%d) = %q is %d wide", n, got, cells(got))
		}
		if n >= 13 && !strings.HasSuffix(got, "tobolovi-com") {
			t.Errorf("shortenPath(%d) = %q lost the directory", n, got)
		}
	}
	// Short folders are kept whole rather than collapsed with the rest.
	if got, want := shortenPath("/var/folders/nk/T/acme/building-access-analysis", 40), "/var/fo…rs/nk/"+e+"/building-access-analysis"; got != want {
		t.Errorf("short folders: %q, want %q", got, want)
	}
}
