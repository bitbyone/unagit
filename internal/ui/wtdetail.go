package ui

import (
	"fmt"
	"strings"

	"github.com/tobola/unagit/internal/gitx"
)

// The detail of a worktree is where to look before deciding anything about
// it: what it was made from and how far that has moved, what it holds that
// origin does not, what is not committed, and what would change on a merge.
// Git is asked for it in the background; the outline shows until it answers.

// wtFacts is what git says about one worktree.
type wtFacts struct {
	loaded bool
	head   string
	busy   string // a merge or rebase git is in the middle of
	dirty  []string
	// onto is the base as git names it; own the commits since, ownCount all
	// of them; incoming what the base has that this branch lacks.
	onto     string
	own      []string
	ownCount int
	incoming []string
	// unpushed and pulled are against the branch's own upstream.
	unpushed []string
	pulled   []string
	stat     []string // what the branch changes against its base, file by file
}

// listLimit keeps a long history from pushing everything else out of view.
const listLimit = 12

func gitLines(git *gitx.Git, dir string, args ...string) []string {
	out, err := git.Run(dir, args...)
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// gatherFacts asks git about a worktree. It runs off the event loop.
func (a *App) gatherFacts(r worktreeRow, st remoteState) wtFacts {
	mgr := a.pathManager(r.Instance, r.Path)
	git := mgr.Git()
	f := wtFacts{loaded: true}
	if head := gitLines(git, r.Dir, "log", "-1", "--format=%h  %s  (%cr)"); len(head) > 0 {
		f.head = head[0]
	}
	f.busy = mgr.OperationInProgress(r.Dir)
	f.dirty = gitLines(git, r.Dir, "--no-optional-locks", "status", "--porcelain", "--untracked-files=all")
	logFormat := "--format=%h  %s  (%an, %cr)"

	f.onto = st.Onto
	if f.onto == "" && st.Base != "" {
		f.onto = git.BaseRef(r.Dir, st.Base)
	}
	if f.onto != "" {
		f.own = gitLines(git, r.Dir, "log", fmt.Sprintf("-%d", listLimit), logFormat, f.onto+"..HEAD")
		f.ownCount = git.Count(r.Dir, f.onto+"..HEAD")
		f.incoming = gitLines(git, r.Dir, "log", fmt.Sprintf("-%d", listLimit), logFormat, "HEAD.."+f.onto)
		f.stat = gitLines(git, r.Dir, "diff", "--stat=72", f.onto+"...HEAD")
	} else {
		f.own = gitLines(git, r.Dir, "log", "-8", logFormat)
	}
	if st.Upstream.Name != "" && !st.Upstream.Gone {
		f.unpushed = gitLines(git, r.Dir, "log", fmt.Sprintf("-%d", listLimit), logFormat, "@{upstream}..HEAD")
		f.pulled = gitLines(git, r.Dir, "log", fmt.Sprintf("-%d", listLimit), logFormat, "HEAD..@{upstream}")
	}
	return f
}

// lines writes a list into the detail, cut at listLimit with the rest counted.
func (d *detailBuf) lines(items []string, total int, colour string) {
	for _, l := range items {
		d.raw("  " + colour + esc(l) + tagEnd + "\n")
	}
	if total > len(items) {
		d.raw(fmt.Sprintf("  %s… and %d more%s\n", tag(colDim), total-len(items), tagEnd))
	}
}

// baseLine says what the branch was made from and how the two stand.
func baseLine(st remoteState, f wtFacts) string {
	if st.Base == "" {
		return tag(colDim) + "not known - unagit did not make this branch" + tagEnd
	}
	text := tag(colBranch) + esc(st.Base) + tagEnd
	if !f.loaded {
		return text
	}
	if f.onto == "" {
		return text + tag(colBad) + " · gone from origin and from the clone" + tagEnd
	}
	own := fmt.Sprintf("%d commit(s) of its own", f.ownCount)
	if f.ownCount == 0 {
		own = "no commits of its own yet"
	}
	parts := []string{own}
	if n := len(f.incoming); n > 0 {
		more := ""
		if n >= listLimit {
			more = "+"
		}
		parts = append(parts, tag(colWarn)+fmt.Sprintf("%d%s new on %s", n, more, f.onto)+tagEnd)
	} else {
		parts = append(parts, tag(colOn)+"up to date with "+esc(f.onto)+tagEnd)
	}
	return text + tag(colMuted) + " · " + tagEnd + strings.Join(parts, tag(colMuted)+" · "+tagEnd)
}

// stateLine is the working tree in a few words; where the branch stands
// against origin is the Remote line's.
func stateLine(f wtFacts) string {
	switch {
	case !f.loaded:
		return tag(colMuted) + "looking …" + tagEnd
	case f.busy != "":
		return tag(colBad) + "a " + f.busy + " is in progress" + tagEnd
	case len(f.dirty) > 0:
		return tag(colWarn) + fmt.Sprintf("%d uncommitted file(s)", len(f.dirty)) + tagEnd
	}
	return tag(colOn) + "clean" + tagEnd
}

func (a *App) remoteLine(st remoteState, known bool) string {
	plain, name, colour := remoteWords(st, known)
	text := tag(colour) + esc(a.remoteSentence(st, known, plain)) + tagEnd
	if name != "" {
		text += tag(colDim) + " with " + esc(name) + tagEnd
	}
	return text
}

// commentsLine says what Incomm holds on a worktree; nothing with Incomm off.
func (a *App) commentsLine(st remoteState) string {
	if !a.cfg.Integrations.Incomm {
		return ""
	}
	if st.Comments == 0 {
		return tag(colDim) + "none in Incomm · r brings the merge request's" + tagEnd
	}
	line := fmt.Sprintf("%d in Incomm", st.Comments)
	if st.Pending > 0 {
		return line + tag(colWarn) + fmt.Sprintf(" · %d not published · P in Merge requests", st.Pending) + tagEnd
	}
	return line
}

// writeFacts writes the lists git answered with, under headings.
func writeFacts(d *detailBuf, st remoteState, f wtFacts) {
	if !f.loaded {
		return
	}
	if len(f.dirty) > 0 {
		d.section(fmt.Sprintf("Uncommitted · %d", len(f.dirty)))
		shown := f.dirty[:min(len(f.dirty), listLimit)]
		d.lines(shown, len(f.dirty), tag(colWarn))
	}
	if len(f.unpushed) > 0 {
		d.section("Not on origin yet")
		d.lines(f.unpushed, st.Upstream.Ahead, tag(colText))
	}
	if len(f.pulled) > 0 {
		d.section("On origin, not here")
		d.lines(f.pulled, st.Upstream.Behind, tag(colText))
	}
	if f.onto != "" {
		d.section(fmt.Sprintf("Since %s · %d", f.onto, f.ownCount))
		if len(f.own) == 0 {
			d.raw("  " + tag(colDim) + "nothing yet" + tagEnd + "\n")
		}
		d.lines(f.own, f.ownCount, tag(colText))
		if len(f.incoming) > 0 {
			d.section("New on " + f.onto)
			d.lines(f.incoming, len(f.incoming), tag(colText))
		}
		if len(f.stat) > 0 {
			d.section("Changes against " + f.onto)
			d.lines(f.stat, len(f.stat), tag(colMuted))
		}
	} else if len(f.own) > 0 {
		d.section("Latest commits")
		d.lines(f.own, len(f.own), tag(colText))
	}
}
