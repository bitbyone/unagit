package ui

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// herdrOn says whether herdr may be used: what the user chose, or, unset,
// whether it is installed.
func (a *App) herdrOn() bool {
	if on := a.cfg.Integrations.Herdr; on != nil {
		return *on
	}
	_, err := a.executable(mux.Herdr)
	return err == nil
}

// ghosttyOn is the same for Ghostty, which only a Mac has.
func (a *App) ghosttyOn() bool {
	if on := a.cfg.Integrations.Ghostty; on != nil && !*on {
		return false
	}
	return a.findGhosttyClient() != nil
}

// agentOn says whether an agent is offered: what the user chose, or,
// unset, whether it is installed.
func (a *App) agentOn(ag agents.Agent) bool {
	if on, set := a.cfg.Integrations.Agents[ag.ID]; set {
		return on
	}
	_, err := a.executable(ag.Command)
	return err == nil
}

func (a *App) setAgentOn(ag agents.Agent, on bool) {
	if a.cfg.Integrations.Agents == nil {
		a.cfg.Integrations.Agents = map[string]bool{}
	}
	a.cfg.Integrations.Agents[ag.ID] = on
}

// detectMultiplexer finds what unagit runs in, again after herdr is turned
// on or off.
func (a *App) detectMultiplexer() {
	if a.findMux != nil {
		a.multiplexer = a.findMux()
		return
	}
	a.multiplexer = mux.Detect(os.Getenv, a.executable, a.herdrOn())
}

// herdr is the herdr server agents are started in: the one unagit runs in,
// or the user's usual one from outside herdr.
func (a *App) herdr() *mux.Client {
	if !a.herdrOn() {
		return nil
	}
	if c := a.multiplexer; c != nil && c.Kind == mux.Herdr {
		return c
	}
	if a.findHerdr != nil {
		return a.findHerdr()
	}
	return mux.DetectHerdr(os.Getenv, a.executable)
}

// ghostty is Ghostty, when it is on. A split is possible only when unagit
// runs in a Ghostty terminal of its own, not in a multiplexer inside one.
func (a *App) ghostty() *mux.Client {
	if on := a.cfg.Integrations.Ghostty; on != nil && !*on {
		return nil
	}
	return a.findGhosttyClient()
}

func (a *App) findGhosttyClient() *mux.Client {
	if a.findGhostty != nil {
		return a.findGhostty()
	}
	return mux.DetectGhostty(os.Getenv, a.executable, os.Getenv("TERM_PROGRAM") == "ghostty" && a.multiplexer == nil)
}

// placeKeyOf names a place so the choice can be remembered.
func placeKeyOf(place editorPlace) string {
	if place.client == nil {
		return "here"
	}
	return place.client.Kind + "-" + map[mux.Placement]string{
		mux.Tab: "tab", mux.Vertical: "right", mux.Horizontal: "down", mux.Window: "window",
	}[place.where]
}

// clientPlaces are the places one client offers, as picker items.
func (a *App) clientPlaces(c *mux.Client) []pickItem {
	var items []pickItem
	name := c.Name()
	for _, where := range c.Places() {
		place := editorPlace{client: c, where: where}
		label, about := "", ""
		switch where {
		case mux.Tab:
			label, about = name+" Tab", "A new tab of its own, named after what it opens."
			if c.Kind == mux.Herdr || c.Kind == mux.Zellij {
				about = "A new tab beside unagit's, named after what it opens."
			}
		case mux.Vertical:
			label, about = name+" Split Right", "Beside unagit, in the same "+map[bool]string{true: "terminal", false: "tab"}[c.Kind == mux.Ghostty]+"."
		case mux.Horizontal:
			label, about = name+" Split Below", "Below unagit, in the same "+map[bool]string{true: "terminal", false: "tab"}[c.Kind == mux.Ghostty]+"."
		case mux.Window:
			if c.Kind == mux.Herdr {
				label, about = "herdr · "+mux.AgentsWorkspace, "A tab of herdr's "+mux.AgentsWorkspace+" workspace, named after what it opens, brought forward in herdr - wherever herdr is shown. The workspace is made the first time."
			} else {
				label, about = name+" Window", "A new window of its own."
			}
		}
		items = append(items, pickItem{Label: label, About: about, Data: place})
	}
	return items
}

// agentPlaces are everywhere an agent can be opened, this terminal first.
func (a *App) agentPlaces(ag agents.Agent) []pickItem {
	items := []pickItem{{Label: "This Terminal", Data: editorPlace{},
		About: "Suspend unagit and run " + ag.Name + " here; unagit comes back when it ends."}}
	if c := a.multiplexer; c != nil {
		items = append(items, a.clientPlaces(c)...)
	}
	if c := a.herdr(); c != nil && (a.multiplexer == nil || a.multiplexer.Kind != mux.Herdr) {
		items = append(items, a.clientPlaces(c)...)
	}
	if c := a.ghostty(); c != nil {
		items = append(items, a.clientPlaces(c)...)
	}
	return items
}

