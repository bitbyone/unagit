package ui

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/workspace"
)

// Enter on a row of Worktrees opens the worktree as one view of its own: a
// block for every repository in it - one for a worktree of its own - and, for
// a grouped one, a block for the group above them. j/k move from block to
// block, and the keys act on the block that is lit: p updates the repository
// lit, or every one when the group is.

// pageWorktree is the worktree view.
const pageWorktree = "worktree"

// wtView is the worktree view while it is open.
type wtView struct {
	row    worktreeRow // as it was when last read from the disk
	at     int         // the lit block: 0 is the group's in a grouped worktree
	body   *blockList
	footer *tview.TextView
	frame  *tview.Flex
	facts  map[string]wtFacts // what git said, by directory
	// loaded is set once git has answered for every repository: the view
	// shows nothing of them until then, so that it is drawn whole, once,
	// rather than growing row by row. A reload keeps what is shown until the
	// new answer is complete.
	loaded  bool
	reading int // loads under way; the newest one wins
}

// blocks are what the view lights in turn: the group, then its repositories;
// a worktree of its own is its only block.
func (v *wtView) blocks() []worktreeRow {
	if !v.row.grouped() {
		return []worktreeRow{v.row}
	}
	return append([]worktreeRow{v.row}, v.row.Members...)
}

// lit is the block the keys act on.
func (v *wtView) lit() worktreeRow {
	b := v.blocks()
	return b[min(v.at, len(b)-1)]
}

// showWorktreeView opens the view of a worktree.
func (a *App) showWorktreeView(r worktreeRow) {
	v := &wtView{row: r, facts: map[string]wtFacts{}}
	v.body = newBlockList()
	v.body.waiting = "reading the worktree from git …"
	v.footer = tview.NewTextView().SetDynamicColors(true).SetTextColor(colDim)
	v.footer.SetWrap(true).SetWordWrap(true)
	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.body, 0, 1, true).
		AddItem(v.footer, 1, 0, false)
	title := r.Path
	if r.grouped() {
		title += fmt.Sprintf(" · %d repositories", len(r.Members))
	}
	v.frame = frame
	box(frame.Box, title)
	// A Flex leaves what is under it; the list would show between the border
	// and the blocks. The inside is cleared, a column kept free on each side,
	// and the footer made as tall as its keys need at the width it got.
	frame.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		for row := y + 1; row < y+h-1; row++ {
			for col := x + 1; col < x+w-1; col++ {
				screen.SetContent(col, row, ' ', nil, tcell.StyleDefault)
			}
		}
		width := max(1, w-4)
		frame.ResizeItem(v.footer, max(1, len(tview.WordWrap(v.footer.GetText(false), width))), 0)
		return x + 2, y + 1, width, max(0, h-2)
	})
	v.body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey { return a.worktreeViewKeys(v, ev) })
	a.wtView = v
	a.pages.AddPage(pageWorktree, modalPct(frame, 92, 92), true, true)
	a.tv.SetFocus(v.body)
	a.renderWorktreeView()
	a.readWorktreeFacts()
}

// closeWorktreeView leaves the view for the list.
func (a *App) closeWorktreeView() {
	a.wtView = nil
	a.closeModal(pageWorktree)
}

// reloadWorktreeView follows what the disk says now: the row is read again
// from the list, git is asked again, and the view drawn anew. A worktree gone
// from the disk closes it.
func (a *App) reloadWorktreeView() {
	v := a.wtView
	if v == nil {
		return
	}
	for _, r := range a.worktrees {
		if sameDir(r.Dir, v.row.Dir) {
			v.row = r
			v.at = min(v.at, len(v.blocks())-1)
			a.renderWorktreeView()
			a.readWorktreeFacts()
			return
		}
	}
	a.closeWorktreeView()
}

// readWorktreeFacts asks git about every repository of the view, each in a
// goroutine of its own, and draws once all of them have answered: where each
// branch stands against origin and what its work is. Asking for the first
// half here and leaving the rest to the list's own load is what made the view
// appear by halves.
func (a *App) readWorktreeFacts() {
	v := a.wtView
	members := []worktreeRow{v.row}
	if v.row.grouped() {
		members = v.row.Members
	}
	v.reading++
	ticket := v.reading
	integrate := a.cfg.Integrations.Incomm
	go func() {
		states := make([]remoteState, len(members))
		facts := make([]wtFacts, len(members))
		var wg sync.WaitGroup
		for i, m := range members {
			wg.Add(1)
			go func() {
				defer wg.Done()
				git := a.newManager(m.Instance, m.Path, nil).Git()
				states[i] = remoteStateOf(git, m, git.BranchUpstreams(m.Dir), git.BranchBases(m.Dir),
					git.RebasedFrom(m.Dir), integrate)
				facts[i] = a.gatherFacts(m, states[i])
			}()
		}
		wg.Wait()
		a.tv.QueueUpdateDraw(func() {
			if a.wtView != v || ticket != v.reading {
				return
			}
			if a.wtRemote == nil {
				a.wtRemote = map[string]remoteState{}
			}
			v.facts = map[string]wtFacts{}
			for i, m := range members {
				a.wtRemote[m.Dir] = states[i]
				v.facts[m.Dir] = facts[i]
			}
			v.loaded = true
			a.renderWorktreeView()
		})
	}()
}

