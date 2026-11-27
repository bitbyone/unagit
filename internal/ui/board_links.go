package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/board"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/workspace"
)

// A task's links (plans/board.md, phases 2 and 3) are to what unagit
// knows - a repository, a merge request, a worktree, a sandbox, a task in
// one - and are alive: what each is now is read from what unagit has
// already read for its lists, never asked of a server for the Board's
// sake, and written into the link when it changes, so a card draws at
// once and says what became of a thing once it is gone. The editors and
// agents open in a task's directories are found, not linked.

// --------------------------------------------------------------- the links

// linkOfProject is a link to a repository.
func linkOfProject(pr forge.Project) board.Link {
	return board.Link{Kind: board.KindRepository, Key: pr.Instance + "/" + pr.PathWithNamespace,
		Instance: pr.Instance, Project: pr.PathWithNamespace, Label: pr.PathWithNamespace, URL: pr.WebURL}
}

// linkOfMR is a link to a merge request.
func (a *App) linkOfMR(mr forge.MergeRequest) board.Link {
	path := a.projectPathOfMR(mr)
	l := board.Link{Kind: board.KindMR, Key: fmt.Sprintf("%s/%s!%d", mr.Instance, path, mr.IID),
		Instance: mr.Instance, Project: path, IID: mr.IID,
		Label: fmt.Sprintf("%s !%d", path, mr.IID), URL: mr.WebURL}
	l.Seen = mrState(mr)
	return l
}

// linkOfWorktree is a link to a worktree on disk, a grouped one too.
func (a *App) linkOfWorktree(r worktreeRow) board.Link {
	label := r.Path
	switch {
	case r.keptReview():
		label = fmt.Sprintf("%s !%d review", r.Path, r.Review)
	case r.Branch != "":
		label += " · " + r.Branch
	}
	return board.Link{Kind: board.KindWorktree, Key: r.Dir, Path: r.Dir,
		Instance: r.Instance, Project: r.Path, IID: r.Review, Label: label, URL: a.wtRemote[r.Dir].URL}
}

// linkOfSandbox is a link to one of unibox's sandboxes.
func linkOfSandbox(s unibox.Sandbox) board.Link {
	return board.Link{Kind: board.KindSandbox, Key: s.Name, Sandbox: s.Name, Label: s.Name}
}

// linkOfSandboxTask is a link to a task in a sandbox.
func linkOfSandboxTask(t unibox.Task) board.Link {
	return board.Link{Kind: board.KindSandboxTask, Key: t.Sandbox + "/" + t.ID, Sandbox: t.Sandbox, Task: t.ID,
		Label: t.Name() + " · " + t.Sandbox}
}

// mrState is what a merge request listed is, in the link's words.
func mrState(mr forge.MergeRequest) board.LinkState {
	st := board.LinkState{State: mr.State}
	switch mr.State {
	case "opened", "open", "":
		st.State = "open"
		if mr.Draft {
			st.State = "draft"
		}
	case "merged", "closed":
		st.Gone = true
	}
	st.MR = &board.MRSnapshot{Title: mr.Title, Author: mr.Author.Username, Target: mr.TargetBranch,
		Pipeline: mr.Pipeline, Approved: len(mr.ApprovedBy), Required: mr.ApprovalsRequired, Comments: mr.Comments}
	return st
}

// listedMR is the merge request a link names, as the list has it.
func (a *App) listedMR(l board.Link) (forge.MergeRequest, bool) {
	for _, mr := range a.mrs {
		if mr.Instance == l.Instance && mr.IID == l.IID && a.projectPathOfMR(mr) == l.Project {
			return mr, true
		}
	}
	return forge.MergeRequest{}, false
}

// listedWorktree is the worktree a link names, as Worktrees lists it.
func (a *App) listedWorktree(dir string) (worktreeRow, bool) {
	for _, r := range a.worktrees {
		if sameDir(r.Dir, dir) {
			return r, true
		}
	}
	return worktreeRow{}, false
}

// sandboxTaskRow is the row of a sandbox's task, for its actions.
func (a *App) sandboxTaskRow(l board.Link) (sandboxRow, bool) {
	for _, r := range a.sandboxRows {
		if r.kind == rowTask && r.task.ID == l.Task && r.sandbox.Name == l.Sandbox {
			return r, true
		}
	}
	return sandboxRow{}, false
}

