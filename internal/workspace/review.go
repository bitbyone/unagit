package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// Review pins the two commits GitLab itself renders a merge request diff from.
// BaseSHA is the merge base, not the tip of the target branch: the target may
// have moved on since, and diffing against its tip would show the target's own
// commits backwards.
//
// From narrows the review to the commits from that one up to the head, for a
// second look after the author answered in new commits. Empty is the whole
// merge request.
type Review struct {
	BaseSHA string
	HeadSHA string
	From    string
	// Seen, when set, is the head the reviewer marked as reviewed without
	// checking it out; it stands for the head the review last had.
	Seen string
}

// Meta is what unagit records in a worktree's own git configuration, so an
// editor can find out which merge request it is looking at and what to diff
// against:
//
//	git config unagit.mr.base    # what the pending change is measured from
//	git config unagit.mr.head    # the merge request head
//	git config unagit.mr.mode    # branch or review
//	git config unagit.mr.from    # the first commit shown, when not all are
//
// The base is the merge base unless the review was narrowed to its latest
// commits; it is always what HEAD is, so a diff against it matches the gutter.
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
	From    string
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
	if rev.From != "" {
		// From here on "base" is what the pending change starts at: the
		// parent of the first commit to show, rather than the merge base.
		if base, err = m.startOf(mainDir, base, head, rev.From); err != nil {
			return "", err
		}
	}

	dir := m.ReviewDir(projectPath, mr.IID, mr.SourceBranch)
	meta := Meta{
		IID: mr.IID, Project: projectPath, Source: mr.SourceBranch, Target: mr.TargetBranch,
		Base: base, Head: head, URL: mr.WebURL, Mode: ModeReview, From: rev.From,
	}

	if Exists(dir) {
		m.log("Updating review worktree for !%d", mr.IID)
		// Everything the merge request changes is pending here by design, so
		// the reviewer's own edits are what the worktree adds on top of the
		// head it was last given - and those must survive.
		edits := m.ownEdits(dir, m.ReadMeta(dir))
		// The reviewer's comments are not edits; they are set aside while
		// the worktree moves and put back after.
		if len(edits) > 0 && rev.From != "" {
			// Opening it anyway would show the whole change while the reviewer
			// asked for part of it, which reads as if the narrowing worked.
			return "", fmt.Errorf("%d file(s) in the review worktree have your own edits (%s) - "+
				"discard or stash them before narrowing the review", len(edits), strings.Join(edits, ", "))
		}
		if len(edits) > 0 {
			m.log("! %d file(s) with your own edits, leaving the worktree alone", len(edits))
			// Left alone, but not left showing the comments as a change.
			m.hideNotes(dir)
			m.writeMeta(dir, meta)
			return dir, nil
		}
		restore, err := m.setNotesAside(dir)
		if err != nil {
			return dir, err
		}
		if err := m.git.ResetHard(dir, base); err != nil {
			return dir, errors.Join(err, restore())
		}
		if err := m.pendChange(dir, base, head); err != nil {
			return dir, errors.Join(err, restore())
		}
		if err := restore(); err != nil {
			return dir, err
		}
		m.hideNotes(dir)
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
	m.hideNotes(dir)
	m.writeMeta(dir, meta)
	m.log("The merge request is now pending on top of the merge base:")
	m.log("  git diff                 the whole change")
	m.log("  git config unagit.mr.base / .head   the two ends of it")
	return dir, nil
}

// startOf is the commit a review narrowed to from..head is measured against:
// from's parent. from has to be one of the merge request's own commits, or the
// pending change would include work that is not the merge request's.
func (m *Manager) startOf(mainDir, base, head, from string) (string, error) {
	if !m.git.CommitExists(mainDir, from) || from == base ||
		!m.git.IsAncestor(mainDir, base, from) || !m.git.IsAncestor(mainDir, from, head) {
		return "", fmt.Errorf("commit %s is not part of the merge request any more - pick it again from the list", short(from))
	}
	return m.git.RevParse(mainDir, from+"^")
}

// MRCommit is one commit of a merge request, and whether the reviewer has not
// seen it yet.
type MRCommit struct {
	gitx.LogEntry
	New bool
}

// MRCommits lists the commits of a merge request, oldest first, as they are on
// the server now, read from a clone. It is the way round when the forge cannot
// list them; with a forge, MarkUnseen alone needs no clone.
func (m *Manager) MRCommits(mr forge.MergeRequest, project forge.Project, rev Review) ([]MRCommit, error) {
	mainDir, head, err := m.prepareMR(mr, project)
	if err != nil {
		return nil, err
	}
	if m.git.CommitExists(mainDir, rev.HeadSHA) {
		head = rev.HeadSHA
	}
	base := m.resolveBase(mainDir, mr, rev, head)
	if base == "" {
		return nil, fmt.Errorf("cannot work out what !%d branched from - is %s on origin?", mr.IID, mr.TargetBranch)
	}
	entries, err := m.git.Commits(mainDir, base, head)
	if err != nil {
		return nil, err
	}
	commits := make([]MRCommit, len(entries))
	for i, e := range entries {
		commits[i] = MRCommit{LogEntry: e}
	}
	if err := m.MarkUnseen(mr, project, Review{BaseSHA: base, HeadSHA: head}, commits); err != nil {
		m.log("! could not tell which commits are new: %v", err)
	}
	return commits, nil
}

