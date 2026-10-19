package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/unibox"
)

// What can be done on the Sandboxes screen, each a unibox command. What
// takes the terminal - an agent, a shell, a sign-in - runs where the user
// picks, as an agent opened from a worktree does: this terminal, or a tab,
// split or window of Zellij, herdr or Ghostty. What reads or changes
// something runs behind the interface and says what unibox said.

// sandboxRowActions are what can be done with a task, a sandbox's card or
// a profile.
func (a *App) sandboxRowActions(r sandboxRow) []uiAction {
	detail := uiAction{name: "Show Details", about: "Move into the detail: the whole configuration, as unibox reports it.",
		keys: "Enter", rank: 5, run: func() { a.sandboxes.openChosenDetail() }}
	switch r.kind {
	case rowTask:
		return append(a.taskActions(r), detail)
	case rowSandbox:
		return append(a.sandboxActions(r), detail)
	}
	return append(a.profileActions(r), detail)
}

// profileAgents are the agents a row's profile offers.
func profileAgents(r sandboxRow) []string {
	if len(r.profile.Agents) > 0 {
		return r.profile.Agents
	}
	if r.kind == rowTask && r.task.Agent != "" {
		return []string{r.task.Agent}
	}
	return nil
}

// agentFor is an agent as the place picker wants it: unagit's own where it
// knows it, else one named by unibox's id.
func agentFor(id string) agents.Agent {
	if ag, ok := agents.ByID(id); ok {
		return ag
	}
	return agents.Agent{ID: id, Name: id, Command: id, HerdrKind: id}
}

func (a *App) taskActions(r sandboxRow) []uiAction {
	t := r.task
	acts := []uiAction{
		{name: "Resume…", about: "Run the task's own agent again, " + agentLabel(t.Agent) + ": here, or in a tab, split or window, the usual place first. One at work is gone to.",
			keys: "o", rank: 10, run: func() { a.resumeTask(r, t.Agent) }},
		{name: "Resume with Agent…", about: "Run the task with another of its profile's agents: which first, then where.",
			keys: "a", rank: 12, when: func() bool { return len(profileAgents(r)) > 1 }, run: func() { a.pickTaskAgent(r) }},
		{name: "Open Shell in Task…", about: "A shell in the task's directory in the sandbox, where its agent works; it ends when you leave it.",
			keys: "s", rank: 20, run: func() { a.uniboxShell("Shell in "+t.Name(), "resume", t.ID, "--shell") }},
		{name: "Rename Task…", about: "Give the task a name taken wherever its ID is; an empty name takes the one it has away.",
			keys: "e", rank: 40, run: func() { a.renameTask(r) }},
		{name: "Copy Resume Command", about: "Put unibox resume with the task on the clipboard, to go on with it from another terminal.",
			keys: "y", rank: 45, run: func() { a.yank("Resume command", "unibox resume "+t.Name()) }},
		{name: "Open Repository in Browser", about: "The forge's page of the task's first repository, where what its agent pushes arrives.",
			keys: "w", rank: 46, when: func() bool { return len(t.Repositories) > 0 },
			run: func() { a.openWeb(strings.TrimSuffix(t.Repositories[0].URL, ".git")) }},
		{name: "Remove Task…", about: "Take the task and its clones out of the sandbox. unibox refuses while it has a session or work not pushed.",
			keys: "d", rank: 90, run: func() { a.removeTask(r) }},
	}
	return append(acts, a.taskAgentActions(r)...)
}

// taskAgentActions are the second and third tiers of resuming, as opening
// has them: "Resume with <agent>…" for each agent of the profile, asking
// where, and - found only by typing - each agent in each place at once.
func (a *App) taskAgentActions(r sandboxRow) []uiAction {
	var acts []uiAction
	for _, id := range profileAgents(r) {
		ag := agentFor(id)
		aliases := append([]string{}, agentAliases[id]...)
		acts = append(acts, uiAction{
			name:  "Resume with " + ag.Name + "…",
			about: "Run " + ag.Name + " in the task, in the sandbox: here, or in a tab, split or window, the usual place first.",
			rank:  600, icon: agentIcons[id], aliases: aliases,
			run: func() { a.resumeTask(r, id) },
		})
		for _, place := range placesOf(a.agentPlaces(ag)) {
			acts = append(acts, uiAction{
				name:  "Resume with " + ag.Name + " in " + placeName(place),
				about: "Run " + ag.Name + " in the task, in the sandbox, in " + placeAbout(place) + ".",
				rank:  700, icon: agentIcons[id], filterOnly: true, prefer: placePrefer(place.where),
				aliases: append(append([]string{}, aliases...), placeAliases(place.where)...),
				run: func() {
					a.cfg.State.UsePlace(placeOfAgent, placeKeyOf(place))
					a.cfg.State.AgentPlace = placeKeyOf(place)
					a.saveState()
					a.resumeTaskIn(r, id, place)
				},
			})
		}
	}
	return acts
}

