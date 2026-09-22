package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tobola/unagit/internal/forge"
)

// Review pins the two commits GitLab itself renders a merge request diff from.
// BaseSHA is the merge base, not the tip of the target branch: the target may
// have moved on since, and diffing against its tip would show the target's own
// commits backwards.
type Review struct {
	BaseSHA string
	HeadSHA string
}

// Meta is what unagit records in a worktree's own git configuration, so an
// editor can find out which merge request it is looking at and what to diff
// against:
//
//	git config unagit.mr.base    # the merge base
//	git config unagit.mr.head    # the merge request head
//	git config unagit.mr.mode    # branch or review
//
// Per-worktree configuration keeps this out of the shared repository config,
// so every merge request directory carries its own answer.
type Meta struct {
	IID     int
	Project string
	Source  string
	Target  string
	Base    string
	Head    string
	URL     string
	Mode    string
}

// Modes a worktree can be in.
const (
	ModeBranch = "branch"
	ModeReview = "review"
)

// prepareMR makes sure the main clone exists and the merge request head plus
// its target branch are on disk. It returns the main clone and the head commit.
func (m *Manager) prepareMR(mr forge.MergeRequest, project forge.Project) (string, string, error) {
	if project.PathWithNamespace == "" {
		return "", "", fmt.Errorf("unknown project path for merge request !%d - refresh the project index", mr.IID)
	}
	mainDir, err := m.ensureMain(project)
	if err != nil {
		return "", "", err
	}
	m.git.WorktreePrune(mainDir)
	if err := m.git.FetchRefspec(mainDir, m.headRef(mr.IID)); err != nil {
		return "", "", err
	}
	head, err := m.git.RevParse(mainDir, "FETCH_HEAD")
	if err != nil {
		return "", "", err
	}
	// The target branch is what the diff is measured against.
	if mr.TargetBranch != "" {
		if err := m.git.FetchRefspec(mainDir, mr.TargetBranch); err != nil {
			m.log("! could not fetch %s, the diff base may be approximate", mr.TargetBranch)
		}
	}
	return mainDir, head, nil
}

// resolveBase picks the commit the merge request should be diffed against:
// GitLab's own base when we have it, the local merge base otherwise.
func (m *Manager) resolveBase(mainDir string, mr forge.MergeRequest, rev Review, head string) string {
	if m.git.CommitExists(mainDir, rev.BaseSHA) {
		return rev.BaseSHA
	}
	if mr.TargetBranch != "" {
		if base, err := m.git.MergeBase(mainDir, "origin/"+mr.TargetBranch, head); err == nil && base != "" {
			return base
		}
	}
	return ""
}

// EnsureMRReview prepares a worktree in which the whole merge request shows up
// as one pending change: HEAD and the index stay on the merge base while the
// working tree holds the merge request head. Diff tools, gutter signs and hunk
// navigation then work on the change as a whole, instead of on the individual
// commits.
func (m *Manager) EnsureMRReview(mr forge.MergeRequest, project forge.Project, rev Review) (string, error) {
	projectPath := project.PathWithNamespace
	mainDir, head, err := m.prepareMR(mr, project)
	if err != nil {
		return "", err
	}
	if m.git.CommitExists(mainDir, rev.HeadSHA) {
		head = rev.HeadSHA
	}
	base := m.resolveBase(mainDir, mr, rev, head)
	if base == "" {
		return "", fmt.Errorf("cannot work out what !%d branched from - is %s on origin?", mr.IID, mr.TargetBranch)
	}

	dir := m.ReviewDir(projectPath, mr.IID, mr.SourceBranch)
	meta := Meta{
		IID: mr.IID, Project: projectPath, Source: mr.SourceBranch, Target: mr.TargetBranch,
		Base: base, Head: head, URL: mr.WebURL, Mode: ModeReview,
	}

	if Exists(dir) {
		m.log("Updating review worktree for !%d", mr.IID)
		// Everything the merge request changes is pending here by design, so
		// the reviewer's own edits are what the worktree adds on top of the
		// head it was last given - and those must survive.
		if edits := m.ownEdits(dir, m.ReadMeta(dir)); len(edits) > 0 {
			m.log("! %d file(s) with your own edits, leaving the worktree alone", len(edits))
			m.writeMeta(dir, meta)
			return dir, nil
		}
		if err := m.git.ResetHard(dir, base); err != nil {
			return dir, err
		}
		if err := m.pendChange(dir, base, head); err != nil {
			return dir, err
		}
		m.writeMeta(dir, meta)
		return dir, nil
	}

	m.log("Creating review worktree for !%d (%s → %s)", mr.IID, mr.SourceBranch, mr.TargetBranch)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if err := m.git.WorktreeAddDetached(mainDir, dir, base); err != nil {
		return "", err
	}
	if err := m.pendChange(dir, base, head); err != nil {
		return "", err
	}
	m.writeMeta(dir, meta)
	m.log("The merge request is now pending on top of the merge base:")
	m.log("  git diff                 the whole change")
	m.log("  git config unagit.mr.base / .head   the two ends of it")
	return dir, nil
}

