package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tobola/unagit/internal/gitlab"
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
func (m *Manager) prepareMR(mr gitlab.MergeRequest, projectPath, httpURL string) (string, string, error) {
	if projectPath == "" {
		return "", "", fmt.Errorf("unknown project path for merge request !%d - refresh the project index", mr.IID)
	}
	mainDir, err := m.ensureMain(projectPath, httpURL)
	if err != nil {
		return "", "", err
	}
	m.git.WorktreePrune(mainDir)
	if err := m.git.FetchRefspec(mainDir, fmt.Sprintf("refs/merge-requests/%d/head", mr.IID)); err != nil {
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
func (m *Manager) resolveBase(mainDir string, mr gitlab.MergeRequest, rev Review, head string) string {
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
// as pending changes: HEAD stays on the merge base while the index and the
// working tree hold the merge request head. Diff tools, gutter signs and
// hunk navigation then work on the change as a whole, instead of on the
// individual commits.
func (m *Manager) EnsureMRReview(mr gitlab.MergeRequest, projectPath, httpURL string, rev Review) (string, error) {
	mainDir, head, err := m.prepareMR(mr, projectPath, httpURL)
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
		// The index always differs from HEAD here, so "dirty" means the
		// reviewer's own unstaged edits - those must survive.
		if edits := m.git.UnstagedFiles(dir); len(edits) > 0 {
			m.log("! %d file(s) with your own edits, leaving the worktree alone", len(edits))
			m.writeMeta(dir, meta)
			return dir, nil
		}
		if err := m.git.ResetHard(dir, base); err != nil {
			return dir, err
		}
		if err := m.git.ReadTree(dir, head); err != nil {
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
	if err := m.git.ReadTree(dir, head); err != nil {
		return "", err
	}
	m.writeMeta(dir, meta)
	m.log("The merge request is now staged on top of the merge base:")
	m.log("  git diff --staged        the whole change")
	m.log("  git config unagit.mr.base / .head   the two ends of it")
	return dir, nil
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