func (a *App) sandboxActions(r sandboxRow) []uiAction {
	s := r.sandbox
	running := func() bool { return s.State == "running" }
	return []uiAction{
		{name: "New Task…", about: "Clone a worktree or a repository of yours into this sandbox and start an agent on it: the agent, then where.",
			keys: "n", rank: 10, run: func() { a.newSandboxTask(r) }},
		{name: "Open Shell…", about: "A shell in the sandbox, in /work: here, or in a tab, split or window. It counts as a session while it is open.",
			keys: "s", rank: 20, run: func() { a.uniboxShell("Shell in "+s.Name, "sandbox", "shell", s.Name) }},
		{name: "Ports…", about: "The sandbox's ports: switch one on or off while it runs, or open one in the browser. The profile stays as it is.",
			keys: "p", rank: 30, run: func() { a.showSandboxPorts(r) }},
		{name: "Run Script…", about: "Run one of the profile's scripts in the sandbox now, in /work, its output in a log.",
			keys: "!", rank: 35, run: func() { a.runSandboxScript(r) }},
		{name: "Show Instructions", about: "The instructions the sandbox's agents get, as unibox puts them together, attached texts included.",
			keys: "i", rank: 40, run: func() { a.showInstructions(s.Name, "") }},
		{name: "Stop Sandbox", about: "Stop the VM; its disk, caches and tasks stay, and the next start boots it again. unibox refuses while an agent runs.",
			keys: "x", rank: 50, when: running, run: func() { a.uniboxDo("Stopping "+s.Name, "sandbox", "stop", s.Name) }},
		{name: "Use as Context", about: "Make this the sandbox, and its profile the profile, unibox commands work in when they are not told.",
			keys: "u", rank: 60, when: func() bool { return a.ubx.overview.Context.Set.Sandbox != s.Name },
			run: func() { a.uniboxDo("Using "+s.Name, "context", "use", "-s", s.Name, "-p", s.Profile) }},
		{name: "Recreate Sandbox…", about: "Build the image when it needs it and make the VM again from the profile as it is now. Work not pushed is never lost.",
			keys: "C", rank: 70, run: func() { a.recreateSandbox(r) }},
		{name: "Remove Sandbox…", about: "Remove the VM and every task in it. unibox refuses while it has a session or work not pushed.",
			keys: "d", rank: 90, run: func() { a.removeSandbox(r) }},
	}
}

func (a *App) profileActions(r sandboxRow) []uiAction {
	pr := r.profile
	broken := func() bool { return pr.Error != "" }
	return []uiAction{
		{name: "New Task…", about: "Clone a worktree or a repository of yours into a sandbox of this profile and start an agent on it.",
			keys: "n", rank: 10, run: func() { a.newSandboxTask(r) }},
		{name: "New Sandbox…", about: "Make a sandbox of this profile, named by you, without a task in it yet.",
			keys: "c", rank: 15, run: func() { a.newSandbox(pr) }},
		{name: "Build Image", about: "Build the profile's image for the default runtime, check its agents and load it; its output in a log.",
			keys: "b", rank: 20, run: func() { a.uniboxStream("Building "+pr.Name, "profile", "build", pr.Name) }},
		{name: "Rebuild Image", about: "Build the profile's image again even when it is current, its layers from scratch.",
			rank: 21, run: func() { a.uniboxStream("Rebuilding "+pr.Name, "profile", "build", pr.Name, "--rebuild") }},
		{name: "Migrate Profile", about: "Bring the profile to the format this unibox reads, with unibox init; what it cannot carry over it names.",
			keys: "m", rank: 22, when: broken, run: func() { a.uniboxStream("Migrating "+pr.Name, "init", pr.Name) }},
		{name: "Show Changes Since Build", about: "What changed in the profile since its image was last built, as a diff.",
			keys: "D", rank: 30, run: func() { a.showProfileDiff(pr) }},
		{name: "Show Instructions", about: "The instructions the profile's agents get, as unibox puts them together.",
			keys: "i", rank: 31, run: func() { a.showInstructions("", pr.Name) }},
		{name: "Network…", about: "The hosts the profile's sandboxes may reach beyond the runtime's policy, and those refused: add, remove.",
			keys: "N", rank: 32, run: func() { a.showProfileNetwork(pr) }},
		{name: "Services…", about: "The forges and services the sandboxes are signed in to, and whether each token is stored: add, remove.",
			keys: "v", rank: 33, run: func() { a.showProfileServices(pr) }},
		{name: "Sign In Agents…", about: "Sign the profile's agents in, with unibox agent login in this terminal: the tokens go to the platform's store.",
			keys: "l", rank: 34, run: func() { a.uniboxSignIn("Signing in the agents of "+pr.Name, "agent", "login", "-p", pr.Name) }},
		{name: "Open in Editor", about: "Open the profile's directory of the environments repository in the default editor.",
			keys: "O", rank: 40, when: func() bool { return a.ubx.overview.Environments != "" }, run: func() { a.openProfileDir(pr, false) }},
		{name: "Use as Context", about: "Make this the profile unibox commands work in when they are not told.",
			keys: "u", rank: 60, when: func() bool { return a.ubx.overview.Context.Set.Profile != pr.Name },
			run: func() { a.uniboxDo("Using "+pr.Name, "context", "use", "-p", pr.Name) }},
	}
}