// linkNow is what a link is now, from what unagit has read, and whether
// that can be told: a server whose list was never read, unibox not read,
// say nothing, and the link keeps what it last was.
func (v *boardView) linkNow(l board.Link) (board.LinkState, bool) {
	a := v.app
	switch l.Kind {
	case board.KindRepository:
		if _, ok := a.projByKey[projectKey{l.Instance, l.Project}]; ok {
			if a.diskOf(l.Instance, l.Project).Cloned {
				return board.LinkState{State: "cloned"}, true
			}
			return board.LinkState{State: "not cloned"}, true
		}
		for _, p := range a.projects {
			if p.Instance == l.Instance {
				return board.LinkState{State: "not listed", Gone: true}, true
			}
		}
	case board.KindMR:
		if mr, ok := a.listedMR(l); ok {
			return mrState(mr), true
		}
		// Not listed: merged or closed, most likely. It is asked once of
		// its server, and kept as it was told.
		if st, ok := v.asked[l.Key]; ok {
			return st, st.State != ""
		}
		for _, mr := range a.mrs {
			if mr.Instance == l.Instance {
				v.askMR(l)
				break
			}
		}
	case board.KindWorktree:
		if workspace.Exists(l.Path) {
			return board.LinkState{State: "on disk"}, true
		}
		return board.LinkState{State: "removed", Gone: true}, true
	case board.KindSandbox, board.KindSandboxTask:
		if !a.ubx.read || a.ubx.err != nil {
			return board.LinkState{}, false
		}
		for _, s := range a.ubx.overview.Sandboxes {
			if s.Name != l.Sandbox {
				continue
			}
			if l.Kind == board.KindSandbox {
				return board.LinkState{State: s.State}, true
			}
			for _, t := range s.Tasks {
				if t.ID == l.Task {
					if _, ok := s.RunOf(t.ID); ok {
						return board.LinkState{State: "working"}, true
					}
					return board.LinkState{State: "idle"}, true
				}
			}
		}
		return board.LinkState{State: "removed", Gone: true}, true
	}
	return board.LinkState{}, false
}

// askMR asks a merge request's server, once, what became of a merge
// request the list no longer has.
func (v *boardView) askMR(l board.Link) {
	a := v.app
	v.asked[l.Key] = board.LinkState{}
	client := a.client(l.Instance)
	if client == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := client.MergeRequestDetail(ctx, forge.MergeRequest{Instance: l.Instance, ProjectPath: l.Project, IID: l.IID})
		a.tv.QueueUpdateDraw(func() {
			if err != nil || d == nil {
				return
			}
			mr := d.MergeRequest
			mr.Instance, mr.ProjectPath = l.Instance, l.Project
			if mr.State == "" {
				mr.State = "closed"
			}
			st := mrState(mr)
			st.MR.Conflicts = d.HasConflicts
			v.asked[l.Key] = st
			v.alive()
		})
	}()
}

// alive puts into each link of the space's tasks what it is now, and
// into the task's story what became of it, writing only a task whose
// links changed.
func (v *boardView) alive() {
	a := v.app
	if v.sandboxLinks() && !a.ubx.loading && (!a.ubx.read || time.Since(a.ubx.at) > time.Minute) && a.solo == nil {
		a.readSandboxes(false)
	}
	now := time.Now()
	wrote := false
	for i, t := range v.tasks {
		fresh := map[string]board.LinkState{}
		for _, l := range t.Links {
			st, ok := v.linkNow(l)
			if !ok || st.Same(l.Seen) {
				continue
			}
			st.Since = l.Seen.Since
			if st.State != l.Seen.State || st.Gone != l.Seen.Gone || st.Since.IsZero() {
				st.Since = now
			}
			fresh[l.Kind+"\x00"+l.Key] = st
		}
		if len(fresh) == 0 {
			continue
		}
		changed, err := v.store.Change(t.Space, t.ID, func(m *board.Task) error {
			for j := range m.Links {
				l := &m.Links[j]
				st, ok := fresh[l.Kind+"\x00"+l.Key]
				if !ok {
					continue
				}
				// The first state a link is known in is no news.
				if l.Seen.State != "" && (st.State != l.Seen.State || st.Gone != l.Seen.Gone) {
					m.Tell(now, l.Name()+" "+st.State)
				}
				l.Seen = st
			}
			return nil
		})
		if err != nil {
			continue
		}
		v.tasks[i], wrote = changed, true
	}
	if wrote {
		v.stamp = v.store.Stamp(v.space.ID)
		v.fill()
	}
}

