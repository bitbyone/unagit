package ui

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/watch"
	"github.com/tobola/unagit/internal/workspace"
)

// agentRow is one coding agent unagit started: in herdr, which says what it
// is doing, or elsewhere - Zellij, Ghostty, a terminal - where only where it
// runs is known.
type agentRow struct {
	// Kind is the agent's id (agents.All), or herdr's name for one unagit
	// does not know.
	Kind string
	// Status is herdr's: working, blocked, idle, done, unknown; "" where
	// nothing can tell.
	Status string
	Title  string
	Dir    string
	Branch string
	// Where says where it runs, as a person would: "herdr · Unagit Agents ›
	// gateway !7", "Zellij", "Ghostty".
	Where string
	// Pane is herdr's pane, or the record's pane elsewhere.
	Pane      string
	Workspace string
	// Record is what unagit wrote down when it started the agent.
	Record *session.Record
}

// herdrAgent says whether herdr is where the row runs.
func (r agentRow) herdrAgent() bool { return r.Record != nil && r.Record.Mux == mux.Herdr }

// agentName is how an agent kind reads.
func agentName(kind string) string {
	if ag, ok := agents.ByID(kind); ok {
		return ag.Name
	}
	return kind
}

// agentStatusOrder puts the agents waiting for an answer first, then those
// at work: what wants the eye before what does not.
func agentStatusOrder(status string) int {
	switch status {
	case "blocked":
		return 0
	case "working":
		return 1
	case "done":
		return 2
	case "idle":
		return 3
	case "ended":
		return 5
	}
	return 4
}

const (
	agentsLookVisible = 2 * time.Second
	agentsLookHidden  = 10 * time.Second
)

// watchAgents reads the agents often while their tab is in front, and now
// and then otherwise, for the count the tab shows.
func (a *App) watchAgents(stop <-chan struct{}) {
	for {
		every := agentsLookHidden
		if a.agentsInFront.Load() {
			every = agentsLookVisible
		}
		select {
		case <-stop:
			return
		case <-a.agentsNow:
		case <-time.After(every):
		}
		a.tv.QueueUpdate(a.refreshAgents)
	}
}

// refreshAgents reads the agents off the event loop and draws them if they
// changed. It runs on the loop.
func (a *App) refreshAgents() {
	if a.agentsReading {
		return
	}
	herdr := a.herdr()
	a.agentsReading = true
	go func() {
		rows, err := readAgents(herdr, a.sessions)
		a.tv.QueueUpdateDraw(func() {
			a.agentsReading = false
			a.agentsError = ""
			if err != nil {
				a.agentsError = err.Error()
			}
			if reflect.DeepEqual(rows, a.agentRows) {
				a.activityPane.updateHeader()
				return
			}
			if changes := a.agentChanges(a.agentRows, rows); len(changes) > 0 && a.watchStore != nil {
				go a.watchStore.AppendHistory(true, changes...)
			}
			a.agentRows = rows
			a.redrawActivity()
			a.drawTabs()
			// The lists mark the directories the agents work in, in the
			// colour of what each is doing.
			a.projectsPane.reload()
			a.mrsPane.reload()
			a.worktreesPane.reload()
		})
	}()
}

// agentKey names an agent in the histories: where it was started, and
// when.
func agentKey(r agentRow) string {
	if r.Record == nil {
		return "agent:" + r.Dir
	}
	return fmt.Sprintf("agent:%s@%s@%d", r.Record.Dir, r.Record.Pane, r.Record.Since.Unix())
}

// agentChanges are the events of what the agents did between two
// readings: a new state, an agent gone. They go to the agents' histories
// only - an agent's wait is drawn on the tab, never a toast.
func (a *App) agentChanges(before, after []agentRow) []watch.Event {
	was := map[string]agentRow{}
	for _, r := range before {
		was[agentKey(r)] = r
	}
	var out []watch.Event
	for _, r := range after {
		key := agentKey(r)
		old, seen := was[key]
		delete(was, key)
		if seen && old.Status == r.Status {
			continue
		}
		out = append(out, a.agentEvent(r, r.Status))
	}
	for _, r := range was {
		out = append(out, a.agentEvent(r, "closed"))
	}
	return out
}

// agentEvent says an agent is in a state, as its history keeps it.
func (a *App) agentEvent(r agentRow, status string) watch.Event {
	what := a.agentWhat(r)
	if what == "" {
		what = tildePath(r.Dir)
	}
	name := agentName(r.Kind)
	heading, how, level := "Agent started", "was started in "+what, watch.Info
	switch status {
	case "working":
		heading, how = "Agent working", "is at work in "+what
	case "blocked":
		heading, how, level = "Agent waiting", "waits for an answer in "+what, watch.Warning
	case "done":
		heading, how, level = "Agent done", "is done in "+what, watch.Success
	case "idle":
		heading, how = "Agent idle", "is idle in "+what
	case "ended":
		heading, how = "Agent ended", "ended in "+what
	case "closed":
		heading, how = "Agent closed", "was closed in "+what
	}
	return watch.Event{Key: agentKey(r), What: name, Heading: heading, Line: name + " " + how, Project: what, Title: r.Title, Level: level}
}