// sandboxesActions are what the screen itself can do, from any panel.
func (a *App) sandboxesActions(p *pane) []uiAction {
	v := a.sandboxes
	env := func() bool { return a.ubx.overview.Environments != "" }
	return append([]uiAction{
		{name: "Refresh", about: "Read the sandboxes, their tasks, the profiles and the environment from unibox again.", keys: "r", rank: 30, run: a.refreshSandboxes},
		{name: "Go to Sandbox Cards", about: "Move to the cards of the sandboxes: h and l between them, their actions on each.", keys: "B", rank: 40,
			run: func() { v.focus(v.cards) }},
		{name: "Go to Tasks", about: "Move to the tasks of every sandbox, where agents are resumed.", keys: "t", rank: 41, run: func() { v.focus(p.table) }},
		{name: "Go to Profiles", about: "Move to the profiles: their images, agents, services and network.", keys: "P", rank: 42,
			when: func() bool { return v.lowerH > 0 }, run: func() { v.focus(v.profiles) }},
		{name: "Go to Log", about: "Move to the log of what was asked of unibox and what it said.", keys: "L", rank: 43,
			when: func() bool { return v.lowerH > 0 }, run: func() { v.focus(v.log) }},
		{name: "Go to Environment", about: "Move to the environment: the profiles' repository, the context, the runtimes.", keys: "E", rank: 44,
			run: func() { v.focus(v.env) }},
		{name: "Secrets…", about: "The tokens kept in the platform's store for profiles to name: store one, replace it, remove it. No value is ever shown.",
			keys: "K", rank: 50, run: a.showSecrets},
		{name: "Values…", about: "Your own values for what profiles ask of you - a name, a login - shared or one profile's: set, change, remove.",
			keys: "V", rank: 51, run: a.showValues},
		{name: "unibox Settings…", about: "unibox's own configuration: the default runtime, which runtimes may be used, whether an idle sandbox stops.",
			keys: "U", rank: 52, run: a.showUniboxSettings},
		{name: "Validate Profiles", about: "Check the environments repository - its layout, every profile, every instruction text - in a log.",
			rank: 82, when: env, run: func() { a.uniboxStream("Validating the profiles", "profile", "validate") }},
		{name: "Prune Images…", about: "Remove the images unibox no longer needs: older builds, and those of profiles and sandboxes that are gone.",
			rank: 85, run: func() {
				a.confirmWith("Prune images", "Remove every image unibox no longer needs?", "Prune", nil, func() {
					a.closeModal(pageConfirm)
					a.uniboxStream("Pruning images", "prune")
				})
			}},
	}, a.listActions(p)...)
}

// uniboxDo runs a command that changes something, behind the interface,
// and says what unibox said it did. A refusal is a warning: unibox keeps
// work it would lose, and says how to go on.
func (a *App) uniboxDo(title string, args ...string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	job := a.startJob(title)
	e := a.logUnibox(title, args...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		said, err := c.Do(ctx, args...)
		a.tv.QueueUpdateDraw(func() {
			a.endJob(job)
			e.end(a, said, err, refusal(err))
			a.sayUnibox(title, said, err)
			a.refreshSandboxes()
		})
	}()
}

// refusal says whether unibox refused: it keeps work it would lose, and
// says how to go on.
func refusal(err error) bool {
	var failure *unibox.Error
	return errors.As(err, &failure) && failure.Refused()
}

func (a *App) sayUnibox(title, said string, err error) {
	switch {
	case refusal(err):
		a.flash(err.Error())
	case err != nil:
		a.errorf("%v", err)
	case said != "":
		a.done(said)
	default:
		a.done(strings.ToLower(title[:1]) + title[1:] + " - done")
	}
}

// uniboxStream runs a command whose progress is worth reading in a log.
func (a *App) uniboxStream(title string, args ...string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	e := a.logUnibox(title, args...)
	a.runTaskThen(title, func(log func(string)) (string, error) {
		var lines []string
		err := c.Stream(context.Background(), func(line string) {
			lines = append(lines, line)
			log(line)
		}, args...)
		a.tv.QueueUpdateDraw(func() {
			e.output = lines
			said := ""
			if len(lines) > 0 {
				said = lines[len(lines)-1]
			}
			e.end(a, said, err, refusal(err))
		})
		return "", err
	}, func(string) {
		a.done(strings.ToLower(title[:1]) + title[1:] + " - done")
		a.refreshSandboxes()
	})
}

// paneDir is where a pane opened for a sandbox starts: the sandbox has no
// directory of the host's, so the home directory.
func paneDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "/"
}

// resumeTask asks where to run an agent in a task and runs it there. An
// agent at work in the task already is gone to instead, when unagit knows
// its pane.
func (a *App) resumeTask(r sandboxRow, agent string) {
	if agent == "" {
		agent = r.task.Agent
	}
	if agent == r.task.Agent && r.working() {
		if rec, ok := a.taskSession(r.task); ok {
			row := agentRow{Kind: rec.Editor, Dir: rec.Dir, Pane: rec.Pane, Record: &rec}
			a.goToAgent(row)
			return
		}
	}
	ag := agentFor(agent)
	a.pickPlace(placeOfAgent, "Resume "+r.task.Name()+" with "+ag.Name+" · where", a.agentPlaces(ag), func(place editorPlace) {
		a.resumeTaskIn(r, agent, place)
	})
}

// taskSession is the record of the agent unagit started in a task, still
// in its pane.
func (a *App) taskSession(t unibox.Task) (session.Record, bool) {
	for _, rec := range a.sessions.List() {
		if rec.Task == t.ID && rec.Pane != "" {
			return rec, true
		}
	}
	return session.Record{}, false
}

func (a *App) resumeTaskIn(r sandboxRow, agent string, place editorPlace) {
	args := []string{"resume", r.task.ID}
	if agent != "" && agent != r.task.Agent {
		args = append(args, "--agent", agent)
	}
	a.runSandboxAgent(a.taskRecord(r.task, agent), place, args)
}

// taskRecord is what is written down of an agent in a task, so Activity
// lists it with its sandbox.
func (a *App) taskRecord(t unibox.Task, agent string) session.Record {
	branch := ""
	if len(t.Repositories) > 0 {
		branch = t.Repositories[0].Branch
	}
	return session.Record{Project: t.Sandbox + "/" + t.Name(), Title: branch, Branch: branch, Mode: session.ModeBranch,
		Dir: paneDir(), Editor: agent, Sandbox: t.Sandbox, Task: t.ID}
}

// runSandboxAgent runs a unibox command that runs an agent, in a place:
// here, suspending unagit until it ends, or in a pane, written down there.
func (a *App) runSandboxAgent(what session.Record, place editorPlace, args []string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	cmd := c.Command(args...)
	cmd.Dir = what.Dir
	name := agentLabel(what.Editor)
	a.logTerminal(name+" in "+what.Label(), place, args)
	go func() {
		if place.client == nil {
			what.Launcher = c.Binary
			err := a.runInTerminal(what.Dir, what, cmd)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					a.errorf("%s in %s: %v", name, what.Label(), err)
				} else {
					a.done(name + " ended in " + what.Label())
				}
				a.refreshSandboxes()
			})
			return
		}
		what.Launcher = c.Binary
		a.openInPane(what, place, cmd, "opened "+name+" in "+place.client.Name()+": ")
		a.tv.QueueUpdateDraw(a.refreshSandboxes)
	}()
}

