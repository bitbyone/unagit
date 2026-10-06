package ui

import (
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/config"
)

func linkConfig() *config.Config {
	cfg := config.Default()
	cfg.AddInstance(config.Instance{Name: "work", URL: "https://gitlab.example.com"})
	cfg.AddInstance(config.Instance{Name: "sub", URL: "https://code.example.org/gitlab"})
	cfg.AddInstance(config.Instance{Name: "me", Kind: config.KindGitHub, URL: config.GitHubURL,
		Groups: []config.Group{{FullPath: "someone"}}})
	cfg.AddInstance(config.Instance{Name: "company", Kind: config.KindGitHub, URL: config.GitHubURL,
		Groups: []config.Group{{FullPath: "acme"}}})
	return cfg
}

// TestParseMRLinkReadsWhatPeoplePaste: the links come from a browser, with
// whatever tab or comment anchor was open.
func TestParseMRLinkReadsWhatPeoplePaste(t *testing.T) {
	t.Parallel()
	cfg := linkConfig()
	id := func(name string) string {
		for _, inst := range cfg.Instances {
			if inst.Name == name {
				return inst.ID
			}
		}
		return ""
	}
	for raw, want := range map[string]MRLink{
		"https://gitlab.example.com/group/sub/app/-/merge_requests/12":            {id("work"), "group/sub/app", 12},
		"https://gitlab.example.com/group/app/-/merge_requests/12/diffs#note_345": {id("work"), "group/app", 12},
		"  https://gitlab.example.com/group/app/merge_requests/3?tab=commits \n":  {id("work"), "group/app", 3},
		"https://code.example.org/gitlab/team/app/-/merge_requests/7":             {id("sub"), "team/app", 7},
		"https://github.com/acme/api/pull/42/files":                               {id("company"), "acme/api", 42},
		"https://github.com/someone/dotfiles/pull/1":                              {id("me"), "someone/dotfiles", 1},
		"https://github.com/stranger/tool/pull/9#discussion_r1":                   {id("me"), "stranger/tool", 9},
	} {
		got, err := ParseMRLink(cfg, raw)
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %+v, want %+v", raw, got, want)
		}
	}
}

func TestParseMRLinkSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	cfg := linkConfig()
	for raw, want := range map[string]string{
		"group/app!12": "not a merge request address",
		"https://elsewhere.example/group/app/-/merge_requests/1":  "no server in Settings is at elsewhere.example",
		"https://gitlab.example.com/group/app/-/issues/4":         "does not point at a merge request",
		"https://gitlab.example.com/group/app/-/merge_requests/x": "does not point at a merge request",
		"https://github.com/acme/api/issues/42":                   "does not point at a merge request",
	} {
		if _, err := ParseMRLink(cfg, raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", raw, err, want)
		}
	}
}

// TestGoalOpensAMergeRequestOutsideTheIndex: a link to a repository that is
// not in any selected group still opens, looked up by its path.
func TestGoalOpensAMergeRequestOutsideTheIndex(t *testing.T) {
	t.Parallel()
	srv := fakeGitLab(t)
	cfg := writeTestConfig(t, srv.URL)
	link, err := ParseMRLink(cfg, srv.URL+"/acme/other/-/merge_requests/5")
	if err != nil {
		t.Fatal(err)
	}
	a, sc := startApp(t, newApp(cfg, testVault(t, cfg)).WithGoal(Goal{Link: link}))
	waitFor(t, a, sc, "Opening acme/other !5")
	// It then tries to clone from the stub, which fails; wait so git is done
	// before the temporary directories go.
	waitFor(t, a, sc, "Press Esc to close")
}

// TestGoalReviewsAMergeRequestFromTheList: one the list has is selected there
// and opened for review.
func TestGoalReviewsAMergeRequestFromTheList(t *testing.T) {
	t.Parallel()
	srv := fakeGitLab(t)
	cfg := writeTestConfig(t, srv.URL)
	link, err := ParseMRLink(cfg, srv.URL+"/acme/gateway/-/merge_requests/8")
	if err != nil {
		t.Fatal(err)
	}
	a, sc := startApp(t, newApp(cfg, testVault(t, cfg)).WithGoal(Goal{Link: link, Review: true}))
	waitFor(t, a, sc, "Opening acme/gateway !8 for review")
	waitFor(t, a, sc, "Press Esc to close")
	if got := onLoop(a, func() int {
		if i := a.mrsPane.selectedIndex(); i >= 0 {
			return a.mrs[i].IID
		}
		return 0
	}); got != 8 {
		t.Errorf("the list has !%d selected, want !8", got)
	}
}