// sandboxLinks says whether a task of the space links to unibox.
func (v *boardView) sandboxLinks() bool {
	for _, t := range v.tasks {
		for _, l := range t.Links {
			if l.Kind == board.KindSandbox || l.Kind == board.KindSandboxTask {
				return true
			}
		}
	}
	return false
}

// --------------------------------------------------------------- drawing

// linkMark is a link drawn short, in its state's colour: the compact
// card's row of icons.
func linkMark(l board.Link) string {
	colour := role("board.link")
	mark := glyphLinkURL
	switch l.Kind {
	case board.KindRepository:
		mark = glyphLinkRepository
	case board.KindMR:
		mark = fmt.Sprintf("!%d", l.IID)
		colour = mrColour(l.Seen.State)
	case board.KindWorktree:
		mark = glyphLinkWorktree
	case board.KindSandbox:
		mark = glyphLinkSandbox
		if l.Seen.State == "running" {
			colour = role("board.link.live")
		}
	case board.KindSandboxTask:
		mark = glyphLinkTask
		if l.Seen.State == "working" {
			colour = role("board.link.working")
		}
	}
	if l.Seen.Gone && l.Seen.State != "merged" {
		colour = role("board.link.gone")
	}
	out := tag(colour) + esc(mark) + tagEnd
	if mr := l.Seen.MR; mr != nil && !l.Seen.Gone {
		if g, c := ciMark(mr.Pipeline); g != "" {
			out += tag(c) + g + tagEnd
		}
	}
	return out
}

// mrColour is a merge request's state's colour.
func mrColour(state string) tcell.Color {
	switch state {
	case "draft", "merged", "closed":
		return role("board.link." + map[string]string{"draft": "draft", "merged": "live", "closed": "closed"}[state])
	}
	return role("board.link")
}

// linkWords is what a link is, in words: its state, and since when once it
// is gone.
func linkWords(l board.Link) string {
	st := l.Seen
	var parts []string
	if mr := st.MR; mr != nil {
		parts = append(parts, st.State)
		if mr.Pipeline != "" {
			parts = append(parts, "pipeline "+mr.Pipeline)
		}
		if mr.Required > 0 || mr.Approved > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d approved", mr.Approved, max(mr.Required, mr.Approved)))
		}
		if mr.Conflicts {
			parts = append(parts, "conflicts")
		}
	} else if st.State != "" {
		parts = append(parts, st.State)
	}
	if st.Gone && !st.Since.IsZero() {
		parts = append(parts, humanAge(st.Since))
	}
	return strings.Join(parts, " · ")
}

// linkLine is a link on a row of its own: its mark, its name and what it is.
func linkLine(l board.Link) string {
	ink := colText
	if l.Seen.Gone {
		ink = role("board.link.gone")
	}
	name := l.Name()
	if l.Kind == board.KindMR && l.Project != "" {
		// Its mark is its number already.
		name = l.Project
	}
	line := linkMark(l) + " " + tag(ink) + esc(name) + tagEnd
	if words := linkWords(l); words != "" {
		line += tag(colMuted) + " · " + esc(words) + tagEnd
	}
	if l.Kind == board.KindMR && l.Seen.MR != nil && l.Seen.MR.Title != "" {
		line += tag(colMuted) + "  " + esc(l.Seen.MR.Title) + tagEnd
	}
	return line
}

// mergedNotDone says a task links a merge request merged while the task
// is not done: the card asks, it never moves by itself.
func mergedNotDone(t board.Task) bool {
	if t.State == board.Done {
		return false
	}
	for _, l := range t.Links {
		if l.Kind == board.KindMR && l.Seen.State == "merged" {
			return true
		}
	}
	return false
}

// ----------------------------------------------------- what is found open

// found is an editor or an agent open in one of a task's directories.
type found struct {
	label, sub string
	agent      bool
	run        func()
}