// uniboxShell asks where a shell in a sandbox opens, and opens it there.
// A shell is not written down: it is no editor and no agent.
func (a *App) uniboxShell(title string, args ...string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	here := "Suspend unagit and open the shell here; unagit comes back when it ends."
	a.pickPlace(placeOfAgent, title+" · where", a.places(here, true), func(place editorPlace) {
		a.logTerminal(title, place, args)
		cmd := c.Command(args...)
		cmd.Dir = paneDir()
		go func() {
			if place.client == nil {
				err := a.runTerminalEditor(cmd, editors.Editor{Name: "unibox"})
				a.tv.QueueUpdateDraw(func() {
					if err == nil {
						a.done(strings.ToLower(title[:1]) + title[1:] + " ended")
					}
					a.refreshSandboxes()
				})
				return
			}
			client, err := a.besideUnagit(place)
			if err == nil {
				_, err = client.Open(place.where, cmd.Dir, title, cmd)
			}
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					a.errorf("%v", err)
				} else {
					a.done("opened " + strings.ToLower(title[:1]) + title[1:] + " in " + place.client.Name())
				}
			})
		}()
	})
}

// uniboxSignIn runs a unibox command that asks for a token in this
// terminal: unibox takes it there, into the platform's store, and unagit
// never sees it.
func (a *App) uniboxSignIn(title string, args ...string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	cmd := c.Command(args...)
	e := a.logUnibox(title, args...)
	go func() {
		err := a.runTerminalEditor(cmd, editors.Editor{Name: "unibox"})
		a.tv.QueueUpdateDraw(func() {
			e.end(a, "in this terminal", err, false)
			if err == nil {
				a.done(strings.ToLower(title[:1]) + title[1:] + " - done")
			}
			a.refreshSandboxes()
		})
	}()
}

// logTerminal writes down a command that runs an agent or a shell, and
// where: what it does there is the terminal's to say.
func (a *App) logTerminal(title string, place editorPlace, args []string) {
	e := a.logUnibox(title, args...)
	e.level = eventHere
	e.said = "in this terminal"
	if place.client != nil {
		e.said = "in " + placeAbout(place)
	}
	a.sandboxes.drawLog()
}

// pickTaskAgent asks which of the profile's agents runs the task, then
// where.
func (a *App) pickTaskAgent(r sandboxRow) {
	var items []pickItem
	for _, id := range profileAgents(r) {
		about := "Run " + agentLabel(id) + " in the task."
		if id == r.task.Agent {
			about = "The task's own agent, as it last ran."
		}
		items = append(items, pickItem{Label: agentLabel(id), About: about, Data: id})
	}
	a.showPickerWith("Resume "+r.task.Name()+" with", items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		a.resumeTask(r, it.Data.(string))
	})
}

// renameTask asks for the task's new name; an empty one takes the name
// away.
func (a *App) renameTask(r sandboxRow) {
	a.askText("Rename task "+r.task.ID, "Name", r.task.Alias, "Rename", nil, func(name string) {
		if name == "" {
			a.uniboxDo("Taking the name of "+r.task.ID+" away", "task", "rename", r.task.ID, "--reset")
			return
		}
		a.uniboxDo("Renaming "+r.task.Name(), "task", "rename", r.task.ID, name)
	})
}

func (a *App) removeTask(r sandboxRow) {
	t := r.task
	var warnings []string
	if r.working() {
		warnings = append(warnings, "its agent is at work - unibox refuses until it ends")
	}
	body := fmt.Sprintf("Remove task %s from %s, with its clones?\n\nWhat was pushed stays on the forge; unibox refuses while anything is not pushed.", t.Name(), t.Sandbox)
	a.confirmWith("Remove task", body, "Remove", warnings, func() {
		a.closeModal(pageConfirm)
		a.uniboxDo("Removing "+t.Name(), "task", "rm", t.ID, "--yes")
	})
}

func (a *App) removeSandbox(r sandboxRow) {
	s := r.sandbox
	body := fmt.Sprintf("Remove sandbox %s, its disk and its caches?", s.Name)
	var warnings []string
	if len(s.Tasks) > 0 {
		var names []string
		for _, t := range s.Tasks {
			names = append(names, t.Name())
		}
		sort.Strings(names)
		warnings = append(warnings, "its tasks go with it: "+strings.Join(names, ", "))
	}
	if len(s.Runs) > 0 {
		warnings = append(warnings, "an agent runs in it - unibox refuses until it ends")
	}
	a.confirmWith("Remove sandbox", body, "Remove", warnings, func() {
		a.closeModal(pageConfirm)
		a.uniboxDo("Removing "+s.Name, "sandbox", "rm", s.Name, "--yes")
	})
}

func (a *App) recreateSandbox(r sandboxRow) {
	s := r.sandbox
	body := fmt.Sprintf("Make %s again from profile %s as it is now?\n\nIts tasks are cloned again; work not pushed makes unibox refuse.", s.Name, s.Profile)
	a.confirmChoices("Recreate sandbox", body, nil, []choice{
		{"Recreate", func() {
			a.closeModal(pageConfirm)
			a.uniboxStream("Recreating "+s.Name, "sandbox", "recreate", s.Name)
		}},
		{"Rebuild and Recreate", func() {
			a.closeModal(pageConfirm)
			a.uniboxStream("Rebuilding and recreating "+s.Name, "sandbox", "recreate", s.Name, "--rebuild")
		}},
	})
}