// MarkUnseen marks the commits the review worktree has not been given yet: a
// commit is new when the head last checked out has neither it nor a rebased
// copy of it. Without a review worktree nothing was seen and nothing is
// marked - and nothing is cloned either, which is what lets the list of
// commits come from the forge alone. The clone is fetched only when the
// commits to compare are not in it yet.
func (m *Manager) MarkUnseen(mr forge.MergeRequest, project forge.Project, rev Review, commits []MRCommit) error {
	dir := m.ReviewDir(project.PathWithNamespace, mr.IID, mr.SourceBranch)
	mainDir := m.ProjectDir(project.PathWithNamespace)
	if !Exists(dir) || !Exists(mainDir) {
		return nil
	}
	seen := m.ReadMeta(dir).Head
	if rev.Seen != "" {
		seen = rev.Seen
	}
	if seen == "" || !m.git.CommitExists(mainDir, seen) || seen == rev.HeadSHA {
		return nil
	}
	head, base := rev.HeadSHA, rev.BaseSHA
	if head == "" || base == "" || !m.git.CommitExists(mainDir, head) || !m.git.CommitExists(mainDir, base) {
		var err error
		if mainDir, head, err = m.prepareMR(mr, project); err != nil {
			return err
		}
		if m.git.CommitExists(mainDir, rev.HeadSHA) {
			head = rev.HeadSHA
		}
		if base = m.resolveBase(mainDir, mr, rev, head); base == "" {
			return fmt.Errorf("cannot work out what !%d branched from - is %s on origin?", mr.IID, mr.TargetBranch)
		}
	}
	unseen, err := m.git.Unseen(mainDir, seen, head, base)
	if err != nil {
		return err
	}
	for i := range commits {
		commits[i].New = unseen[commits[i].SHA]
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
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
	// new files instead, without putting their content in the index. Comment
	// stores are left untracked: they are not part of what is reviewed.
	var added []string
	for _, path := range m.git.AddedPaths(dir, base, head) {
		if !isNotes(path) {
			added = append(added, path)
		}
	}
	return m.git.IntentToAdd(dir, added)
}

// notesDir is where Incomm keeps a worktree's comments. A repository may
// commit it, but in a review it is the reviewer's notebook, not part of the
// change: a comment written or imported there must not show up as a change,
// nor count as an edit that stops the worktree from following the merge
// request.
const notesDir = ".incomm"

func isNotes(path string) bool {
	return path == notesDir || strings.HasPrefix(path, notesDir+"/")
}

// hideNotes keeps the comment stores out of every diff and status of the
// review worktree. The usual case is a repository that does not commit them:
// the directory is ignored through info/exclude, the local ignore file, which
// holds for every worktree of the repository. A repository that does commit
// it cannot have it ignored - ignoring passes tracked files by - so its
// tracked files are marked skip-worktree in the review worktree instead.
func (m *Manager) hideNotes(dir string) {
	paths := m.git.TrackedUnder(dir, notesDir)
	if len(paths) == 0 {
		if err := m.git.Exclude(dir, "/"+notesDir+"/"); err != nil {
			m.log("! could not ignore %s: %v", notesDir, err)
		}
		return
	}
	if err := m.git.SkipWorktree(dir, paths, true); err != nil {
		m.log("! could not hide %s from the diff", notesDir)
	}
}

// setNotesAside moves the comments out of the worktree before it is reset,
// and returns what puts them back - the reviewer's own, whatever the merge
// request has in its version of the directory.
func (m *Manager) setNotesAside(dir string) (func() error, error) {
	notes := filepath.Join(dir, notesDir)
	if _, err := os.Stat(notes); os.IsNotExist(err) {
		return func() error { return nil }, nil
	}
	// Hidden files would be left as they are by the reset; show them to it.
	if paths := m.git.TrackedUnder(dir, notesDir); len(paths) > 0 {
		_ = m.git.SkipWorktree(dir, paths, false)
	}
	aside := filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".notes-aside")
	if err := os.RemoveAll(aside); err != nil {
		return nil, err
	}
	if err := os.Rename(notes, aside); err != nil {
		return nil, fmt.Errorf("could not set the comments aside: %w", err)
	}
	return func() error {
		if err := os.RemoveAll(notes); err != nil {
			return err
		}
		if err := os.Rename(aside, notes); err != nil {
			return fmt.Errorf("your comments are in %s - move them back to %s: %w", aside, notes, err)
		}
		return nil
	}, nil
}

// ownEdits lists what the reviewer changed on top of the head unagit last put
// in the worktree.
func (m *Manager) ownEdits(dir string, previous Meta) []string {
	if previous.Head == "" || !m.git.CommitExists(dir, previous.Head) {
		// Nothing to compare against, so everything pending has to count as
		// the reviewer's: better a worktree that will not update than one that
		// throws away work it could not account for.
		return withoutNotes(m.git.UnstagedFiles(dir))
	}
	return withoutNotes(m.git.ChangedSince(dir, previous.Head))
}

func withoutNotes(paths []string) []string {
	var out []string
	for _, p := range paths {
		if !isNotes(p) {
			out = append(out, p)
		}
	}
	return out
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
	// A review opened whole again must not go on claiming to be narrowed.
	if meta.From == "" {
		m.git.UnsetWorktreeConfig(dir, "unagit.mr.from")
	} else if err := m.git.SetWorktreeConfig(dir, "unagit.mr.from", meta.From); err != nil {
		m.log("! could not write unagit.mr.from")
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
		From:    get("from"),
	}
	fmt.Sscanf(get("iid"), "%d", &meta.IID)
	return meta
}