// readAgents lists the agents unagit started - those it wrote down - with
// what herdr says of the ones in it: what each is doing, its conversation,
// the workspace and tab it is in. herdr's other agents are not unagit's to
// show.
func readAgents(herdr *mux.Client, sessions *session.Store) ([]agentRow, error) {
	var rows []agentRow
	var err error
	known := map[string]mux.HerdrAgent{}
	var recorded []session.Record
	inHerdr := false
	for _, r := range sessions.List() {
		if _, ok := agents.ByID(r.Editor); ok {
			recorded = append(recorded, r)
			inHerdr = inHerdr || r.Mux == mux.Herdr
		}
	}
	if herdr != nil && inHerdr {
		var found []mux.HerdrAgent
		found, err = herdr.Agents()
		for _, h := range found {
			known[h.Pane] = h
		}
	}
	for _, r := range recorded {
		r := r
		row := agentRow{Kind: r.Editor, Dir: r.Dir, Pane: r.Pane, Record: &r, Where: "this terminal"}
		switch {
		case r.Mux == mux.Herdr:
			row.Where = "herdr"
			if h, ok := known[r.Pane]; ok {
				row.Status, row.Title, row.Workspace = h.Status, h.Title, h.Workspace
				row.Where = "herdr · " + h.Workspace
				if h.Tab != "" && h.Tab != h.Workspace {
					row.Where += " › " + h.Tab
				}
			} else if herdr != nil && err == nil {
				// Its pane is open, but herdr sees no agent in it: the agent
				// ended and left its shell.
				row.Status = "ended"
			}
		case r.Mux != "":
			row.Where = muxName(r.Mux)
		}
		row.Branch, _ = workspace.WorktreeHead(r.Dir)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if l, r := agentStatusOrder(rows[i].Status), agentStatusOrder(rows[j].Status); l != r {
			return l < r
		}
		return rows[i].Record.Since.After(rows[j].Record.Since)
	})
	return rows, err
}

// agentWhat is what the agent works on, as the lists name it: the merge
// request or worktree unagit opened it in, or the repository, worktree or
// clone its directory is.
func (a *App) agentWhat(r agentRow) string {
	if r.Record != nil && r.Record.Project != "" {
		return r.Record.Label()
	}
	dir := filepath.Clean(r.Dir)
	for _, w := range a.worktrees {
		if sameDirectory(w.Dir, dir) {
			return w.Path
		}
		for _, m := range w.Members {
			if sameDirectory(m.Dir, dir) {
				return m.Path
			}
		}
	}
	for _, p := range a.projects {
		if sameDirectory(a.projectDir(p.Instance, p.PathWithNamespace), dir) {
			return p.PathWithNamespace
		}
	}
	return ""
}

// agentState is the state column: a glyph and a word, in the state's colour.
func agentState(status string, frame int) (string, string, string) {
	switch status {
	case "working":
		return spinnerGlyph(frame), "working", "agents.working"
	case "blocked":
		return glyphManual, "waiting", "agents.waiting"
	case "done":
		return glyphCheck, "done", "agents.idle"
	case "idle":
		return glyphDot, "idle", "agents.idle"
	case "ended":
		return glyphRing, "ended", "agents.unknown"
	case "":
		return glyphRing, "", "agents.unknown"
	}
	return glyphRing, status, "agents.unknown"
}

// agentRecord is what an editor opened in the agent's directory records.
func (a *App) agentRecord(r agentRow) session.Record {
	if r.Record != nil {
		rec := *r.Record
		rec.Pane, rec.Mux, rec.MuxSession, rec.MuxLauncher, rec.Editor = "", "", "", "", ""
		return rec
	}
	return session.Record{Project: a.agentWhat(r), Title: r.Branch, Mode: session.ModeBranch}
}

// goToAgent brings an agent forward where it runs: in herdr, and herdr's
// Ghostty terminal with it when unagit is not in herdr itself; in its
// Zellij pane or Ghostty terminal.
func (a *App) goToAgent(r agentRow) {
	go func() {
		err := a.reachAgent(r)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%v", err)
				return
			}
			what := a.agentWhat(r)
			if what == "" {
				what = tildePath(r.Dir)
			}
			a.done("went to " + agentName(r.Kind) + " · " + what)
		})
	}()
}