// pendChange puts the merge request in the working tree while leaving HEAD and
// the index on the merge base, so the change reads as unstaged work.
//
// Unstaged is what makes it show up everywhere without being told: a plain git
// diff, and every editor that draws its gutter, compare the file against the
// index. Staging it instead would leave both of them with nothing to report.
func (m *Manager) pendChange(dir, base, head string) error {
	if err := m.git.ReadTree(dir, head); err != nil {
		return err
	}
	// read-tree moved the index along with the working tree; putting the index
	// back on HEAD is what turns the change from staged into pending.
	if err := m.git.ResetIndex(dir); err != nil {
		return err
	}
	// Files the merge request adds would now be untracked, and untracked files
	// are what git diff passes over. Intent-to-add entries make them read as
	// new files instead, without putting their content in the index.
	return m.git.IntentToAdd(dir, m.git.AddedPaths(dir, base, head))
}

// ownEdits lists what the reviewer changed on top of the head unagit last put
// in the worktree.
func (m *Manager) ownEdits(dir string, previous Meta) []string {
	if previous.Head == "" || !m.git.CommitExists(dir, previous.Head) {
		// Nothing to compare against, so everything pending has to count as
		// the reviewer's: better a worktree that will not update than one that
		// throws away work it could not account for.
		return m.git.UnstagedFiles(dir)
	}
	return m.git.ChangedSince(dir, previous.Head)
}

// writeMeta records the merge request in the worktree's own configuration.
// Failures are not fatal: they cost an editor integration, not the checkout.
func (m *Manager) writeMeta(dir string, meta Meta) {
	mainDir := m.ProjectDir(meta.Project)
	if err := m.git.EnableWorktreeConfig(mainDir); err != nil {
		m.log("! per-worktree config unavailable, skipping the merge request metadata")
		return
	}
	for key, value := range map[string]string{
		"unagit.mr.iid":     fmt.Sprintf("%d", meta.IID),
		"unagit.mr.project": meta.Project,
		"unagit.mr.source":  meta.Source,
		"unagit.mr.target":  meta.Target,
		"unagit.mr.base":    meta.Base,
		"unagit.mr.head":    meta.Head,
		"unagit.mr.url":     meta.URL,
		"unagit.mr.mode":    meta.Mode,
	} {
		if value == "" {
			continue
		}
		if err := m.git.SetWorktreeConfig(dir, key, value); err != nil {
			m.log("! could not write %s", key)
			return
		}
	}
}

// ReadMeta reads back what unagit recorded for a worktree.
func (m *Manager) ReadMeta(dir string) Meta {
	get := func(key string) string { return m.git.WorktreeConfig(dir, "unagit.mr."+key) }
	meta := Meta{
		Project: get("project"),
		Source:  get("source"),
		Target:  get("target"),
		Base:    get("base"),
		Head:    get("head"),
		URL:     get("url"),
		Mode:    get("mode"),
	}
	fmt.Sscanf(get("iid"), "%d", &meta.IID)
	return meta
}