// renderWorktreeView draws every block, the lit one in the focused border.
func (a *App) renderWorktreeView() {
	v := a.wtView
	v.body.lit = v.at
	v.footer.SetText(worktreeViewHint(v.lit()))
	if !v.loaded {
		v.body.blocks = nil
		return
	}
	blocks := make([]textBlock, 0, len(v.blocks()))
	for _, r := range v.blocks() {
		if r.grouped() {
			blocks = append(blocks, a.groupBlock(r))
		} else {
			blocks = append(blocks, a.memberBlock(r, v.facts[r.Dir]))
		}
	}
	v.body.blocks = blocks
}

// groupBlock is the group's own block: where it is and how it stands as a
// whole.
func (a *App) groupBlock(r worktreeRow) textBlock {
	plain, colour := a.worktreeRemoteWords(r)
	rows := []string{
		kvRow("Folder", tag(colMuted)+esc(tildePath(r.Dir))+tagEnd),
		kvRow("Branch", tag(colBranch)+esc(a.worktreeBranch(r))+tagEnd),
		kvRow("Origin", tag(colour)+esc(plain)+tagEnd),
	}
	if edits := a.worktreeEdits(r); edits != "" {
		rows = append(rows, kvRow("Work tree", tag(colWarn)+edits+" uncommitted"+tagEnd))
	} else {
		rows = append(rows, kvRow("Work tree", tag(colOn)+"clean"+tagEnd))
	}
	if mr := a.worktreeMR(r); mr != "" {
		rows = append(rows, kvRow("MR", tag(colAccent)+esc(mr)+tagEnd))
	}
	if com, colour := a.worktreeComments(r); com != "" {
		rows = append(rows, kvRow("Comments", tag(colour)+com+tagEnd))
	}
	return textBlock{title: esc(r.Path) + tag(colDim) + " · every repository" + tagEnd, rows: rows}
}

// memberBlock is one repository: its branch and where it stands, its merge
// request, its comments, and what git says of its work.
func (a *App) memberBlock(m worktreeRow, f wtFacts) textBlock {
	st, known := a.wtRemote[m.Dir]
	title := esc(m.Path)
	if m.Group != "" {
		title = esc(filepath.Base(m.Dir)) + tag(colDim) + "  " + esc(m.Path) + tagEnd
	}
	rows := []string{
		kvRow("Branch", tag(colBranch)+esc(m.Branch)+tagEnd),
		kvRow("Base", baseLine(st, f)),
		kvRow("Origin", a.remoteLine(st, known)),
		kvRow("Work tree", stateLine(f)),
	}
	if mr, ok := a.openMRFor(m); ok {
		rows = append(rows, kvRow("MR", tag(colAccent)+fmt.Sprintf("!%d", mr.IID)+tagEnd+" "+esc(mr.Title)))
		rows = append(rows, moreRow(tag(colDim)+esc(mr.WebURL)+tagEnd))
	} else {
		rows = append(rows, kvRow("MR", tag(colDim)+"none · n opens one"+tagEnd))
	}
	if c := a.commentsLine(st); c != "" {
		rows = append(rows, kvRow("Comments", c))
	}
	if len(f.own) > 0 {
		rows = append(rows, "")
		label := "Commits"
		for _, c := range f.own[:min(len(f.own), 3)] {
			rows = append(rows, kvRow(label, tag(colMuted)+esc(c)+tagEnd))
			label = ""
		}
		if f.ownCount > 3 {
			rows = append(rows, moreRow(tag(colDim)+fmt.Sprintf("… and %d more · l lists them", f.ownCount-3)+tagEnd))
		}
	}
	if len(f.dirty) > 0 {
		rows = append(rows, "")
		label := "Uncommitted"
		for _, d := range f.dirty[:min(len(f.dirty), 3)] {
			rows = append(rows, kvRow(label, tag(colWarn)+esc(d)+tagEnd))
			label = ""
		}
		if len(f.dirty) > 3 {
			rows = append(rows, moreRow(tag(colDim)+fmt.Sprintf("… and %d more", len(f.dirty)-3)+tagEnd))
		}
	}
	return textBlock{title: title, rows: rows}
}

// worktreeViewHint is the footer: the keys of the block that is lit.
func worktreeViewHint(r worktreeRow) string {
	if r.grouped() {
		return "j/k · Ctrl-O open · p pull · P push · C commit · n MRs · D diff · Ctrl-R rebase · " +
			"U unpublish · a add · r refresh · Esc back"
	}
	keys := "j/k · Ctrl-O open · w web · c comments · l log · p pull · P push · C commit · n MR · " +
		"D diff · Ctrl-R rebase · U unpublish"
	if r.Group != "" {
		keys += " · x take out"
	}
	return keys + " · r refresh · Esc back"
}