// taskDirs are the directories a task's links have on disk: its
// worktrees, its repositories' clones, its merge requests' worktrees.
func (v *boardView) taskDirs(t board.Task) []string {
	a := v.app
	var dirs []string
	for _, l := range t.Links {
		switch l.Kind {
		case board.KindWorktree:
			if !l.Seen.Gone {
				dirs = append(dirs, l.Path)
			}
		case board.KindRepository:
			if a.diskOf(l.Instance, l.Project).Cloned {
				dirs = append(dirs, a.projectDir(l.Instance, l.Project))
			}
		case board.KindMR:
			d := a.diskOf(l.Instance, l.Project).MRs[l.IID]
			for _, dir := range []string{d.BranchDir, d.ReviewDir} {
				if dir != "" {
					dirs = append(dirs, dir)
				}
			}
		}
	}
	return dirs
}

// within says whether dir is one of dirs or inside one.
func within(dir string, dirs []string) bool {
	for _, d := range dirs {
		if sameDir(dir, d) || strings.HasPrefix(filepath.Clean(dir), filepath.Clean(d)+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// foundOpen are the editors and agents open in a task's directories, and
// the agents at work on its sandbox tasks.
func (v *boardView) foundOpen(t board.Task) []found {
	a := v.app
	dirs := v.taskDirs(t)
	var out []found
	seen := map[string]bool{}
	if len(dirs) > 0 {
		for _, r := range a.editorsOpen {
			if !within(r.Dir, dirs) {
				continue
			}
			seen[r.Dir+"\x00"+r.Pane] = true
			out = append(out, found{label: editorShortName(r.Editor), sub: tildePath(r.Dir), run: func() { a.goToEditor(r) }})
		}
		for _, r := range a.agentRows {
			if !within(r.Dir, dirs) || seen[r.Dir+"\x00"+r.Pane] {
				continue
			}
			sub := tildePath(r.Dir)
			if r.Status != "" {
				sub = r.Status + " · " + sub
			}
			out = append(out, found{label: agentName(r.Kind), sub: sub, agent: true, run: func() { a.goToAgent(r) }})
		}
	}
	for _, l := range t.Links {
		if l.Kind != board.KindSandboxTask {
			continue
		}
		if r, ok := a.sandboxTaskRow(l); ok && r.working() {
			out = append(out, found{label: agentLabel(r.task.Agent), sub: "at work on " + r.task.Name(), agent: true, run: func() { a.goToTask(r) }})
		}
	}
	return out
}

// foundMarks are the found drawn short, for the compact card.
func foundMarks(fs []found) string {
	var out []string
	for _, f := range fs {
		g := glyphEditor
		if f.agent {
			g = glyphAgent
		}
		out = append(out, tag(role("board.found"))+g+tagEnd)
	}
	return strings.Join(out, " ")
}

// foundLines are the found on rows of their own.
func foundLines(fs []found) []string {
	var out []string
	for _, f := range fs {
		g := glyphEditor
		if f.agent {
			g = glyphAgent
		}
		out = append(out, tag(role("board.found"))+g+tagEnd+" "+tag(colText)+esc(f.label)+tagEnd+tag(colMuted)+" · "+esc(f.sub)+tagEnd)
	}
	return out
}

// watchNews is the latest news of a watch on a merge request a task
// links, as Activity has it.
func (v *boardView) watchNews(t board.Task) []string {
	a := v.app
	var out []string
	for _, l := range t.Links {
		if l.Kind != board.KindMR {
			continue
		}
		key := pipelineWatch(l.Instance, l.Project, 0, l.IID, "").Key()
		for i := len(a.watchSnap.Events) - 1; i >= 0; i-- {
			e := a.watchSnap.Events[i]
			if e.Key != key {
				continue
			}
			heading := e.Heading
			if heading == "" {
				heading = e.Line
			}
			line := tag(levelSeverity(e.Level).colour()) + glyphWatched + tagEnd + " " + tag(colText) + esc(fmt.Sprintf("!%d %s", l.IID, heading)) + tagEnd
			if !e.At.IsZero() {
				line += tag(colMuted) + " · " + humanAge(e.At) + tagEnd
			}
			out = append(out, line)
			break
		}
	}
	return out
}

// --------------------------------------------------------------- linking

// linkSomething links to a task what the user picks: a repository, a merge
// request, a worktree, a sandbox, a sandbox's task, or an address.
func (v *boardView) linkSomething(t board.Task, then func()) {
	a := v.app
	kinds := []pickItem{
		{Label: "Repository…", Sub: counted(len(a.projects), "repository", "repositories"), Data: board.KindRepository},
		{Label: "Merge Request…", Sub: counted(len(a.mrs), "open", "open"), Data: board.KindMR},
		{Label: "Worktree…", Sub: counted(len(a.worktrees), "on disk", "on disk"), Data: board.KindWorktree},
		{Label: "Sandbox…", Sub: "unibox's", Data: board.KindSandbox},
		{Label: "Sandbox Task…", Sub: "an agent's task in a sandbox", Data: board.KindSandboxTask},
		{Label: "URL…", Sub: "a ticket, a document, a page", Data: board.KindURL},
	}
	a.showPickerWith("Link · "+t.Title, kinds, pickerOptions{pack: true, enterHint: "choose"}, func(it pickItem) {
		kind := it.Data.(string)
		if kind == board.KindURL {
			v.linkURL(t, then)
			return
		}
		items := v.linkables(kind, t)
		if len(items) == 0 {
			a.flash(v.noneToLink(kind))
			return
		}
		a.showPickerWith(strings.TrimSuffix(it.Label, "…")+" · "+t.Title, items, pickerOptions{enterHint: "link"}, func(it pickItem) {
			v.addLink(t, it.Data.(board.Link))
			if then != nil {
				then()
			}
		})
	})
}

// noneToLink says why there is nothing of a kind to link.
func (v *boardView) noneToLink(kind string) string {
	a := v.app
	switch kind {
	case board.KindSandbox, board.KindSandboxTask:
		if !a.ubx.read {
			a.readSandboxes(true)
			return "the sandboxes are being read - ask again in a moment"
		}
		if a.ubx.err != nil {
			return "unibox: " + a.ubx.err.Error()
		}
		return "unibox has no " + map[string]string{board.KindSandbox: "sandbox", board.KindSandboxTask: "task"}[kind]
	case board.KindWorktree:
		return "no worktree on disk"
	}
	return "nothing listed yet - refresh the list first"
}

// linkables are the things of a kind there are to link, those the task
// has already left out.
func (v *boardView) linkables(kind string, t board.Task) []pickItem {
	a := v.app
	var items []pickItem
	add := func(l board.Link, sub string) {
		if !t.Has(l.Kind, l.Key) {
			items = append(items, pickItem{Label: esc(l.Label), Sub: esc(sub), Data: l})
		}
	}
	switch kind {
	case board.KindRepository:
		for _, pr := range a.projects {
			add(linkOfProject(pr), a.instanceLabel(pr.Instance))
		}
	case board.KindMR:
		for _, mr := range a.mrs {
			add(a.linkOfMR(mr), mr.Title)
		}
	case board.KindWorktree:
		for _, r := range a.worktrees {
			add(a.linkOfWorktree(r), tildePath(r.Dir))
		}
	case board.KindSandbox:
		for _, s := range a.ubx.overview.Sandboxes {
			add(linkOfSandbox(s), s.State)
		}
	case board.KindSandboxTask:
		for _, s := range a.ubx.overview.Sandboxes {
			for _, task := range s.Tasks {
				add(linkOfSandboxTask(task), agentLabel(task.Agent))
			}
		}
	}
	return items
}

// addLink links a thing to a task, once: what it is now goes in with it,
// so it is not news later.
func (v *boardView) addLink(t board.Task, l board.Link) {
	if t.Has(l.Kind, l.Key) {
		v.app.flash(l.Name() + " is linked already")
		return
	}
	now := time.Now()
	l.Added = now
	if st, ok := v.linkNow(l); ok {
		l.Seen = st
	}
	l.Seen.Since = now
	v.change(t, func(m *board.Task) {
		m.Links = append(m.Links, l)
		m.Tell(now, "linked "+l.Name())
	})
	v.app.done("Linked " + l.Name())
}

// showLinks lists a task's links: Enter goes to one in its list, w opens
// its page, d takes it off the task.
func (v *boardView) showLinks(t board.Task, then func()) {
	a := v.app
	if len(t.Links) == 0 {
		a.flash("the task links nothing yet - i links something")
		return
	}
	items := make([]pickItem, len(t.Links))
	for i, l := range t.Links {
		items[i] = pickItem{Label: linkMark(l) + " " + esc(l.Name()), Sub: esc(linkWords(l)), Data: l}
	}
	keys := []pickKey{
		a.browserKey("w", "browser", "Open in Browser", "The link's page on its server, or the address.", func(it pickItem) string {
			return it.Data.(board.Link).URL
		}),
		{keys: "d", hint: "remove", name: "Remove Link…", about: "Take the link off the task, after asking; what it links to stays as it is.",
			run: func(it pickItem) { v.removeLink(t, it.Data.(board.Link), then) }},
	}
	a.showPickerWith("Links · "+t.Title, items, pickerOptions{pack: true, enterHint: "go to", keys: keys}, func(it pickItem) {
		v.goToLink(it.Data.(board.Link))
	})
}

// goToLink shows what a link names where unagit lists it.
func (v *boardView) goToLink(l board.Link) {
	a := v.app
	switch l.Kind {
	case board.KindRepository:
		a.goToProject(projectKey{l.Instance, l.Project})
		return
	case board.KindMR:
		if mr, ok := a.listedMR(l); ok {
			a.showMRAt(mr)
			return
		}
	case board.KindWorktree:
		if _, ok := a.listedWorktree(l.Path); ok {
			a.switchTab(pageWorktrees)
			a.worktreesPane.clearFilter()
			a.worktreesPane.reload()
			a.worktreesPane.selectWhere(func(i int) bool { return i < len(a.worktrees) && sameDir(a.worktrees[i].Dir, l.Path) })
			return
		}
	case board.KindSandbox, board.KindSandboxTask:
		if !l.Seen.Gone {
			a.switchTab(pageSandboxes)
			return
		}
	}
	if l.URL != "" {
		a.openWeb(l.URL)
		return
	}
	a.flash(l.Name() + " is " + l.Seen.State + " - nothing lists it")
}

// removeLink takes a link off a task, after asking.
func (v *boardView) removeLink(t board.Task, l board.Link, then func()) {
	v.app.confirmWith("Remove Link", fmt.Sprintf("Take [::b]%s[::-] off the task?\n\nWhat it links to stays as it is.", esc(l.Name())), "Remove", nil, func() {
		now := time.Now()
		v.change(t, func(m *board.Task) {
			m.Links = slices.DeleteFunc(m.Links, func(o board.Link) bool { return o.Kind == l.Kind && o.Key == l.Key })
			m.Tell(now, "unlinked "+l.Name())
		})
		if then != nil {
			then()
		}
		v.app.done("Unlinked " + l.Name())
	})
}

// addToTask links what is chosen in a list to a task of the Board: the
// open space's tasks first, then the other spaces', and New Task… before
// them all.
func (a *App) addToTask(l board.Link) {
	v := a.board
	if v == nil {
		return
	}
	v.reload()
	items := []pickItem{{Label: "New Task…", Sub: "in " + esc(v.space.Name) + ", named after it", Data: nil}}
	add := func(sp board.Space, tasks []board.Task) {
		for _, t := range tasks {
			if t.State == board.Done {
				continue
			}
			sub := board.StateName(t.State)
			if sp.ID != v.space.ID {
				sub = sp.Name + " · " + sub
			}
			if t.Has(l.Kind, l.Key) {
				sub += " · linked already"
			}
			items = append(items, pickItem{Label: esc(t.Title), Sub: esc(sub), Data: t})
		}
	}
	add(v.space, v.tasks)
	for _, sp := range v.spaces {
		if sp.ID == v.space.ID {
			continue
		}
		if tasks, err := v.store.Tasks(sp.ID); err == nil {
			add(sp, tasks)
		}
	}
	a.showPickerWith("Add to Task · "+l.Name(), items, pickerOptions{pack: true, enterHint: "link"}, func(it pickItem) {
		t, ok := it.Data.(board.Task)
		if !ok {
			title := l.Name()
			if l.Seen.MR != nil && l.Seen.MR.Title != "" {
				title = l.Seen.MR.Title
			}
			v.newTask(title, func(t board.Task) { v.addLink(t, l) })
			return
		}
		v.addLink(t, l)
	})
}

// addToTaskAction is "Add to Task…" for what a list has chosen.
func (a *App) addToTaskAction(link func() board.Link) uiAction {
	return uiAction{name: "Add to Task…", about: "Link it to a task of the Board - one of the open space's, another space's, or a new one.",
		keys: "", rank: 85, when: func() bool { return a.board != nil }, run: func() { a.addToTask(link()) }}
}

// --------------------------------------------------------------- carry on

// carryOn takes the user back into a task's work: to an editor or agent
// open in it, else into its worktree or clone, else the review of its merge
// request, else its address. With more than one at the first level there
// is, it asks which.
func (v *boardView) carryOn(t board.Task) {
	a := v.app
	var level []pickItem
	choose := func(label, sub string, run func()) {
		level = append(level, pickItem{Label: esc(label), Sub: esc(sub), Data: run})
	}
	for _, f := range v.foundOpen(t) {
		choose(f.label, f.sub, f.run)
	}
	if len(level) == 0 {
		v.openings(t, choose)
	}
	if len(level) == 0 {
		for _, l := range t.Links {
			if l.Kind != board.KindMR || l.Seen.Gone {
				continue
			}
			if mr, ok := a.listedMR(l); ok {
				choose("Review "+l.Name(), "a review worktree, the change unstaged", func() { a.openMRReview(mr, nil) })
			}
		}
	}
	if len(level) == 0 {
		for _, l := range t.Links {
			if l.URL != "" && (l.Kind == board.KindURL || l.Seen.Gone) {
				choose(l.Name(), l.URL, func() { a.openWeb(l.URL) })
			}
		}
	}
	if len(level) == 0 {
		a.flash("nothing to carry on in - i links a worktree, a merge request or a page")
		return
	}
	run := func(it pickItem) {
		it.Data.(func())()
		if t.State == board.Todo && !a.cfg.Board.StayOnCarryOn {
			v.change(t, func(m *board.Task) {
				m.SetState(board.Doing, time.Now())
				m.Rank = v.topRank(board.Doing)
			})
		}
	}
	if len(level) == 1 {
		run(level[0])
		return
	}
	a.showPickerWith("Carry On · "+t.Title, level, pickerOptions{pack: true, enterHint: "go"}, run)
}

// openings are the ways into a task's work on disk: its worktrees, its
// merge requests' worktrees, its repositories' clones, its sandbox tasks
// to resume.
func (v *boardView) openings(t board.Task, choose func(label, sub string, run func())) {
	a := v.app
	open := func(then func(ed *editors.Editor)) func() {
		return func() { a.withEditor(false, then) }
	}
	for _, l := range t.Links {
		if l.Seen.Gone {
			continue
		}
		switch l.Kind {
		case board.KindWorktree:
			if r, ok := a.listedWorktree(l.Path); ok {
				choose("Open "+l.Name(), tildePath(r.Dir), open(func(ed *editors.Editor) {
					if r.grouped() {
						a.openGroup(r, ed)
					} else {
						a.openWorktree(r, ed)
					}
				}))
			}
		case board.KindMR:
			mr, ok := a.listedMR(l)
			if !ok {
				continue
			}
			d := a.diskOf(l.Instance, l.Project).MRs[l.IID]
			if d.Branch {
				choose("Open "+l.Name(), tildePath(d.BranchDir), open(func(ed *editors.Editor) { a.openMR(mr, ed) }))
			}
			if d.Review {
				choose("Review "+l.Name(), tildePath(d.ReviewDir), func() { a.openMRReview(mr, nil) })
			}
		case board.KindRepository:
			if pr, ok := a.projByKey[projectKey{l.Instance, l.Project}]; ok && a.diskOf(l.Instance, l.Project).Cloned {
				choose("Open "+l.Name(), tildePath(a.projectDir(l.Instance, l.Project)), open(func(ed *editors.Editor) { a.openProject(pr, ed) }))
			}
		case board.KindSandboxTask:
			if r, ok := a.sandboxTaskRow(l); ok {
				choose("Resume "+r.task.Name(), "in "+r.sandbox.Name, func() { a.resumeTask(r, r.task.Agent, false) })
			}
		}
	}
}

// --------------------------------------------------------------- tidying up

// tidy is something a task done leaves that can go.
type tidy struct {
	label string
	// why is what keeps it, when it is not safe to take: it is then only
	// said, not offered.
	why string
	run func(log func(string)) error
	// stop is a sandbox to stop, which unibox does, not a task's log.
	stop string
}

// offerTidyUp asks, once a task is done, what of what it linked can go
// now: its worktrees, its merge requests' worktrees, its sandboxes. What
// is safe is ticked; what holds work is named and left alone.
func (v *boardView) offerTidyUp(t board.Task) {
	a := v.app
	if a.cfg.Board.NoTidyUp {
		return
	}
	type candidate struct {
		label, instance, project, dir string
	}
	var dirs []candidate
	var sandboxes []string
	for _, l := range t.Links {
		if l.Seen.Gone {
			continue
		}
		switch l.Kind {
		case board.KindWorktree:
			if r, ok := a.listedWorktree(l.Path); ok && !r.grouped() {
				dirs = append(dirs, candidate{"Worktree " + trunc(l.Name(), 48), r.Instance, r.Path, r.Dir})
			}
		case board.KindMR:
			d := a.diskOf(l.Instance, l.Project).MRs[l.IID]
			if d.Review {
				dirs = append(dirs, candidate{fmt.Sprintf("Review of !%d", l.IID), l.Instance, l.Project, d.ReviewDir})
			}
			if d.Branch {
				dirs = append(dirs, candidate{fmt.Sprintf("Worktree of !%d", l.IID), l.Instance, l.Project, d.BranchDir})
			}
		case board.KindSandbox:
			if l.Seen.State == "running" {
				sandboxes = append(sandboxes, l.Sandbox)
			}
		}
	}
	if len(dirs) == 0 && len(sandboxes) == 0 {
		return
	}
	a.loadThen("Looking at what "+t.Title+" leaves", func(step func(string)) (string, error) {
		var out []tidy
		for _, c := range dirs {
			step(tildePath(c.dir))
			mgr := a.newManager(c.instance, c.project, nil)
			why := strings.Join(mgr.InspectDir(c.dir).Warnings, "; ")
			out = append(out, tidy{label: c.label, why: why, run: func(log func(string)) error {
				return a.newManager(c.instance, c.project, log).RemoveWorktreeDir(c.project, c.dir)
			}})
		}
		for _, s := range sandboxes {
			out = append(out, tidy{label: "Stop sandbox " + s, stop: s})
		}
		v.tidyings = out
		return "", nil
	}, func(string) { v.showTidyUp(t, v.tidyings) })
}

// showTidyUp is the question offerTidyUp asks.
func (v *boardView) showTidyUp(t board.Task, items []tidy) {
	a := v.app
	form := tview.NewForm()
	styleForm(form)
	var boxes []*tview.Checkbox
	var offered []tidy
	// Each item takes its rows and the one under it. What holds work is
	// named, with why, and has no box to tick.
	rows := 0
	for _, it := range items {
		if it.why == "" {
			boxes = append(boxes, addCheckbox(form, it.label, true))
			offered = append(offered, it)
			rows += 2
		}
	}
	for _, it := range items {
		if it.why != "" {
			form.AddTextView(it.label, tag(colWarn)+"stays: "+esc(it.why)+tagEnd, 0, 2, true, false)
			rows += 3
		}
	}
	form.AddButton("Tidy Up", func() {
		a.closeModal(pageForm)
		var work []tidy
		for i, b := range boxes {
			if !b.IsChecked() {
				continue
			}
			if s := offered[i].stop; s != "" {
				a.uniboxDo("Stopping "+s, "sandbox", "stop", s)
				continue
			}
			work = append(work, offered[i])
		}
		if len(work) == 0 {
			return
		}
		a.runTaskThen("Tidying up after "+t.Title, func(log func(string)) (string, error) {
			for _, w := range work {
				log(w.label)
				if err := w.run(log); err != nil {
					return "", err
				}
			}
			return "", nil
		}, func(string) {
			a.refreshDisk()
			v.reload()
		})
	})
	form.AddButton("Keep All", func() { a.closeModal(pageForm) })
	if len(boxes) == 0 {
		form.GetButton(0).SetDisabled(true)
	}
	a.showFormModalSized("Done · "+t.Title, form, 86, rows+5)
}
