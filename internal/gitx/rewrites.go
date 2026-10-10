package gitx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Every change unagit makes to a branch's history - a squash, a message
// edited, a commit undone, a rebase, a base set, a branch deleted - is
// written down, so that it can be undone: what the branch was before and
// after, and its configuration before. The record is the repository's, in
// its git directory where every worktree of it finds it, and each state
// is held by a ref of its own under refs/unagit/rewrites, so that git does
// not throw the commits away while the record still names them. The refs
// are not pushed: a push sends branches and tags.
//
// An undo is a rewrite too, and is written down the same way, so it can be
// undone in its turn: nothing a rewrite left behind is lost while the
// record keeps it.

// Kinds of rewrite.
const (
	RewriteSquash     = "squash"
	RewriteReword     = "reword"
	RewriteUndoCommit = "undo commit"
	RewriteRebase     = "rebase"
	RewriteBase       = "set base"
	RewriteDelete     = "delete"
	RewriteUndo       = "undo"
	RewriteRecover    = "recover"
	RewriteDropShelf  = "delete shelf"
)

// The record keeps the newest rewrites, and none older than this.
const (
	rewritesKept = 200
	rewritesAge  = 90 * 24 * time.Hour
)

// Rewrite is one change to a branch, as the record keeps it.
type Rewrite struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Branch string    `json:"branch"`
	// Dir is the checkout it was done in; What says what was done, in a
	// few words: "squashed 3 commits".
	Dir  string `json:"dir"`
	What string `json:"what"`
	// Before and After are the branch's tip: "" before a branch that was
	// made, after one that was deleted.
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// Files tells a rewrite that changed the files as well, a rebase:
	// undone, they are put back too. The others change the history alone.
	Files bool `json:"files,omitempty"`
	// Config is the branch's configuration before, each "key=value".
	Config []string `json:"config,omitempty"`
	// Shelf is git's line for a shelf deleted, to put it back under.
	Shelf string `json:"shelf,omitempty"`
	// UndoneBy is the undo that took it back; Undid what an undo took back.
	UndoneBy string   `json:"undone_by,omitempty"`
	Undid    []string `json:"undid,omitempty"`
}

// RewriteChange is what a rewrite is about to do, for the record.
type RewriteChange struct {
	Kind, What string
	Files      bool
}

