package ui

import (
	"fmt"
	"path/filepath"
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
	f.dirty = gitLines(git, r.Dir, "status", "--porcelain", "--untracked-files=all")
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

func (a *App) mrLine(r worktreeRow) string {
	mr, ok := a.openMRFor(r)
	if !ok {
		return tag(colDim) + "no open merge request · n creates one" + tagEnd
	}
	line := tag(colAccent) + fmt.Sprintf("!%d", mr.IID) + tagEnd + " " + esc(mr.Title)
	if mr.WebURL != "" {
		// Under the title, in the value column.
		line += "\n" + strings.Repeat(" ", 14) + tag(colDim) + esc(mr.WebURL) + tagEnd
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

// showWorktreeDetail fills the detail column with everything worth knowing
// about one worktree.
func (a *App) showWorktreeDetail(r worktreeRow, focus bool) {
	p := a.worktreesPane
	p.detailSeq++
	seq := p.detailSeq
	title := r.Path + " · " + r.Branch
	st, known := a.wtRemote[r.Dir]

	render := func(f wtFacts) string {
		d := &detailBuf{}
		d.title(r.Branch)
		d.sub(r.Path)
		d.section("Worktree")
		if a.multiInstance() {
			d.kv("Server", esc(a.instanceLabel(r.Instance)))
		}
		d.kv("Repository", esc(r.Path))
		d.kv("Branch", tag(colBranch)+esc(r.Branch)+tagEnd)
		d.kv("Made from", baseLine(st, f))
		d.kv("Remote", a.remoteLine(st, known))
		d.kv("Merge request", a.mrLine(r))
		d.kv("State", stateLine(f))
		if f.head != "" {
			d.kv("HEAD", esc(f.head))
		}
		d.kv("Directory", esc(tildePath(r.Dir)))
		if !r.Moved.IsZero() {
			d.kv("Last moved", humanAge(r.Moved))
		}
		writeFacts(d, st, f)
		return d.String()
	}
	p.openDetail(title, render(wtFacts{}), focus)

	go func() {
		f := a.gatherFacts(r, st)
		a.tv.QueueUpdateDraw(func() {
			if p.detailSeq == seq {
				p.setDetail(title, render(f))
			}
		})
	}()
}

// showGroupDetail fills the detail column with a grouped worktree: the folder,
// then every repository in it, each with the same facts a worktree of its own
// shows, the lists kept short so that all of them fit one screen or two.
func (a *App) showGroupDetail(r worktreeRow, focus bool) {
	p := a.worktreesPane
	p.detailSeq++
	seq := p.detailSeq
	title := r.Path + " · " + fmt.Sprintf("%d repositories", len(r.Members))

	render := func(facts []wtFacts) string {
		d := &detailBuf{}
		d.title(r.Path)
		d.sub(fmt.Sprintf("grouped worktree · %d repositories", len(r.Members)))
		d.section("Grouped worktree")
		d.kv("Directory", esc(tildePath(r.Dir)))
		if r.Branch != "" {
			d.kv("Branch", tag(colBranch)+esc(r.Branch)+tagEnd)
		} else {
			d.kv("Branch", tag(colMuted)+esc(a.worktreeBranch(r))+tagEnd)
		}
		plain, colour := a.worktreeRemoteWords(r)
		d.kv("Remote", tag(colour)+esc(plain)+tagEnd)
		if !r.Moved.IsZero() {
			d.kv("Last moved", humanAge(r.Moved))
		}
		for i, m := range r.Members {
			st, known := a.wtRemote[m.Dir]
			f := wtFacts{}
			if i < len(facts) {
				f = facts[i]
			}
			d.section(filepath.Base(m.Dir))
			d.kv("Repository", esc(m.Path))
			if a.multiInstance() {
				d.kv("Server", esc(a.instanceLabel(m.Instance)))
			}
			d.kv("Branch", tag(colBranch)+esc(m.Branch)+tagEnd)
			d.kv("Made from", baseLine(st, f))
			d.kv("Remote", a.remoteLine(st, known))
			d.kv("Merge request", a.mrLine(m))
			d.kv("State", stateLine(f))
			if f.head != "" {
				d.kv("HEAD", esc(f.head))
			}
			if f.loaded && len(f.stat) > 0 {
				// The last line of --stat is the summary: files, insertions, deletions.
				d.kv("Changes", esc(strings.TrimSpace(f.stat[len(f.stat)-1])))
			}
			short := func(heading string, items []string, total int, colour string) {
				if len(items) == 0 {
					return
				}
				d.raw(tag(colDim) + heading + tagEnd + "\n")
				d.lines(items[:min(len(items), 5)], total, colour)
			}
			if f.onto != "" {
				short("Its own commits", f.own, f.ownCount, tag(colText))
				short("New on "+f.onto, f.incoming, len(f.incoming), tag(colText))
			}
			short("Uncommitted", f.dirty, len(f.dirty), tag(colWarn))
		}
		return d.String()
	}
	p.openDetail(title, render(nil), focus)

	members := r.Members
	states := make([]remoteState, len(members))
	for i, m := range members {
		states[i] = a.wtRemote[m.Dir]
	}
	go func() {
		facts := make([]wtFacts, len(members))
		for i, m := range members {
			facts[i] = a.gatherFacts(m, states[i])
		}
		a.tv.QueueUpdateDraw(func() {
			if p.detailSeq == seq {
				p.setDetail(title, render(facts))
			}
		})
	}()
}