// worktreeViewKeys is what the keys do in the view: move between the blocks,
// or act on the one lit.
func (a *App) worktreeViewKeys(v *wtView, ev *tcell.EventKey) *tcell.EventKey {
	r := v.lit()
	switch ev.Key() {
	case tcell.KeyEsc:
		a.closeWorktreeView()
		return nil
	case tcell.KeyDown:
		v.at = min(v.at+1, len(v.blocks())-1)
		a.renderWorktreeView()
		return nil
	case tcell.KeyUp:
		v.at = max(v.at-1, 0)
		a.renderWorktreeView()
		return nil
	case tcell.KeyCtrlO:
		if r.grouped() {
			a.openGroup(r, nil)
		} else {
			a.openWorktree(r, nil)
		}
		return nil
	case tcell.KeyCtrlR:
		a.rebaseWorktree(r)
		return nil
	case tcell.KeyRune:
	default:
		return ev
	}
	if ev.Modifiers()&tcell.ModAlt != 0 {
		if ev.Rune() == 'd' {
			dir, targets := a.worktreeDiff(r)
			a.diffMenu("Show in Hunk - "+r.Path, dir, targets)
		}
		return nil
	}
	switch ev.Rune() {
	case 'j':
		v.at = min(v.at+1, len(v.blocks())-1)
		a.renderWorktreeView()
	case 'k':
		v.at = max(v.at-1, 0)
		a.renderWorktreeView()
	case 'g':
		v.at = 0
		a.renderWorktreeView()
	case 'G':
		v.at = len(v.blocks()) - 1
		a.renderWorktreeView()
	case 'q':
		a.closeWorktreeView()
	case '?':
		a.showHelp()
	case 'p':
		a.updateWorktree(r)
	case 'P':
		if r.grouped() {
			a.pushGroup(r)
		} else {
			a.pushWorktree(r)
		}
	case 'C':
		a.commitWorktree(r)
	case 'n':
		if r.grouped() {
			a.groupMergeRequests(r)
		} else {
			a.newMergeRequest(r)
		}
	case 'D':
		a.diffKey(a.worktreeDiff(r))
	case 'U':
		a.unpublishBranches(r)
	case 'r':
		a.refreshDisk()
		a.fetchWorktrees()
		a.note("looking at the disk, and asking origin")
	case 'a':
		if v.row.grouped() {
			a.addToGroup(v.row)
		}
	case 'x':
		if r.Group != "" {
			a.confirmTakeOut(v.row, r)
		}
	case 'w':
		a.openWorktreeWeb(r)
	case 'c':
		if mr, ok := a.openMRFor(r); ok && !r.grouped() {
			a.showComments(mr)
		} else if !r.grouped() {
			a.flash("no merge request is open from " + r.Branch + " - n opens one")
		}
	case 'l':
		if !r.grouped() {
			a.showWorktreeLog(r)
		}
	}
	return nil
}

// openWorktreeWeb shows a repository's merge request in the browser, or the
// repository itself when it has none.
func (a *App) openWorktreeWeb(r worktreeRow) {
	if r.grouped() {
		return
	}
	url := ""
	if mr, ok := a.openMRFor(r); ok {
		url = mr.WebURL
	} else if pr := a.worktreeProject(r); pr.WebURL != "" {
		url = pr.WebURL
	}
	if url == "" {
		a.flash("no address known for " + r.Path)
		return
	}
	_ = openBrowser(url)
	a.note("opened " + url)
}

// showWorktreeLog lists the commits of a repository's branch since its base,
// or the latest ones; Enter shows one in Hunk when it is on.
func (a *App) showWorktreeLog(r worktreeRow) {
	base := a.wtRemote[r.Dir].Base
	if base == "" {
		base = r.Base
	}
	mgr := a.pathManager(r.Instance, r.Path)
	go func() {
		from := mgr.ChangeBase(r.Dir, base)
		commits := mgr.Commits(r.Dir, from, 50)
		a.tv.QueueUpdateDraw(func() {
			if len(commits) == 0 {
				a.flash(r.Branch + " has no commits of its own yet")
				return
			}
			items := make([]pickItem, len(commits))
			for i, c := range commits {
				items[i] = pickItem{Label: c.SHA[:8] + "  " + c.Subject, Sub: c.When, Data: c}
			}
			title := "Commits of " + r.Branch
			if from != "HEAD" && base != "" {
				title += " since " + base
			}
			a.showPicker(title, items, func(it pickItem) {
				c := it.Data.(workspace.CommitLine)
				if bin, ok := a.hunkBinary(); ok {
					go a.runView(bin, diffView{dir: r.Dir, args: []string{"show", c.SHA}})
				}
			})
		})
	}()
}