// Rewriting runs rewrite, a change to the history of the branch checked
// out in dir, and writes it down. When the change moved the branch off what
// its upstream has, where the upstream stood is noted (SetRebasedFrom): P
// then force-pushes with exactly that as the lease, so nothing pushed since
// can be lost. A rebase that only brought the branch forward rewrote
// nothing and is not written down.
func (g *Git) Rewriting(dir string, c RewriteChange, rewrite func() error) (Rewrite, error) {
	branch, _ := g.out(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branch == "" {
		if err := rewrite(); err != nil {
			return Rewrite{}, err
		}
		return Rewrite{}, nil
	}
	return g.rewritingBranch(dir, branch, c, rewrite)
}

// rewritingBranch is Rewriting for a branch, checked out or not.
func (g *Git) rewritingBranch(dir, branch string, c RewriteChange, rewrite func() error) (Rewrite, error) {
	before := g.branchTip(dir, branch)
	config := g.branchSettings(dir, branch)
	upstreamAt, upstreamErr := g.out(dir, "rev-parse", "--verify", "--quiet", branch+"@{upstream}")
	if err := rewrite(); err != nil {
		return Rewrite{}, err
	}
	after := g.branchTip(dir, branch)
	if after != "" && upstreamErr == nil && upstreamAt != "" && !g.IsAncestor(dir, upstreamAt, after) {
		_ = g.SetRebasedFrom(dir, branch, upstreamAt)
	}
	sameConfig := slices.Equal(config, g.branchSettings(dir, branch))
	switch {
	case before == after && sameConfig:
		return Rewrite{}, nil
	case c.Kind == RewriteRebase && sameConfig && before != "" && g.IsAncestor(dir, before, after):
		return Rewrite{}, nil
	}
	return g.record(dir, Rewrite{Kind: c.Kind, Branch: branch, Dir: dir, What: c.What,
		Before: before, After: after, Files: c.Files, Config: config})
}

// RecordDeletion writes down a branch deleted where git keeps no trace of
// it - on origin - so that it can be made again here at sha.
func (g *Git) RecordDeletion(dir, branch, sha, what string) (Rewrite, error) {
	return g.record(dir, Rewrite{Kind: RewriteDelete, Branch: branch, Dir: dir, What: what, Before: sha})
}

// Rewrites is the record of the repository dir is in, newest first.
func (g *Git) Rewrites(dir string) []Rewrite {
	path, err := g.rewritesPath(dir)
	if err != nil {
		return nil
	}
	all := readRewrites(path)
	slices.Reverse(all)
	return all
}

// branchTip is where a branch is, "" when there is no such branch.
func (g *Git) branchTip(dir, branch string) string {
	sha, err := g.out(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	return sha
}

// branchSettings is a branch's configuration, each "key=value", sorted.
func (g *Git) branchSettings(dir, branch string) []string {
	out, err := g.out(dir, "config", "--get-regexp", `^branch\.`+regexp.QuoteMeta(branch)+`\.`)
	if err != nil || out == "" {
		return nil
	}
	var settings []string
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(line, " ")
		settings = append(settings, key+"="+value)
	}
	slices.Sort(settings)
	return settings
}

// restoreSettings puts a branch's configuration back as it was.
func (g *Git) restoreSettings(dir, branch string, settings []string) error {
	_, _ = g.Run(dir, "config", "--remove-section", "branch."+branch)
	for _, s := range settings {
		key, value, _ := strings.Cut(s, "=")
		if _, err := g.Run(dir, "config", "--add", key, value); err != nil {
			return err
		}
	}
	return nil
}

// rewritesPath is the record's file, in the git directory the repository's
// worktrees share.
func (g *Git) rewritesPath(dir string) (string, error) {
	common, err := g.out(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return filepath.Join(common, "unagit", "rewrites.json"), nil
}

// record adds a rewrite, the refs that hold its states, and lets go of
// those the record no longer keeps.
func (g *Git) record(dir string, r Rewrite) (Rewrite, error) {
	r.ID = strconv.FormatInt(time.Now().UnixNano(), 36)
	if r.At.IsZero() {
		r.At = time.Now()
	}
	err := g.withRecord(dir, func(all []Rewrite) ([]Rewrite, error) {
		for _, state := range []struct{ name, sha string }{{"before", r.Before}, {"after", r.After}} {
			if state.sha == "" {
				continue
			}
			if _, err := g.Run(dir, "update-ref", rewriteRef(r.ID, state.name), state.sha); err != nil {
				return nil, err
			}
		}
		all = append(all, r)
		cut := time.Now().Add(-rewritesAge)
		from := 0
		for from < len(all) && all[from].At.Before(cut) {
			from++
		}
		from = max(from, len(all)-rewritesKept)
		for _, old := range all[:from] {
			_, _ = g.Run(dir, "update-ref", "-d", rewriteRef(old.ID, "before"))
			_, _ = g.Run(dir, "update-ref", "-d", rewriteRef(old.ID, "after"))
		}
		return all[from:], nil
	})
	return r, err
}

func rewriteRef(id, state string) string { return "refs/unagit/rewrites/" + id + "/" + state }

// withRecord changes the record under its lock, each instance of unagit in
// turn: change gets it oldest first and answers it as it is to be kept.
func (g *Git) withRecord(dir string, change func(all []Rewrite) ([]Rewrite, error)) error {
	path, err := g.rewritesPath(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	all, err := change(readRewrites(path))
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readRewrites(path string) []Rewrite {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var all []Rewrite
	_ = json.Unmarshal(b, &all)
	return all
}

// ----------------------------------------------------------------- undo

// UndoPlan is what undoing a rewrite would do, for the question before it.
type UndoPlan struct {
	// Chain is the rewrite and the later ones of its branch not undone
	// yet, oldest first: the branch goes back to before all of them.
	Chain []Rewrite
	// To is where the branch goes, "" for a branch deleted again - which
	// never happens: undoing a deletion makes the branch.
	To string
	// Since counts the commits made on the branch after the newest of the
	// chain, which leave it too; Moved says the branch is somewhere the
	// chain does not explain, neither there nor ahead of it.
	Since int
	Moved bool
	// Force tells that origin has what the branch leaves: a force push
	// will be needed to make origin follow.
	Force bool
	// Files tells that the files go back as well, not only the history.
	Files bool
	// Blocked says why it cannot be undone, "" when it can.
	Blocked string
}

// ErrUndone marks a rewrite that was undone already.
var ErrUndone = errors.New("it was undone already")

// PlanUndo says what undoing the rewrite id would do.
func (g *Git) PlanUndo(dir, id string) (UndoPlan, error) {
	all := g.Rewrites(dir)
	slices.Reverse(all)
	at := slices.IndexFunc(all, func(r Rewrite) bool { return r.ID == id })
	if at < 0 {
		return UndoPlan{}, fmt.Errorf("the record no longer has it - it keeps %d rewrites and %d days", rewritesKept, int(rewritesAge.Hours()/24))
	}
	r := all[at]
	if undone(all, r) {
		return UndoPlan{}, ErrUndone
	}
	// A shelf put back is undone by deleting it again, on the shelf.
	if r.Kind == RewriteUndo && r.Branch == "" {
		return UndoPlan{Chain: []Rewrite{r}, Blocked: "it put a shelf back - delete it again from the shelf"}, nil
	}
	// A shelf deleted is a thing of its own, not a branch's state.
	if r.Kind == RewriteDropShelf {
		plan := UndoPlan{Chain: []Rewrite{r}, To: r.Before}
		shelves, _ := g.Shelves(dir)
		switch {
		case slices.ContainsFunc(shelves, func(s Shelf) bool { return s.SHA == r.Before }):
			plan.Blocked = "it is on the shelf again"
		case !g.HasCommit(dir, r.Before):
			plan.Blocked = "git no longer has " + shortID(r.Before)
		}
		return plan, nil
	}
	plan := UndoPlan{Chain: []Rewrite{r}, To: r.Before, Files: r.Files}
	for _, later := range all[at+1:] {
		if later.Branch == r.Branch && !undone(all, later) {
			plan.Chain = append(plan.Chain, later)
			plan.Files = plan.Files || later.Files
		}
	}
	newest := plan.Chain[len(plan.Chain)-1]
	tip := g.branchTip(dir, r.Branch)
	switch {
	case r.Kind == RewriteDelete && len(plan.Chain) == 1 && tip != "":
		plan.Blocked = "a branch named " + r.Branch + " is here again - delete or rename it first"
	case r.Before == "":
		plan.Blocked = "there is nothing to go back to"
	case !g.HasCommit(dir, r.Before):
		plan.Blocked = "git no longer has " + shortID(r.Before)
	case tip != "" && newest.After != "" && tip != newest.After:
		if g.IsAncestor(dir, newest.After, tip) {
			plan.Since = g.Count(dir, newest.After+".."+tip)
		} else {
			plan.Moved = true
		}
	}
	if checkout := g.CheckedOut(dir)[r.Branch]; checkout != "" {
		if op := g.OperationInProgress(checkout); op != "" && plan.Blocked == "" {
			plan.Blocked = "a " + op + " is in progress in " + checkout + " - finish or abort it first"
		}
	}
	if up, err := g.out(dir, "rev-parse", "--verify", "--quiet", r.Branch+"@{upstream}"); err == nil && up != "" && r.Before != "" {
		plan.Force = !g.IsAncestor(dir, up, r.Before)
	}
	return plan, nil
}

// undone tells whether a rewrite is undone now: taken back by an undo that
// was not itself undone.
func undone(all []Rewrite, r Rewrite) bool {
	if r.UndoneBy == "" {
		return false
	}
	at := slices.IndexFunc(all, func(o Rewrite) bool { return o.ID == r.UndoneBy })
	return at < 0 || !undone(all, all[at])
}

// Undone tells whether a rewrite of the record is undone now.
func Undone(all []Rewrite, r Rewrite) bool { return undone(all, r) }

// UndoRewrite puts the branch of the rewrite id back as it was before it,
// the later rewrites of the branch with it, and its configuration too. The
// history alone goes back when that is all they changed - the files stay,
// and so does what is not committed; after a rebase the files go back as
// well, and what is not committed stays unless it collides, when nothing
// is done. The undo is written down, and can be undone in its turn.
func (g *Git) UndoRewrite(dir, id string) (Rewrite, error) {
	plan, err := g.PlanUndo(dir, id)
	if err != nil {
		return Rewrite{}, err
	}
	if plan.Blocked != "" {
		return Rewrite{}, errors.New(plan.Blocked)
	}
	r := plan.Chain[0]
	if r.Kind == RewriteDropShelf {
		return g.undoDropShelf(dir, r)
	}
	what := "undid: " + r.What
	if n := len(plan.Chain) - 1; n > 0 {
		what += fmt.Sprintf(" and %d after it", n)
	}
	checkout := g.CheckedOut(dir)[r.Branch]
	move := func() error {
		switch {
		case checkout == "":
			if _, err := g.Run(dir, "branch", "--force", "--no-track", r.Branch, plan.To); err != nil {
				return err
			}
		case plan.Files:
			if _, err := g.Run(checkout, "reset", "--keep", "--quiet", plan.To); err != nil {
				return fmt.Errorf("what is not committed in %s collides with the files going back - commit or stash it first: %w", checkout, err)
			}
		default:
			if _, err := g.Run(checkout, "reset", "--soft", "--quiet", plan.To); err != nil {
				return err
			}
		}
		return g.restoreSettings(dir, r.Branch, r.Config)
	}
	where := dir
	if checkout != "" {
		where = checkout
	}
	undo, err := g.rewritingBranch(where, r.Branch, RewriteChange{Kind: RewriteUndo, What: what, Files: plan.Files}, move)
	if err != nil {
		return Rewrite{}, err
	}
	if undo.ID == "" {
		return undo, nil
	}
	ids := make([]string, len(plan.Chain))
	for i, c := range plan.Chain {
		ids[i] = c.ID
	}
	err = g.withRecord(dir, func(all []Rewrite) ([]Rewrite, error) {
		for i := range all {
			switch {
			case slices.Contains(ids, all[i].ID):
				all[i].UndoneBy = undo.ID
			case all[i].ID == undo.ID:
				all[i].Undid = ids
			}
		}
		return all, nil
	})
	undo.Undid = ids
	return undo, err
}

// ReflogEntry is one place a branch has been, as git's reflog keeps it.
type ReflogEntry struct {
	SHA string
	At  time.Time
	// Action is what moved it there, in git's words: "commit: …",
	// "rebase (finish): …", "reset: moving to HEAD~1".
	Action  string
	Subject string
}

// Reflog is where the branch has been, newest first - whatever moved it,
// unagit or anything else - at most limit places; HEAD's for "".
func (g *Git) Reflog(dir, branch string, limit int) ([]ReflogEntry, error) {
	ref := "HEAD"
	if branch != "" {
		ref = "refs/heads/" + branch
	}
	out, err := g.out(dir, "log", "-g", "-n", strconv.Itoa(limit), "--date=unix", "--format=%H%x1f%gd%x1f%gs%x1f%s", ref, "--")
	if err != nil || out == "" {
		return nil, err
	}
	var entries []ReflogEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\x1f", 4)
		if len(f) < 4 {
			continue
		}
		e := ReflogEntry{SHA: f[0], Action: f[2], Subject: f[3]}
		if open := strings.LastIndex(f[1], "@{"); open >= 0 {
			if unix, err := strconv.ParseInt(strings.TrimSuffix(f[1][open+2:], "}"), 10, 64); err == nil {
				e.At = time.Unix(unix, 0)
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// RecoverTo puts the branch checked out in dir back where it was at sha,
// the files with it: what is not committed stays, and when it would
// collide nothing is done. It is written down, so it can be undone.
func (g *Git) RecoverTo(dir, sha, what string) (Rewrite, error) {
	return g.Rewriting(dir, RewriteChange{Kind: RewriteRecover, What: what, Files: true}, func() error {
		if op := g.OperationInProgress(dir); op != "" {
			return fmt.Errorf("a %s is in progress - finish or abort it first", op)
		}
		if _, err := g.Run(dir, "reset", "--keep", "--quiet", sha); err != nil {
			return fmt.Errorf("what is not committed collides with the files going back - commit or stash it first: %w", err)
		}
		return nil
	})
}

// ForceRemoves lists, newest first, the commits of origin's copy that a
// force push would take off it - those of at, the upstream when "", that
// HEAD lacks - each its short id and subject.
func (g *Git) ForceRemoves(dir, at string) []string {
	if at == "" {
		at = "@{upstream}"
	}
	return g.RewriteCommits(dir, at, "HEAD")
}

// undoDropShelf puts a deleted shelf back, and writes that down.
func (g *Git) undoDropShelf(dir string, r Rewrite) (Rewrite, error) {
	if _, err := g.Run(dir, "stash", "store", "--message", r.Shelf, r.Before); err != nil {
		return Rewrite{}, err
	}
	undo, err := g.record(dir, Rewrite{Kind: RewriteUndo, Dir: dir, What: "undid: " + r.What, After: r.Before, Undid: []string{r.ID}})
	if err != nil {
		return undo, err
	}
	return undo, g.withRecord(dir, func(all []Rewrite) ([]Rewrite, error) {
		for i := range all {
			if all[i].ID == r.ID {
				all[i].UndoneBy = undo.ID
			}
		}
		return all, nil
	})
}

// RewriteCommits lists, newest first, the commits of a state of a rewrite
// that the other state lacks - before against after, or after against
// before - each its short id and subject: what a squash made of what.
func (g *Git) RewriteCommits(dir, from, against string) []string {
	if from == "" {
		return nil
	}
	args := []string{"log", "--format=%h %s", "-50", from}
	if against != "" {
		args = append(args, "^"+against)
	}
	out, err := g.out(dir, args...)
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
