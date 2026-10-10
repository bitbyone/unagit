package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/workspace"
)

// Choosing the branch a branch is measured against: Set Base… records it,
// for a branch made outside unagit or one whose base was wrong, and Rebase
// onto… puts the branch on top of one once, the base left as it was. Both
// pick from the same list, the branches the branch manager reads, ordered by
// how likely each is: the base there is, the default branch, the target of
// the merge request open from the branch, then the rest by name.

// baseScope is the branch a base is chosen for.
type baseScope struct {
	project forge.Project
	dir     string // a checkout that has the branch out
	branch  string
	// row is the worktree it was chosen in, for a rebase; zero in
	// Repositories.
	row worktreeRow
}

// baseCandidate is a branch offered as a base, and why it comes where it does.
type baseCandidate struct {
	name string
	why  string
}

// chooseBase reads the repository's branches and lists them, then hands
// the one chosen to pick. title names the picker; enterHint, enterName and
// enterAbout say what Enter does.
func (a *App) chooseBase(s baseScope, title, enterHint, enterName, enterAbout string, pick func(name string)) {
	pr := s.project
	client := a.client(pr.Instance)
	cloned := a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned
	git := a.newManager(pr.Instance, pr.PathWithNamespace, nil).Git()
	var mrTarget string
	var mrIID int
	if mr, ok := a.openMROn(pr, s.branch); ok {
		mrTarget, mrIID = mr.TargetBranch, mr.IID
	}
	var candidates []baseCandidate
	a.loadThen("Loading branches of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		// Without a token origin's branches are what the clone last fetched
		// of them; the list is still worth having.
		var remote []forge.Branch
		if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var err error
			if remote, err = client.ProjectBranches(ctx, pr); err != nil {
				return "", err
			}
		}
		branches := a.readBranches(pr, remote, cloned)
		current := git.BranchBases(s.dir)[s.branch]
		if current == "" {
			current = s.row.Base
		}
		candidates = baseCandidates(branches, s.branch, current, mrTarget, mrIID)
		log(fmt.Sprintf("%d branch(es)", len(candidates)))
		return "", nil
	}, func(string) {
		if len(candidates) == 0 {
			a.flash(pr.PathWithNamespace + " has no other branch to choose")
			return
		}
		width := 0
		for _, c := range candidates {
			width = max(width, len([]rune(c.name)))
		}
		items := make([]pickItem, len(candidates))
		for i, c := range candidates {
			items[i] = pickItem{Label: fmt.Sprintf("%-*s", width, c.name), Sub: c.why, Data: c.name}
		}
		opts := pickerOptions{pack: true, enterHint: enterHint, enterName: enterName, enterAbout: enterAbout}
		a.showPickerWith(title, items, opts, func(it pickItem) { pick(it.Data.(string)) })
	})
}

// baseCandidates orders the branches a base may be: the current base, the
// default branch, the merge request's target, then the rest by name; the
// branch itself is not its own base.
func baseCandidates(branches []branchInfo, self, current, mrTarget string, mrIID int) []baseCandidate {
	rank := func(b branchInfo) int {
		switch {
		case b.name == current:
			return 0
		case b.isDefault:
			return 1
		case b.name == mrTarget:
			return 2
		}
		return 3
	}
	var out []branchInfo
	for _, b := range branches {
		if b.name != self {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return out[i].name < out[j].name
	})
	candidates := make([]baseCandidate, len(out))
	for i, b := range out {
		var why []string
		if b.name == current {
			why = append(why, "the base now")
		}
		if b.isDefault {
			why = append(why, "default")
		}
		if b.name == mrTarget {
			why = append(why, fmt.Sprintf("target of !%d", mrIID))
		}
		if !b.remote {
			why = append(why, "local only")
		}
		candidates[i] = baseCandidate{name: b.name, why: strings.Join(why, " · ")}
	}
	return candidates
}

// setBase records the base of a branch: Set Base….
func (a *App) setBase(s baseScope) {
	a.chooseBase(s, "Base of "+s.branch, "set as base", "Set Base", "Record this branch as the one "+s.branch+" was made from.",
		func(base string) {
			git := a.newManager(s.project.Instance, s.project.PathWithNamespace, nil).Git()
			since := time.Now()
			change := gitx.RewriteChange{Kind: gitx.RewriteBase, What: "set the base to " + base}
			if _, err := git.Rewriting(s.dir, change, func() error { return git.SetBranchBase(s.dir, s.branch, base) }); err != nil {
				a.errorf("recording the base of %s: %s", s.branch, firstLine(err.Error()))
				return
			}
			a.logRewrites(s.project, s.dir, since)
			// The rows read where they stand against the base again.
			a.refreshDisk()
			a.done(fmt.Sprintf("%s is now based on %s", s.branch, base))
		})
}

// rebaseOnto rebases a worktree's branch onto a branch chosen: Rebase onto….
func (a *App) rebaseOnto(s baseScope) {
	items := a.worktreeItems([]worktreeRow{s.row})
	if len(items) == 0 {
		a.flash(s.row.Path + " holds no repository")
		return
	}
	a.chooseBase(s, "Rebase "+s.branch+" onto", "rebase onto it", "Rebase onto", "Put the branch's commits on top of this branch; its base stays.",
		func(onto string) {
			a.moveMany("Rebasing "+s.row.Path+" onto "+onto, items,
				func(m *workspace.Manager, dir, _ string) (string, error) { return m.RebaseOnto(dir, onto) })
		})
}

// worktreeBase is the scope of Set Base… and Rebase onto… for a worktree.
func (a *App) worktreeBase(r worktreeRow) baseScope {
	return baseScope{project: a.worktreeProject(r), dir: r.Dir, branch: r.Branch, row: r}
}

// cloneBase is the scope of Set Base… for a clone in Repositories.
func (a *App) cloneBase(pr forge.Project) baseScope {
	return baseScope{project: pr, dir: a.projectDir(pr.Instance, pr.PathWithNamespace),
		branch: a.diskOf(pr.Instance, pr.PathWithNamespace).Branch}
}