// newSandbox asks for a name and makes a sandbox of the profile.
func (a *App) newSandbox(pr unibox.Profile) {
	a.askSandboxName(pr, func(name string) {
		a.uniboxStream("Creating "+name, "sandbox", "create", name, "-p", pr.Name)
	})
}

// askSandboxName asks for a new sandbox's name, offering the profile's
// name with the next number no sandbox has.
func (a *App) askSandboxName(pr unibox.Profile, then func(string)) {
	taken := map[string]bool{}
	for _, s := range a.ubx.overview.Sandboxes {
		taken[s.Name] = true
	}
	suggested := pr.Name
	for n := 1; taken[suggested]; n++ {
		suggested = fmt.Sprintf("%s-%d", pr.Name, n)
	}
	a.askText("New sandbox of "+pr.Name, "Name", suggested, "Create", func(name string) string {
		switch {
		case name == "":
			return "enter a name for the sandbox"
		case taken[name]:
			return "a sandbox " + name + " exists already - choose another name"
		}
		return ""
	}, then)
}

// ------------------------------------------------------------ a new task

// taskSource is a directory of unagit's a task can be made from: a clone,
// a worktree, or a group's members together.
type taskSource struct {
	label, branch string
	dirs          []string
}

// taskSources are the clones and worktrees on disk, worktrees first: work
// usually starts on a branch of its own.
func (a *App) taskSources() []taskSource {
	var out []taskSource
	for _, w := range a.worktrees {
		if w.keptReview() {
			continue
		}
		if w.grouped() {
			var dirs []string
			for _, m := range w.Members {
				dirs = append(dirs, m.Dir)
			}
			out = append(out, taskSource{label: w.Path, branch: w.Branch, dirs: dirs})
			continue
		}
		out = append(out, taskSource{label: w.Path, branch: w.Branch, dirs: []string{w.Dir}})
	}
	var clones []taskSource
	for _, pr := range a.projects {
		info := a.diskOf(pr.Instance, pr.PathWithNamespace)
		if info.Cloned {
			clones = append(clones, taskSource{label: pr.PathWithNamespace, branch: info.Branch, dirs: []string{a.projectDir(pr.Instance, pr.PathWithNamespace)}})
		}
	}
	sort.SliceStable(clones, func(i, j int) bool { return clones[i].label < clones[j].label })
	return append(out, clones...)
}

// newSandboxTask starts a task from the Sandboxes screen: what to clone,
// the sandbox (for a profile's row), the agent, the place.
func (a *App) newSandboxTask(r sandboxRow) {
	sources := a.taskSources()
	if len(sources) == 0 {
		a.flash("nothing is on disk to clone into a sandbox - clone a repository or make a worktree first")
		return
	}
	var items []pickItem
	for _, s := range sources {
		about := "Clone " + strings.Join(tildePaths(s.dirs), ", ") + " into the sandbox, at its branch as the forge has it."
		items = append(items, pickItem{Label: s.label, Sub: s.branch, About: about, Data: s})
	}
	a.showPickerWith("New task · what to work on", items, pickerOptions{explain: true}, func(it pickItem) {
		src := it.Data.(taskSource)
		start := unibox.Start{Dirs: src.dirs}
		if r.kind == rowSandbox {
			start.Sandbox, start.Profile = r.sandbox.Name, r.sandbox.Profile
			a.pickStartAgent(start, src.label, r.profile)
			return
		}
		a.pickStartSandbox(start, src.label, &r.profile)
	})
}

// openInSandboxAction is Open in Sandbox… for a row of the lists: dirs
// are what it clones into the sandbox, and ok says whether it is on disk.
func (a *App) openInSandboxAction(label string, dirs func() []string, ok func() bool) uiAction {
	return uiAction{name: "Open in Sandbox…", about: "Clone it into a unibox sandbox at its branch, as the forge has it, and start an agent there: the sandbox, the agent, the place.",
		rank: 620, aliases: []string{"unibox", "sandbox", "vm"},
		when: func() bool { return a.uniboxOn() && (ok == nil || ok()) },
		run:  func() { a.startInSandbox(label, dirs()) }}
}

// startInSandbox is Open in Sandbox… of a worktree, a group or a clone:
// the sandbox, the agent, the place.
func (a *App) startInSandbox(label string, dirs []string) {
	if !a.uniboxOn() {
		a.flash("unibox is off - turn it on in Settings › Integrations")
		return
	}
	a.withSandboxes(func() {
		a.pickStartSandbox(unibox.Start{Dirs: dirs}, label, nil)
	})
}

// withSandboxes reads unibox first when it has not been yet, so the
// pickers have the profiles and sandboxes to offer.
func (a *App) withSandboxes(then func()) {
	if a.ubx.read && a.ubx.err == nil {
		then()
		return
	}
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var o unibox.Overview
	a.loadThen("Reading the sandboxes", func(func(string)) (string, error) {
		var err error
		o, err = c.Overview()
		return "", err
	}, func(string) {
		a.ubx = uniboxState{overview: o, read: true, at: time.Now(), details: map[string]*sandboxDetail{}}
		a.sandboxRows = sandboxRows(o)
		a.sandboxesPane.reload()
		then()
	})
}