// pickPlace asks where something opens, the cursor on where the last agent
// went when that place is still offered.
func (a *App) pickPlace(title string, items []pickItem, then func(editorPlace)) {
	start := 0
	for i, it := range items {
		if placeKeyOf(it.Data.(editorPlace)) == a.cfg.Integrations.AgentPlace {
			start = i
		}
	}
	a.showPickerWith(title, items, pickerOptions{start: start, pack: true, explain: true}, func(it pickItem) {
		then(it.Data.(editorPlace))
	})
}

// agentActions are the "Open in <agent>…" of every agent that is on: each
// asks where, then opens the directory open would.
func (a *App) agentActions(open func(editorPlace)) []uiAction {
	var actions []uiAction
	for _, ag := range agents.All {
		ag := ag
		actions = append(actions, uiAction{
			name:  "Open in " + ag.Name + "…",
			about: "Start " + ag.Name + " in this directory: in this terminal, or in a tab, split or window - of herdr, Zellij or Ghostty, whichever are here.",
			rank:  21,
			when:  func() bool { return a.agentOn(ag) },
			run: func() {
				if !a.agentOn(ag) {
					a.flash(ag.Name + " is off - turn it on in Settings › Integrations")
					return
				}
				a.pickPlace("Open in "+ag.Name+" · where", a.agentPlaces(ag), func(place editorPlace) {
					a.cfg.Integrations.AgentPlace = placeKeyOf(place)
					a.saveConfig()
					place.agent = &ag
					open(place)
				})
			},
		})
	}
	return actions
}

// openAgent starts an agent in a directory that is ready: here, suspending
// unagit as a terminal editor does; in herdr, which starts it itself and
// then knows it as an agent; elsewhere as any command in a pane.
func (a *App) openAgent(dir string, what session.Record, place editorPlace) {
	ag := *place.agent
	a.tv.QueueUpdateDraw(func() { a.closeModal(pageTask) })
	bin, err := a.executable(ag.Command)
	if err != nil {
		a.tv.QueueUpdateDraw(func() {
			a.flash(ag.Command + " is not on PATH - install " + ag.Name + ", see Settings › Integrations")
		})
		return
	}
	what.Dir, what.Editor, what.Launcher = dir, ag.ID, bin
	what.Branch, _ = workspace.WorktreeHead(dir)
	switch {
	case place.client == nil:
		cmd := exec.Command(bin)
		cmd.Dir = dir
		err := a.runInTerminal(dir, what, cmd)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%s: %v", ag.Name, err)
			} else {
				a.done(ag.Name + " ended in " + tildePath(dir))
			}
		})
	case place.client.Kind == mux.Herdr:
		a.startHerdrAgent(what, place, ag)
	default:
		a.openInPane(what, place, exec.Command(bin), "opened "+ag.Name+" in "+place.client.Name()+": ")
	}
}

// startHerdrAgent opens a shell where asked and has herdr start the agent
// in it, so herdr follows what the agent is doing.
func (a *App) startHerdrAgent(what session.Record, place editorPlace, ag agents.Agent) {
	var job *bgJob
	a.tv.QueueUpdateDraw(func() { job = a.startJob("Starting " + ag.Name + " in herdr") })
	defer a.tv.QueueUpdateDraw(func() { a.endJob(job) })
	c := place.client
	pane, err := c.OpenShell(place.where, what.Dir, muxTabName(what))
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	if err := c.StartAgent(pane, herdrAgentName(what), ag.HerdrKind, nil); err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%s did not start in herdr: %v", ag.Name, err) })
		return
	}
	a.recordPane(what, c, pane, nil, "started "+ag.Name+" in herdr: ")
	if place.where == mux.Window {
		a.bringHerdrForward(mux.AgentsWorkspace)
	}
	a.agentsNowAsk()
}

var notName = regexp.MustCompile(`[^a-z0-9]+`)

// herdrAgentName is how herdr is to call the agent: readable, from what it
// works on, and never the same twice.
func herdrAgentName(what session.Record) string {
	name := strings.Trim(notName.ReplaceAllString(strings.ToLower(muxTabName(what)), "-"), "-")
	if name == "" {
		name = "agent"
	}
	var id [2]byte
	_, _ = rand.Read(id[:])
	return fmt.Sprintf("%s-%x", name, id)
}