func (a *App) reachAgent(r agentRow) error {
	if r.herdrAgent() {
		h := a.herdr()
		if h == nil {
			return fmt.Errorf("herdr is off; turn it on in Settings › Integrations")
		}
		if err := h.FocusAgent(r.Pane); err != nil {
			return err
		}
		a.bringHerdrForward(r.Workspace)
		return nil
	}
	if r.Record == nil || r.Pane == "" {
		return fmt.Errorf("%s runs in a terminal of its own; go there", agentName(r.Kind))
	}
	c := a.clientOf(*r.Record)
	if c == nil {
		return fmt.Errorf("%s runs in %s, which this unagit cannot reach; go there", agentName(r.Kind), r.Where)
	}
	return c.Focus(r.Pane)
}

// bringHerdrForward shows the terminal herdr runs in, when unagit is not in
// herdr itself and Ghostty is: herdr titles its terminal "<host>:
// <workspace>", so the terminal is found by the workspace just focused.
// Where it cannot be found, herdr is still on the agent, for whatever brings
// it forward.
func (a *App) bringHerdrForward(workspaceLabel string) {
	if c := a.multiplexer; c != nil && c.Kind == mux.Herdr || workspaceLabel == "" {
		return
	}
	g := a.ghostty()
	if g == nil {
		return
	}
	// herdr retitles its terminal a moment after the focus moves.
	for i := 0; i < 10; i++ {
		if g.FocusEndingWith(": "+workspaceLabel) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// closeAgent ends an agent in herdr, after asking: its conversation goes
// with its pane.
func (a *App) closeAgent(r agentRow) {
	if !r.herdrAgent() {
		a.flash(agentName(r.Kind) + " runs in " + r.Where + " - close it there")
		return
	}
	h := a.herdr()
	if h == nil {
		a.flash("herdr is off; turn it on in Settings › Integrations")
		return
	}
	what := a.agentWhat(r)
	if what == "" {
		what = tildePath(r.Dir)
	}
	body := fmt.Sprintf("Close %s in %s?\n\nIts herdr pane closes with it, and what it was doing stops.", agentName(r.Kind), what)
	a.confirmWith("Close Agent", body, "Close", nil, func() {
		go func() {
			err := h.ClosePane(r.Pane)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					a.errorf("%v", err)
					return
				}
				a.done("closed " + agentName(r.Kind) + " · " + what)
				a.agentsNowAsk()
			})
		}()
	})
}

// agentsNowAsk reads the agents again without waiting for the next look.
func (a *App) agentsNowAsk() {
	select {
	case a.agentsNow <- struct{}{}:
	default:
	}
}

// agentActionsOf are what can be done with one agent.
func (a *App) agentActionsOf(p *pane, r agentRow) []uiAction {
	return []uiAction{
		{name: "Go to Agent", about: "Bring the agent forward where it runs: its herdr tab - and herdr's Ghostty terminal with it - or its Zellij pane or Ghostty terminal.", keys: "Enter", rank: 10, run: p.enter},
		{name: "Open", about: "Open the agent's directory in your default editor, here.", keys: "Ctrl-O", rank: 20, run: func() { p.onOpen(false) }},
		{name: "Open With…", about: "Choose the editor, then open the agent's directory here.", keys: "Alt-O", rank: 25, run: func() { p.onOpen(true) }},
		{name: "Close Agent…", about: "End the agent in herdr: its pane closes, and what it was doing stops. Asks first.", keys: "x", rank: 60,
			when: r.herdrAgent, run: func() { a.closeAgent(r) }},
	}
}

func (a *App) runningAgentsAction(keys string) uiAction {
	return uiAction{name: "Running Agents…", about: "Go to a coding agent at work, the ones waiting for an answer first.", keys: keys, rank: 505, run: a.showRunningAgents}
}

// showRunningAgents lists the agents to go to one, from any screen.
func (a *App) showRunningAgents() {
	if len(a.agentRows) == 0 {
		a.note("no agents started from unagit")
		return
	}
	items := make([]pickItem, len(a.agentRows))
	table := make([][]string, len(a.agentRows))
	for i, r := range a.agentRows {
		glyph, word, colour := agentState(r.Status, a.spinFrame)
		what := a.agentWhat(r)
		if what == "" {
			what = tildePath(r.Dir)
		}
		name := agentName(r.Kind)
		if icon := agentIcons[r.Kind]; icon != "" {
			name = icon + " " + name
		}
		table[i] = []string{tag(role(colour)) + esc(strings.TrimSpace(glyph+" "+word)) + tagEnd, esc(name), esc(what), esc(trunc(r.Title, 40))}
		items[i] = pickItem{About: strings.TrimSpace(r.Title + " · " + r.Where), Data: r}
	}
	header, labels := pickTable([]string{"STATE", "AGENT", "REPOSITORY", "TITLE"}, table)
	for i := range items {
		items[i].Label = labels[i]
	}
	a.showPickerWith("Running agents", items, pickerOptions{wide: true, explain: true, header: header, enterHint: "go to"}, func(it pickItem) {
		a.goToAgent(it.Data.(agentRow))
	})
}