// pickStartSandbox asks which sandbox a task goes into: one of a profile's
// - every profile's, without one - or a new one of it.
func (a *App) pickStartSandbox(start unibox.Start, label string, only *unibox.Profile) {
	o := a.ubx.overview
	var items []pickItem
	type choiceOf struct {
		profile unibox.Profile
		sandbox string
		fresh   bool
	}
	for _, s := range o.Sandboxes {
		if only != nil && s.Profile != only.Name {
			continue
		}
		var pr unibox.Profile
		for _, p := range o.Profiles {
			if p.Name == s.Profile {
				pr = p
			}
		}
		about := fmt.Sprintf("Profile %s, %s, %s. Its tasks share one VM: its memory, its Docker, its ports.", s.Profile, s.Runtime, counted(len(s.Tasks), "task", "tasks"))
		items = append(items, pickItem{Label: s.Name, Sub: s.Profile + " · " + s.State, About: about, Data: choiceOf{profile: pr, sandbox: s.Name}})
	}
	for _, p := range o.Profiles {
		if only != nil && p.Name != only.Name || p.Error != "" {
			continue
		}
		items = append(items, pickItem{Label: "New Sandbox of " + p.Name + "…", About: "A VM of its own for this task, made from " + p.Name + ": nothing it does reaches another task.",
			Data: choiceOf{profile: p, fresh: true}})
	}
	if len(items) == 0 {
		a.flash("no profile to make a sandbox from - add one with unibox init <profile>")
		return
	}
	pick := func(c choiceOf) {
		start.Profile = c.profile.Name
		if !c.fresh {
			start.Sandbox = c.sandbox
			a.pickStartAgent(start, label, c.profile)
			return
		}
		a.askSandboxName(c.profile, func(name string) {
			start.Sandbox = name
			a.pickStartAgent(start, label, c.profile)
		})
	}
	if len(items) == 1 {
		pick(items[0].Data.(choiceOf))
		return
	}
	a.showPickerWith(label+" · which sandbox", items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		pick(it.Data.(choiceOf))
	})
}

// pickStartAgent asks which of the profile's agents works on the task,
// skipped when it has one.
func (a *App) pickStartAgent(start unibox.Start, label string, pr unibox.Profile) {
	if len(pr.Agents) == 0 {
		a.flash("profile " + pr.Name + " has no agents - add one with unibox init " + pr.Name)
		return
	}
	pick := func(id string) {
		start.Agent = id
		ag := agentFor(id)
		a.pickPlace(placeOfAgent, label+" with "+ag.Name+" · where", a.agentPlaces(ag), func(place editorPlace) {
			a.checkAndStart(start, label, place)
		})
	}
	if len(pr.Agents) == 1 {
		pick(pr.Agents[0])
		return
	}
	var items []pickItem
	for _, id := range pr.Agents {
		items = append(items, pickItem{Label: agentLabel(id), About: "Run " + agentLabel(id) + " on the task, in the sandbox.", Data: id})
	}
	a.showPickerWith(label+" · which agent", items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		pick(it.Data.(string))
	})
}

// checkAndStart asks unibox what stands in the way of the start, says it,
// and runs the task where chosen. A branch the forge does not have is
// pushed only when the user says so; anything else unibox run sees to
// itself, or says on the terminal what to do.
func (a *App) checkAndStart(start unibox.Start, label string, place editorPlace) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var check unibox.Check
	a.loadThen("Checking what the task needs", func(func(string)) (string, error) {
		var err error
		check, err = c.Check(start)
		return "", err
	}, func(string) {
		run := func(push bool) {
			start.Push = push
			what := session.Record{Project: start.Sandbox + "/" + label, Mode: session.ModeBranch, Dir: paneDir(),
				Editor: start.Agent, Sandbox: start.Sandbox}
			if len(start.Dirs) > 0 {
				what.Dir = start.Dirs[0]
			}
			a.runSandboxAgent(what, place, append([]string{"run"}, start.Args()...))
		}
		var lines, warnings []string
		unpushed := false
		for _, need := range check.Needs {
			text := need.Message
			if need.Repository != "" && !strings.Contains(text, need.Repository) {
				text = need.Repository + ": " + text
			}
			if need.Warning {
				warnings = append(warnings, text)
				continue
			}
			unpushed = unpushed || need.Code == "branch_not_pushed"
			lines = append(lines, text)
		}
		if check.Ready && len(warnings) == 0 {
			run(false)
			return
		}
		body := "Start " + agentLabel(start.Agent) + " on " + label + " in " + start.Sandbox + "?"
		if len(lines) > 0 {
			body += "\n\nunibox says:\n"
			for _, l := range lines {
				body += "  " + glyphDot + " " + esc(l) + "\n"
			}
			body += "\nunibox run sees to what it can, and says on the terminal what it cannot."
		}
		var choices []choice
		if unpushed {
			choices = append(choices, choice{"Push and Start", func() { a.closeModal(pageConfirm); run(true) }})
		}
		label := "Start"
		if len(lines) > 0 {
			label = "Start Anyway"
		}
		choices = append(choices, choice{label, func() { a.closeModal(pageConfirm); run(false) }})
		a.confirmChoices("New task", body, warnings, choices)
	})
}

func tildePaths(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = tildePath(d)
	}
	return out
}

// ------------------------------------------------------- lists in dialogs

// showSandboxPorts lists a sandbox's ports: Enter switches one on or off
// for the sandbox alone, w opens a published one.
func (a *App) showSandboxPorts(r sandboxRow) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	s := r.sandbox
	var ports []unibox.Port
	a.loadThen("Reading the ports of "+s.Name, func(func(string)) (string, error) {
		var err error
		ports, err = c.Ports(s.Name)
		return "", err
	}, func(string) {
		if len(ports) == 0 {
			a.note(s.Name + " has no ports - name one with unibox network port set <name> --map <mapping> -p " + s.Profile)
			return
		}
		var items []pickItem
		for _, p := range ports {
			state := "off"
			if p.On {
				state = "on"
			}
			items = append(items, pickItem{Label: p.Name, Sub: p.Port + " · " + state, About: portLine(p), Data: p})
		}
		browse := a.browserKey("w", "browser", "Open in Browser", "Open the port where it is published on this machine; the list stays open.",
			func(it pickItem) string {
				p := it.Data.(unibox.Port)
				if len(p.Published) == 0 {
					return ""
				}
				pub := p.Published[0]
				return fmt.Sprintf("http://%s:%d", firstOf(pub.HostIP, "127.0.0.1"), pub.HostPort)
			})
		browse.when = func(it pickItem) bool { return len(it.Data.(unibox.Port).Published) > 0 }
		a.showPickerWith("Ports of "+s.Name, items, pickerOptions{pack: true, explain: true, keys: []pickKey{browse},
			enterName: "Switch On or Off", enterAbout: "Publish the port while the sandbox runs, or take it back; the profile stays as it is."},
			func(it pickItem) {
				p := it.Data.(unibox.Port)
				if p.On || len(p.Published) > 0 {
					a.uniboxDo("Taking "+p.Name+" back", "network", "port", "unpublish", p.Name, "-s", s.Name)
					return
				}
				a.uniboxDo("Publishing "+p.Name, "network", "port", "publish", p.Name, "-s", s.Name)
			})
	})
}

// runSandboxScript asks which of the profile's scripts to run in the
// sandbox, and runs it in a log.
func (a *App) runSandboxScript(r sandboxRow) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	s := r.sandbox
	var scripts map[string]string
	a.loadThen("Reading the scripts of "+s.Profile, func(func(string)) (string, error) {
		var err error
		scripts, err = c.Scripts(s.Name)
		return "", err
	}, func(string) {
		if len(scripts) == 0 {
			a.note("profile " + s.Profile + " has no scripts - name them under scripts in its sandbox.yaml")
			return
		}
		var items []pickItem
		for _, name := range sortedKeys(scripts) {
			items = append(items, pickItem{Label: name, Sub: scripts[name], About: "Run " + scripts[name] + " in " + s.Name + ", in /work, as the agent's user.", Data: name})
		}
		a.showPickerWith("Run script in "+s.Name, items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
			name := it.Data.(string)
			a.uniboxStream("Running "+name+" in "+s.Name, "script", "run", name, "-s", s.Name)
		})
	})
}

// networkRule is one host of a profile's network rules.
type networkRule struct {
	host    string
	allowed bool
	service string
}

// showProfileNetwork lists a profile's network rules: a allows a host, x
// denies one, d takes a rule back to the runtime's policy. Changes reach
// the running sandboxes at once.
func (a *App) showProfileNetwork(pr unibox.Profile) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var network unibox.Network
	a.loadThen("Reading the network rules of "+pr.Name, func(func(string)) (string, error) {
		var err error
		network, err = c.Network(pr.Name)
		return "", err
	}, func(string) {
		var items []pickItem
		for _, h := range network.Allow {
			items = append(items, pickItem{Label: h, Sub: "allowed", About: "The sandboxes may reach " + h + ", which the runtime's policy refuses.", Data: networkRule{host: h, allowed: true}})
		}
		for _, h := range network.Deny {
			items = append(items, pickItem{Label: h, Sub: "denied", About: "The sandboxes may not reach " + h + ", which the runtime's policy allows.", Data: networkRule{host: h}})
		}
		for _, h := range sortedKeys(network.Services) {
			items = append(items, pickItem{Label: h, Sub: "service " + network.Services[h], About: "Reached for service " + network.Services[h] + "; Services… changes it.", Data: networkRule{host: h, service: network.Services[h]}})
		}
		ask := func(verb string) func(pickItem) {
			return func(pickItem) {
				a.askHost(verb+" a host for "+pr.Name, func(host string) {
					a.uniboxDo(strings.ToUpper(verb[:1])+verb[1:]+"ing "+host, "network", verb, host, "-p", pr.Name)
				})
			}
		}
		keys := []pickKey{
			{keys: "a", hint: "allow", name: "Allow Host…", about: "Let the profile's sandboxes reach a host the runtime's policy refuses.", run: ask("allow")},
			{keys: "x", hint: "deny", name: "Deny Host…", about: "Refuse the profile's sandboxes a host the runtime's policy allows.", run: ask("deny")},
			{keys: "d", hint: "remove", name: "Remove Rule", about: "Take the host back to the runtime's policy.",
				when: func(it pickItem) bool { return it.Data != nil && it.Data.(networkRule).service == "" },
				run: func(it pickItem) {
					if it.Data == nil {
						return
					}
					rule := it.Data.(networkRule)
					if rule.service != "" {
						a.flash(rule.host + " is service " + rule.service + "'s - remove the service in Services…")
						return
					}
					a.uniboxDo("Removing the rule for "+rule.host, "network", "rm", rule.host, "-p", pr.Name)
				}},
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "the runtime's policy alone", About: "No rules of the profile's own: a allows a host, x denies one."})
		}
		a.showPickerWith("Network of "+pr.Name, items, pickerOptions{pack: true, explain: true, keys: keys}, nil)
	})
}

// askHost asks for a host name.
func (a *App) askHost(title string, then func(string)) {
	a.askText(title, "Host", "", "Add", func(host string) string {
		if host == "" {
			return "enter a host, like jitpack.io"
		}
		return ""
	}, then)
}

// showProfileServices lists a profile's services: a adds one - unibox asks
// for its token in this terminal - and d takes one out.
func (a *App) showProfileServices(pr unibox.Profile) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var services []unibox.Service
	a.loadThen("Reading the services of "+pr.Name, func(func(string)) (string, error) {
		var err error
		services, err = c.Services(pr.Name)
		return "", err
	}, func(string) {
		var items []pickItem
		for _, s := range services {
			sub := strings.Join(s.Hosts, ", ") + " · token " + s.Token
			about := "Signs the sandboxes in to " + strings.Join(s.Hosts, ", ")
			if s.Git {
				about += " for git"
			}
			if s.Author != "" {
				about += "; commits as " + s.Author
			}
			items = append(items, pickItem{Label: s.Name, Sub: sub, About: about + ".", Data: s})
		}
		keys := []pickKey{
			{keys: "a", hint: "add", name: "Add Service…", about: "Sign the sandboxes in to a forge or a service; unibox asks for its token in this terminal.",
				run: func(pickItem) {
					a.askServiceName(func(name string) {
						a.uniboxSignIn("Adding "+name, "service", "add", name, "-p", pr.Name)
					})
				}},
			{keys: "d", hint: "remove", name: "Remove Service", about: "Take the service out of the profile, and its token unless another profile names it.",
				when: func(it pickItem) bool { return it.Data != nil },
				run: func(it pickItem) {
					if it.Data == nil {
						return
					}
					s := it.Data.(unibox.Service)
					a.confirmWith("Remove service", "Take "+s.Name+" out of "+pr.Name+"?", "Remove", nil, func() {
						a.closeModal(pageConfirm)
						a.uniboxDo("Removing "+s.Name, "service", "rm", s.Name, "-p", pr.Name, "--yes")
					})
				}},
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "no services yet", About: "a signs the sandboxes in to a forge, so their agents can push."})
		}
		a.showPickerWith("Services of "+pr.Name, items, pickerOptions{pack: true, explain: true, keys: keys}, nil)
	})
}

// askServiceName asks what a new service is called: github, gitlab and
// artifactory unibox knows by the name.
func (a *App) askServiceName(then func(string)) {
	a.askText("Add a service", "Service", "", "Add", func(name string) string {
		if name == "" {
			return "enter a name: github, gitlab, artifactory, or one of your own"
		}
		return ""
	}, then)
}

// ----------------------------------------------------------- what is read

// showInstructions shows the instructions a sandbox's or a profile's
// agents get.
func (a *App) showInstructions(sandbox, profile string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	name := firstOf(sandbox, profile)
	var text string
	a.loadThen("Reading the instructions of "+name, func(func(string)) (string, error) {
		var err error
		text, err = c.Instructions(sandbox, profile)
		return "", err
	}, func(string) {
		if strings.TrimSpace(text) == "" {
			a.note(name + " gives its agents no instructions")
			return
		}
		a.showSandboxText("Instructions · "+name, esc(text))
	})
}

// showProfileDiff shows what changed in a profile since its last build.
func (a *App) showProfileDiff(pr unibox.Profile) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var diff string
	a.loadThen("Reading what changed in "+pr.Name, func(func(string)) (string, error) {
		var err error
		diff, err = c.Diff(pr.Name)
		return "", err
	}, func(string) {
		if strings.TrimSpace(diff) == "" {
			a.note(pr.Name + " has not changed since its last build")
			return
		}
		a.showSandboxText("Changes since the build · "+pr.Name, diffMarkup(diff))
	})
}

// diffMarkup colours a unified diff line by line.
func diffMarkup(diff string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		colour := colText
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			colour = colMuted
		case strings.HasPrefix(line, "+"):
			colour = role("diff.added")
		case strings.HasPrefix(line, "-"):
			colour = role("diff.deleted")
		case strings.HasPrefix(line, "@@"):
			colour = role("diff.hunk")
		}
		b.WriteString(tag(colour) + esc(line) + tagEnd + "\n")
	}
	return b.String()
}

// showSandboxText shows text to read in front of the screen.
func (a *App) showSandboxText(title, markup string) {
	view := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetScrollable(true)
	view.SetTextColor(colText)
	view.SetText(markup)
	box(view.Box, esc(title)).SetBorderPadding(0, 0, 1, 1)
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if halfPage(view, ev) {
			return nil
		}
		if ev.Key() == tcell.KeyEsc || ev.Rune() == 'q' || ev.Rune() == 'h' {
			a.closeModal(pageCommit)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageCommit, modalPct(view, 80, 85), true, true)
	a.tv.SetFocus(view)
}

// ---------------------------------------------------- the repository

// openProfileDir opens a profile's directory of the environments
// repository in the default editor, or asks which when ask.
func (a *App) openProfileDir(pr unibox.Profile, ask bool) {
	env := a.ubx.overview.Environments
	if env == "" {
		a.flash("no environments repository yet - choose one with Set Environments Repository… (:)")
		return
	}
	dir := filepath.Join(env, "sandboxes", pr.Name)
	what := session.Record{Project: "sandboxes/" + pr.Name, Mode: session.ModeRepository}
	a.withEditor(ask, func(ed *editors.Editor) { a.openNow(dir, what, ed) })
}

func (a *App) openEnvironments() {
	env := a.ubx.overview.Environments
	if env == "" {
		a.flash("no environments repository yet - choose one with Set Environments Repository… (:)")
		return
	}
	a.withEditor(false, func(ed *editors.Editor) {
		a.openNow(env, session.Record{Project: filepath.Base(env), Mode: session.ModeRepository}, ed)
	})
}

// chooseEnvironments picks which clone holds the profiles.
func (a *App) chooseEnvironments() {
	var items []pickItem
	for _, s := range a.taskSources() {
		if len(s.dirs) != 1 {
			continue
		}
		about := "Keep the profiles in " + tildePath(s.dirs[0]) + "; unibox init lays it out where it is not yet."
		items = append(items, pickItem{Label: s.label, Sub: tildePath(s.dirs[0]), About: about, Data: s.dirs[0]})
	}
	if len(items) == 0 {
		a.flash("no clone on disk to keep the profiles in - clone one first, or run unibox init --dir <path>")
		return
	}
	a.showPickerWith("Environments repository", items, pickerOptions{explain: true}, func(it pickItem) {
		a.uniboxDo("Setting the environments repository", "config", "set", "environments", it.Data.(string))
	})
}
