package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
// takes the terminal - an agent, a shell, sbx asking to approve an env -
// runs where the user picks, as an agent opened from a worktree does: this
// terminal, or a tab, split or window of Zellij, herdr or Ghostty. What
// reads or changes something runs behind the interface and says what
// unibox said.

// sandboxRowActions are what can be done with a task, a sandbox's card or
// an env.
func (a *App) sandboxRowActions(r sandboxRow) []uiAction {
	detail := uiAction{name: "Show Details", about: "Move into the detail: the whole configuration, as unibox reports it.",
		keys: "Enter", rank: 5, run: func() { a.sandboxes.openChosenDetail() }}
	switch r.kind {
	case rowTask:
		return append(a.taskActions(r), detail)
	case rowSandbox:
		return append(a.sandboxActions(r), detail)
	}
	return append(a.envActions(r), detail)
}

// rowAgents are the agents a row's sandbox can run, the kit's own first;
// an env whose sandbox is not made yet has its kit's alone.
func rowAgents(r sandboxRow) []string {
	if r.made {
		if ids := r.sandbox.AgentIDs(); len(ids) > 0 {
			return ids
		}
	}
	if r.env.Agent != "" {
		return []string{r.env.Agent}
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
	mounted := func() bool { return len(t.HostPaths()) > 0 }
	acts := []uiAction{
		{name: "Resume…", about: "Run the task's own agent again, " + agentLabel(t.Agent) + ", on its last conversation: here, or in a tab, split or window, the usual place first. One at work is gone to.",
			keys: "o", rank: 10, run: func() { a.resumeTask(r, t.Agent, false) }},
		{name: "Resume with Agent…", about: "Run the task with another agent its sandbox has: which first, then where.",
			keys: "a", rank: 12, when: func() bool { return len(rowAgents(r)) > 1 }, run: func() { a.pickTaskAgent(r) }},
		{name: "Start Afresh…", about: "Run the task's agent on a new conversation rather than its last one: here, or in a tab, split or window.",
			rank: 14, aliases: []string{"new conversation"}, run: func() { a.resumeTask(r, t.Agent, true) }},
		{name: "Open Shell in Task…", about: "A shell in the task's directory in the sandbox, beside its agent; it ends when you leave it.",
			keys: "s", rank: 20, run: func() { a.uniboxShell("Shell in "+t.Name(), "resume", t.ID, "--shell") }},
		{name: "Show Instructions", about: "What the task's agent reads when a session starts - the AGENTS.md unibox writes and the repositories' own - file by file.",
			keys: "i", rank: 30, run: func() { a.showInstructions(t) }},
		{name: "Add Repository…", about: "Give the task one more repository of yours - mounted, or cloned when it is a worktree - even while its agent runs.",
			keys: "+", rank: 35, run: func() { a.addTaskRepository(r) }},
		{name: "Open Checkout", about: "Open the checkout mounted into the task in the default editor: the agent's work is there on the host as it writes it.",
			keys: "O", rank: 38, when: mounted, run: func() { a.openTaskCheckout(t, false) }},
		{name: "Rename Task…", about: "Give the task a name taken wherever its ID is; an empty name takes the one it has away.",
			keys: "e", rank: 40, run: func() { a.renameTask(r) }},
		{name: "Copy Resume Command", about: "Put unibox resume with the task on the clipboard, to go on with it from another terminal.",
			keys: "y", rank: 45, run: func() { a.yank("Resume command", "unibox resume "+t.Name()) }},
		{name: "Open Repository in Browser", about: "The forge's page of the task's first repository, where what its agent pushes arrives.",
			keys: "w", rank: 46, when: func() bool { return len(t.Repositories) > 0 && t.Repositories[0].URL != "" },
			run: func() { a.openWeb(webOfClone(t.Repositories[0].URL)) }},
		{name: "Remove Task…", about: "Take the task out of the sandbox; a mounted checkout is unmounted, never deleted. unibox refuses while it has a session or work not pushed.",
			keys: "d", rank: 90, run: func() { a.removeTask(r) }},
	}
	return append(acts, a.taskAgentActions(r)...)
}

// webOfClone is a forge's page of a repository from its clone URL.
func webOfClone(url string) string {
	url = strings.TrimSuffix(url, ".git")
	if rest, ok := strings.CutPrefix(url, "git@"); ok {
		url = "https://" + strings.Replace(rest, ":", "/", 1)
	}
	return url
}

// taskAgentActions are the second and third tiers of resuming, as opening
// has them: "Resume with <agent>…" for each agent of the sandbox, asking
// where, and - found only by typing - each agent in each place at once.
func (a *App) taskAgentActions(r sandboxRow) []uiAction {
	var acts []uiAction
	for _, id := range rowAgents(r) {
		ag := agentFor(id)
		aliases := append([]string{}, agentAliases[id]...)
		acts = append(acts, uiAction{
			name:  "Resume with " + ag.Name + "…",
			about: "Run " + ag.Name + " in the task, in the sandbox: here, or in a tab, split or window, the usual place first.",
			rank:  600, icon: agentIcons[id], aliases: aliases,
			run: func() { a.resumeTask(r, id, false) },
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
					a.resumeTaskIn(r, id, false, place)
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
		{name: "New Task…", about: "Put a checkout of yours into this sandbox - mounted, or cloned when it is a worktree - and start an agent on it: the agent, then where.",
			keys: "n", rank: 10, run: func() { a.newSandboxTask(r) }},
		{name: "Open Shell…", about: "A shell in the sandbox: here, or in a tab, split or window. It counts as a session while it is open.",
			keys: "s", rank: 20, run: func() { a.uniboxShell("Shell in "+s.Name, "sandbox", "shell", s.Name) }},
		{name: "Ports…", about: "The sandbox's ports, the team's and yours: switch one on or off while it runs, open one in the browser, say whether it is on at each start.",
			keys: "p", rank: 30, run: func() { a.showSandboxPorts(r) }},
		{name: "Network…", about: "The hosts the sandbox may reach beyond its env's, and those it was refused lately: allow, deny, take a rule back.",
			keys: "f", rank: 32, aliases: []string{"firewall", "allow", "deny"}, run: func() { a.showSandboxNetwork(r) }},
		{name: "Resources…", about: "The CPUs and memory of this sandbox alone, over the env's; they take effect when it is next created.",
			keys: "M", rank: 34, aliases: []string{"cpu", "memory"}, run: func() { a.askResources(r) }},
		{name: "Stop Sandbox", about: "Stop the VM; its disk and its tasks stay, and the next session starts it again. unibox refuses while a session runs.",
			keys: "x", rank: 50, when: running, run: func() { a.uniboxDo("Stopping "+s.Name, "sandbox", "stop", s.Name) }},
		{name: "Use as unibox Context", about: "Make this the sandbox unibox commands - and Sandbox with… across unagit - work in when they are not told.",
			keys: "u", rank: 60, when: func() bool { return a.ubx.overview.Context.Set.Sandbox != s.Name },
			run: func() { a.uniboxDo("Using "+s.Name, "context", "use", "-s", s.Name) }},
		{name: "Recreate Sandbox…", about: "Make the sandbox again from its env as it is now, keeping its tasks: mounts made again, clones cloned again. sbx asks in the terminal.",
			keys: "C", rank: 70, when: func() bool { return !s.Adopted }, run: func() { a.recreateSandbox(r) }},
		{name: "Copy Inspect Command", about: "Put unibox sandbox inspect with the sandbox on the clipboard: everything unibox keeps of it.",
			rank: 80, run: func() { a.yank("Inspect command", "unibox sandbox inspect "+s.Name) }},
		{name: "Remove Sandbox…", about: "Remove the VM and every task in it; mounted checkouts are unmounted, never deleted. unibox refuses while it has a session or work not pushed.",
			keys: "d", rank: 90, run: func() { a.removeSandbox(r) }},
	}
}

func (a *App) envActions(r sandboxRow) []uiAction {
	e := r.env
	loads := func() bool { return e.Error == "" }
	notMade := func() bool { return !r.made && e.Error == "" }
	ctx := a.ubx.overview.Context
	return []uiAction{
		{name: "New Task…", about: "Put a checkout of yours into this env's sandbox - made on the way when it is not yet - and start an agent on it.",
			keys: "n", rank: 10, when: loads, run: func() { a.newSandboxTask(r) }},
		{name: "Create Sandbox…", about: "Create the env's sandbox, " + r.sandboxName() + ", without a task in it: in this terminal, where sbx asks to approve the env and unibox for a value it lacks.",
			keys: "c", rank: 15, when: notMade, run: func() { a.uniboxHere("Creating "+r.sandboxName(), "sandbox", "create", "-e", e.Name) }},
		{name: "Values…", about: "What the env asks of you - a login, a name - and what you gave: set, change, remove, for this env, its repository's or every env.",
			keys: "V", rank: 20, when: loads, run: func() { a.showValues(e) }},
		{name: "Tokens…", about: "The tokens the env's sandboxes use and whether sbx has each: store one in this terminal, keep it in unagit, remove it. No value is ever shown.",
			keys: "K", rank: 22, when: loads, aliases: []string{"secrets", "sign in"}, run: func() { a.showSecrets(e) }},
		{name: "Repository Index…", about: "The repositories the env's tasks are given by name - the team's and yours: add one, remove yours, say whether a new task takes it.",
			keys: "i", rank: 24, when: loads, aliases: []string{"repos"}, run: func() { a.showRepoIndex(e) }},
		{name: "Use as unibox Context", about: "Make this env's sandbox the one unibox commands - and Sandbox with… across unagit - work in when they are not told.",
			keys: "u", rank: 60, when: func() bool { return ctx.Set.Env != e.Name && ctx.Set.Sandbox != r.sandboxName() },
			run: func() {
				if r.made {
					a.uniboxDo("Using "+r.sandboxName(), "context", "use", "-s", r.sandboxName())
					return
				}
				a.uniboxDo("Using "+e.Name, "context", "use", "-e", e.Name)
			}},
		{name: "Make Default Env", about: "Make this the env a sandbox is created from when none is named.",
			rank: 62, when: func() bool { return a.ubx.overview.Default != e.Name },
			run: func() { a.uniboxDo("Making "+e.Name+" the default", "config", "set", "default_env", e.Name) }},
		{name: "Open Env Files", about: "Open the team's repository the env's sbxenv files are in, in the default editor.",
			keys: "O", rank: 40, when: func() bool { return len(e.Files) > 0 }, run: func() { a.openEnvFiles(e, false) }},
		{name: "Update Env Files", about: "Fast-forward the repository the env's files are in - unibox's clone, or yours while nothing in it is uncommitted - and say what that changes.",
			rank: 42, aliases: []string{"pull", "env update"}, run: func() { a.uniboxStream("Updating "+e.Name, "env", "update", e.Name) }},
		{name: "Forget Env…", about: "Take the env out of unibox's list; its files and its sandbox stay.",
			rank: 95, run: func() {
				a.confirmWith("Forget env", "Forget env "+e.Name+"?\n\nIts files stay where they are, and so does its sandbox.", "Forget", nil, func() {
					a.closeModal(pageConfirm)
					a.uniboxDo("Forgetting "+e.Name, "env", "rm", e.Name)
				})
			}},
	}
}

// sandboxesActions are what the screen itself can do, from any panel.
func (a *App) sandboxesActions(p *pane) []uiAction {
	v := a.sandboxes
	return append([]uiAction{
		a.setupAction(),
		{name: "Refresh Sandboxes", about: "Read the sandboxes, their tasks, the envs and the environment from unibox again.", keys: "r", rank: 30, run: a.refreshSandboxes},
		{name: "Go to Sandbox Cards", about: "Move to the cards of the sandboxes: h and l between them, their actions on each.", keys: "B", rank: 40,
			run: func() { v.focus(v.cards) }},
		{name: "Go to Tasks", about: "Move to the tasks of every sandbox, where agents are resumed.", keys: "t", rank: 41, run: func() { v.focus(p.table) }},
		{name: "Go to Envs", about: "Move to the envs: the team's sbxenv files, each one's sandbox, values and tokens.", keys: "N", rank: 42,
			when: func() bool { return v.lowerH > 0 }, run: func() { v.focus(v.envs) }},
		{name: "Go to Log", about: "Move to the log of what was asked of unibox and what it said.", keys: "L", rank: 43,
			when: func() bool { return v.lowerH > 0 }, run: func() { v.focus(v.log) }},
		{name: "Go to Environment", about: "Move to the environment: the context, unibox's settings, the tokens sbx keeps.", keys: "E", rank: 44,
			run: func() { v.focus(v.env) }},
		{name: "Run unibox Doctor", about: "Check sbx, every env, your values and tokens and the sandboxes, each with the command that fixes it; Enter runs that command.",
			keys: "D", rank: 45, aliases: []string{"check", "diagnose"}, run: a.showDoctor},
		{name: "Sandbox Tokens in unagit…", about: "The tokens unagit keeps for the sandboxes and hands to sbx while it runs unlocked: replace one, remove it, copy its command.",
			keys: "T", rank: 50, run: a.showCredentials},
		{name: "unibox Settings…", about: "unibox's own configuration: the default env, whether an idle sandbox stops, the SSH agent, editors over SSH and sbx's switches.",
			keys: "U", rank: 52, run: a.showUniboxSettings},
		{name: "Update Env Files", about: "Fast-forward every repository of envs unibox cloned, and say what that changes.",
			rank: 82, aliases: []string{"pull", "env update"}, run: func() { a.uniboxStream("Updating the envs", "env", "update") }},
		{name: "Prune unibox…", about: "Forget the sandboxes sbx no longer has, and unmount what no task uses.",
			rank: 85, run: func() {
				a.confirmWith("Prune", "Forget the sandboxes sbx no longer has and unmount what no task uses?", "Prune", nil, func() {
					a.closeModal(pageConfirm)
					a.uniboxDo("Pruning", "prune")
				})
			}},
		{name: "Install herdr Plugin", about: "Have herdr show the state of sandboxed agents - unibox's plugin, which watches the panes unibox runs agents in.",
			rank: 88, aliases: []string{"herdr"}, when: func() bool { _, err := a.executable("herdr"); return err == nil },
			run: func() { a.uniboxDo("Installing the herdr plugin", "herdr", "install-plugin", "--yes") }},
		{name: "Uninstall herdr Plugin", about: "Take unibox's plugin out of herdr, and its background observer with it.",
			rank: 89, aliases: []string{"herdr"}, when: func() bool { _, err := a.executable("herdr"); return err == nil },
			run: func() { a.uniboxDo("Uninstalling the herdr plugin", "herdr", "uninstall-plugin", "--yes") }},
	}, a.listActions(p)...)
}

// uniboxDo runs a command that changes something, behind the interface,
// and says what unibox said it did. A refusal is a warning: unibox keeps
// work it would lose, and says how to go on.
func (a *App) uniboxDo(title string, args ...string) {
	a.uniboxDoThen(title, nil, args...)
}

// uniboxDoThen is uniboxDo, with then run on the loop once it worked.
func (a *App) uniboxDoThen(title string, then func(), args ...string) {
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
			if err == nil && then != nil {
				then()
			}
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
				said = unibox.StripANSI(lines[len(lines)-1])
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
func (a *App) resumeTask(r sandboxRow, agent string, fresh bool) {
	if agent == "" {
		agent = r.task.Agent
	}
	if agent == r.task.Agent && r.working() && !fresh {
		if rec, ok := a.taskSession(r.task); ok {
			row := agentRow{Kind: rec.Editor, Dir: rec.Dir, Pane: rec.Pane, Record: &rec}
			a.goToAgent(row)
			return
		}
	}
	ag := agentFor(agent)
	title := "Resume " + r.task.Name() + " with " + ag.Name + " · where"
	if fresh {
		title = "Start " + r.task.Name() + " afresh with " + ag.Name + " · where"
	}
	a.pickPlace(placeOfAgent, title, a.agentPlaces(ag), func(place editorPlace) {
		a.resumeTaskIn(r, agent, fresh, place)
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

func (a *App) resumeTaskIn(r sandboxRow, agent string, fresh bool, place editorPlace) {
	args := []string{"resume", r.task.ID}
	if agent != "" && agent != r.task.Agent {
		args = append(args, "--agent", agent)
	}
	if fresh {
		args = append(args, "--new")
	}
	a.runSandboxAgent(a.taskRecord(r.task, agent), place, args)
}

// taskRecord is what is written down of an agent in a task, so Activity
// lists it with its sandbox. A task on a mounted checkout is that
// checkout's, so the lists find it there.
func (a *App) taskRecord(t unibox.Task, agent string) session.Record {
	branch := ""
	if len(t.Repositories) > 0 {
		branch = t.Repositories[0].Branch
	}
	dir := paneDir()
	if paths := t.HostPaths(); len(paths) > 0 {
		if info, err := os.Stat(paths[0]); err == nil && info.IsDir() {
			dir = paths[0]
		}
	}
	return session.Record{Project: t.Sandbox + "/" + t.Name(), Title: branch, Branch: branch, Mode: session.ModeBranch,
		Dir: dir, Editor: agent, Sandbox: t.Sandbox, Task: t.ID}
}

// runSandboxAgent runs a unibox command that runs an agent, in a place:
// here, suspending unagit until it ends, or in a pane, written down there.
func (a *App) runSandboxAgent(what session.Record, place editorPlace, args []string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	cmd := c.CommandHolding(args...)
	cmd.Dir = what.Dir
	name := agentLabel(what.Editor)
	a.logTerminal(name+" in "+what.Label(), place, args)
	go func() {
		what.Launcher = c.Binary
		if place.client == nil {
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
		cmd := c.CommandHolding(args...)
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

// uniboxHere runs a unibox command that asks in this terminal - for a
// token, which unibox hands to sbx and unagit never sees, or for sbx's
// approval of an env and its host commands - with unagit suspended.
func (a *App) uniboxHere(title string, args ...string) {
	a.uniboxHereThen(title, nil, args...)
}

func (a *App) uniboxHereThen(title string, then func(), args ...string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	cmd := c.CommandHolding(args...)
	cmd.Dir = paneDir()
	e := a.logUnibox(title, args...)
	go func() {
		err := a.runTerminalEditor(cmd, editors.Editor{Name: "unibox"})
		a.tv.QueueUpdateDraw(func() {
			e.end(a, "in this terminal", err, false)
			if err == nil {
				a.done(strings.ToLower(title[:1]) + title[1:] + " - done")
				if then != nil {
					then()
				}
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

// pickTaskAgent asks which of the sandbox's agents runs the task, then
// where.
func (a *App) pickTaskAgent(r sandboxRow) {
	var items []pickItem
	for _, id := range rowAgents(r) {
		about := "Run " + agentLabel(id) + " in the task."
		if id == r.task.Agent {
			about = "The task's own agent, on its last conversation."
		}
		items = append(items, pickItem{Label: agentLabel(id), About: about, Data: id})
	}
	a.showPickerWith("Resume "+r.task.Name()+" with", items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		a.resumeTask(r, it.Data.(string), false)
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
	body := fmt.Sprintf("Remove task %s from %s?\n\nA mounted checkout is unmounted and stays on disk; what was pushed stays on the forge. unibox refuses while anything is not pushed.", t.Name(), t.Sandbox)
	a.confirmWith("Remove task", body, "Remove", warnings, func() {
		a.closeModal(pageConfirm)
		a.uniboxDo("Removing "+t.Name(), "task", "rm", t.ID, "--yes")
	})
}

func (a *App) removeSandbox(r sandboxRow) {
	s := r.sandbox
	body := fmt.Sprintf("Remove sandbox %s and its disk?\n\nMounted checkouts are unmounted and stay on disk.", s.Name)
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
		warnings = append(warnings, "a session runs in it - unibox refuses until it ends")
	}
	if s.Adopted {
		warnings = append(warnings, "it was made with sbx: unibox removes it only when forced")
	}
	a.confirmChoices("Remove sandbox", body, warnings, []choice{
		{"Remove", func() {
			a.closeModal(pageConfirm)
			a.uniboxDo("Removing "+s.Name, "sandbox", "rm", s.Name, "--yes")
		}},
		{"Force Remove", func() {
			a.closeModal(pageConfirm)
			a.uniboxDo("Removing "+s.Name+" by force", "sandbox", "rm", s.Name, "--force", "--yes")
		}},
	})
}

func (a *App) recreateSandbox(r sandboxRow) {
	s := r.sandbox
	body := fmt.Sprintf("Make %s again from env %s as it is now?\n\nIts tasks stay: mounts are made again, clones cloned again; work not pushed makes unibox refuse. sbx asks in this terminal to approve the env.", s.Name, s.Env)
	a.confirmWith("Recreate sandbox", body, "Recreate", nil, func() {
		a.closeModal(pageConfirm)
		a.uniboxHere("Recreating "+s.Name, "sandbox", "recreate", s.Name)
	})
}

// ------------------------------------------------------------ a new task

// taskSource is a directory of unagit's a task can be made from: a clone,
// mounted, or a worktree or a group's members, cloned - unibox cannot
// mount a git worktree, so its repositories are cloned from the forge at
// its branch.
type taskSource struct {
	label, branch string
	dirs          []string
	clone         bool
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
			out = append(out, taskSource{label: w.Path, branch: w.Branch, dirs: dirs, clone: true})
			continue
		}
		out = append(out, taskSource{label: w.Path, branch: w.Branch, dirs: []string{w.Dir}, clone: true})
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

// about says what a task does with the source.
func (s taskSource) about() string {
	if s.clone {
		return "Clone " + strings.Join(tildePaths(s.dirs), ", ") + " in the sandbox from the forge, at " + firstOf(s.branch, "its branch") + "; a worktree is never mounted."
	}
	return "Mount " + strings.Join(tildePaths(s.dirs), ", ") + " into the sandbox: the agent works on your files and .git as they are."
}

// newSandboxTask starts a task from the Sandboxes screen: what to work on,
// the agent, the place - in the row's sandbox, or its env's made on the way.
func (a *App) newSandboxTask(r sandboxRow) {
	sources := a.taskSources()
	if len(sources) == 0 {
		a.flash("nothing is on disk to put into a sandbox - clone a repository or make a worktree first")
		return
	}
	var items []pickItem
	for _, s := range sources {
		sub := s.branch
		if s.clone {
			sub = strings.TrimSpace(sub + " · clone")
		}
		items = append(items, pickItem{Label: s.label, Sub: sub, About: s.about(), Data: s})
	}
	a.showPickerWith("New task in "+r.sandboxName()+" · what to work on", items, pickerOptions{explain: true}, func(it pickItem) {
		src := it.Data.(taskSource)
		start := unibox.Start{Dirs: src.dirs, Clone: src.clone, Sandbox: r.sandboxName()}
		if !r.made {
			start.Env = r.env.Name
		}
		a.pickStartAgent(start, src.label, rowAgents(r))
	})
}

// sandboxTarget is something a sandbox can work on: what it is called, its
// directories, and whether they are cloned rather than mounted.
type sandboxTarget struct {
	label string
	dirs  []string
	clone bool
}

// openInSandboxActions are the sandbox's actions for a row of the lists:
// "Open in Sandbox…" asking everything, "Sandbox with <agent>…" asking
// which sandbox only when more than one has the agent, the same in the
// context's sandbox asking only where, and - found only by typing - each
// agent in each place at once. ok says whether it is on disk.
func (a *App) openInSandboxActions(target func() sandboxTarget, ok func() bool) []uiAction {
	can := func() bool { return a.uniboxOn() && (ok == nil || ok()) }
	acts := []uiAction{{name: "Open in Sandbox…", about: "Start an agent on it in a unibox sandbox - a checkout mounted, a worktree cloned at its branch: the sandbox, the agent, the place.",
		rank: 620, aliases: []string{"unibox", "sandbox", "vm"}, when: can,
		run: func() { a.startInSandbox(target()) }}}
	if !a.uniboxOn() {
		return acts
	}
	ctxBox, inContext := a.contextSandbox()
	for _, id := range a.sandboxAgentIDs() {
		ag := agentFor(id)
		aliases := append([]string{"sandbox", "unibox"}, agentAliases[id]...)
		acts = append(acts, uiAction{
			name:  "Sandbox with " + ag.Name + "…",
			about: "Start " + ag.Name + " on it in a sandbox - asked which only when more than one has it, the most used first - then where: here, or in a tab, split or window.",
			rank:  640, icon: agentIcons[id], aliases: aliases, when: can,
			run: func() {
				a.withSandboxes(func() {
					a.pickSandbox(target().label, id, func(start unibox.Start) {
						a.startTargetWith(target(), start, id, nil)
					})
				})
			},
		})
		if inContext && ctxBox.has(id) {
			acts = append(acts, uiAction{
				name:  "Sandbox with " + ag.Name + " in " + ctxBox.name + "…",
				about: "Start " + ag.Name + " on it in " + ctxBox.name + ", the unibox context: only where is asked.",
				rank:  635, icon: agentIcons[id], aliases: append(append([]string{}, aliases...), "context", ctxBox.name), when: can,
				run: func() { a.startTargetWith(target(), ctxBox.start, id, nil) },
			})
		}
		for _, place := range placesOf(a.agentPlaces(ag)) {
			acts = append(acts, uiAction{
				name:  "Sandbox with " + ag.Name + " in " + placeName(place),
				about: "Start " + ag.Name + " on it in a sandbox - which one first, when more than one has it - in " + placeAbout(place) + ".",
				rank:  720, icon: agentIcons[id], filterOnly: true, prefer: placePrefer(place.where), when: can,
				aliases: append(append([]string{}, aliases...), placeAliases(place.where)...),
				run: func() {
					a.withSandboxes(func() {
						a.pickSandbox(target().label, id, func(start unibox.Start) {
							a.startTargetWith(target(), start, id, &place)
						})
					})
				},
			})
		}
	}
	return acts
}

// startTargetWith starts an agent on a target in a sandbox chosen, asking
// where unless place says.
func (a *App) startTargetWith(t sandboxTarget, start unibox.Start, agent string, place *editorPlace) {
	start.Dirs, start.Clone, start.Agent = t.dirs, t.clone, agent
	if place != nil {
		a.cfg.State.UsePlace(placeOfAgent, placeKeyOf(*place))
		a.saveState()
		a.checkAndStart(start, t.label, *place)
		return
	}
	ag := agentFor(agent)
	a.pickPlace(placeOfAgent, t.label+" with "+ag.Name+" in "+start.Sandbox+" · where", a.agentPlaces(ag), func(place editorPlace) {
		a.checkAndStart(start, t.label, place)
	})
}

// sandboxChoice is a sandbox a task can go into: one unibox has, or the
// sandbox of an env that is to be made, and the agents it runs.
type sandboxChoice struct {
	name, env string
	made      bool
	state     string
	tasks     int
	agents    []string
	start     unibox.Start
}

func (c sandboxChoice) has(agent string) bool { return slices.Contains(c.agents, agent) }

// sandboxChoices are where a task can go: every sandbox, then the sandbox
// of every env that has none yet.
func (a *App) sandboxChoices() []sandboxChoice {
	var out []sandboxChoice
	for _, r := range a.sandboxRows {
		switch {
		case r.kind == rowSandbox:
			out = append(out, sandboxChoice{name: r.sandbox.Name, env: r.sandbox.Env, made: true, state: r.sandbox.State,
				tasks: len(r.sandbox.Tasks), agents: rowAgents(r), start: unibox.Start{Sandbox: r.sandbox.Name}})
		case r.kind == rowEnv && !r.made && r.env.Error == "":
			out = append(out, sandboxChoice{name: r.sandboxName(), env: r.env.Name, agents: rowAgents(r),
				start: unibox.Start{Sandbox: r.sandboxName(), Env: r.env.Name}})
		}
	}
	return out
}

// contextSandbox is the sandbox of unibox's context, when one is chosen:
// the sandbox named, or the one its env makes.
func (a *App) contextSandbox() (sandboxChoice, bool) {
	ctx := a.ubx.overview.Context
	for _, c := range a.sandboxChoices() {
		if ctx.Set.Sandbox != "" && c.name == ctx.Set.Sandbox || ctx.Set.Sandbox == "" && ctx.Set.Env != "" && c.env == ctx.Set.Env {
			return c, true
		}
	}
	return sandboxChoice{}, false
}

// sandboxAgentIDs are the agents a sandbox can run: those of the sandboxes
// and envs once unibox was read, or the ones unibox knows before.
func (a *App) sandboxAgentIDs() []string {
	if !a.ubx.read {
		return []string{"claude", "codex", "copilot"}
	}
	var ids []string
	for _, c := range a.sandboxChoices() {
		for _, id := range c.agents {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// startInSandbox is Open in Sandbox… of a worktree, a group or a clone:
// the sandbox, the agent, the place.
func (a *App) startInSandbox(t sandboxTarget) {
	if !a.uniboxOn() {
		a.flash("unibox is off - turn it on in Settings › Integrations")
		return
	}
	a.withSandboxes(func() {
		a.pickSandbox(t.label, "", func(start unibox.Start) {
			start.Dirs, start.Clone = t.dirs, t.clone
			c, _ := a.choiceNamed(start.Sandbox)
			a.pickStartAgent(start, t.label, c.agents)
		})
	})
}

func (a *App) choiceNamed(name string) (sandboxChoice, bool) {
	for _, c := range a.sandboxChoices() {
		if c.name == name {
			return c, true
		}
	}
	return sandboxChoice{}, false
}

// withSandboxes reads unibox first when it has not been yet, so the
// pickers have the envs and sandboxes to offer.
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
		a.drawTabs()
		then()
	})
}

// sandboxesChosen is what the sandboxes chosen for a task are counted under,
// beside the places things are opened in.
const sandboxesChosen = "sandbox"

// pickSandbox asks which sandbox a task goes into - the context's first,
// then the most used - among those that run agent, or every one. A sole
// choice is taken without asking.
func (a *App) pickSandbox(label, agent string, then func(unibox.Start)) {
	uses := a.cfg.State.PlaceUses[sandboxesChosen]
	ctx, inContext := a.contextSandbox()
	var choices []sandboxChoice
	for _, c := range a.sandboxChoices() {
		if agent == "" || c.has(agent) {
			choices = append(choices, c)
		}
	}
	rank := func(c sandboxChoice) int {
		if inContext && c.name == ctx.name {
			return 0
		}
		if c.made {
			return 1
		}
		return 2
	}
	sort.SliceStable(choices, func(i, j int) bool {
		if rank(choices[i]) != rank(choices[j]) {
			return rank(choices[i]) < rank(choices[j])
		}
		return uses[choices[i].name] > uses[choices[j].name]
	})
	if len(choices) == 0 {
		if agent != "" {
			a.flash("no sandbox or env runs " + agentLabel(agent) + " - an env's kit names its agent")
			return
		}
		a.flash("no env to make a sandbox from - I sets unibox up from your team's repository")
		return
	}
	pick := func(c sandboxChoice) {
		a.cfg.State.UsePlace(sandboxesChosen, c.name)
		a.saveState()
		then(c.start)
	}
	if len(choices) == 1 {
		pick(choices[0])
		return
	}
	var items []pickItem
	for _, c := range choices {
		var names []string
		for _, id := range c.agents {
			names = append(names, agentLabel(id))
		}
		sub := c.env + " · " + firstOf(c.state, "not made")
		about := fmt.Sprintf("Env %s, %s; agents %s. Its tasks share one VM.", c.env, counted(c.tasks, "task", "tasks"), strings.Join(names, ", "))
		if !c.made {
			about = "The sandbox of env " + c.env + ", made on the way: sbx asks in the terminal to approve the env."
		}
		if inContext && c.name == ctx.name {
			sub += " · context"
		}
		items = append(items, pickItem{Label: c.name, Sub: sub, About: about, Data: c})
	}
	title := label + " · which sandbox"
	if agent != "" {
		title = label + " with " + agentLabel(agent) + " · which sandbox"
	}
	a.showPickerWith(title, items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		pick(it.Data.(sandboxChoice))
	})
}

// pickStartAgent asks which of the sandbox's agents works on the task,
// skipped when it has one.
func (a *App) pickStartAgent(start unibox.Start, label string, ids []string) {
	pick := func(id string) {
		start.Agent = id
		ag := agentFor(id)
		a.pickPlace(placeOfAgent, label+" with "+ag.Name+" · where", a.agentPlaces(ag), func(place editorPlace) {
			a.checkAndStart(start, label, place)
		})
	}
	switch len(ids) {
	case 0:
		a.flash(start.Sandbox + " names no agent - its env's kit starts one")
		return
	case 1:
		pick(ids[0])
		return
	}
	var items []pickItem
	for _, id := range ids {
		items = append(items, pickItem{Label: agentLabel(id), About: "Run " + agentLabel(id) + " on the task, in the sandbox.", Data: id})
	}
	a.showPickerWith(label+" · which agent", items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		pick(it.Data.(string))
	})
}

// startAskable are the needs unibox run asks about on the terminal - a
// value missing, local changes, a branch to switch - so a start is still
// offered with them.
var startAskable = []string{"arg_missing", "uncommitted", "local_commits", "branch_switch"}

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
			a.runSandboxAgent(what, place, start.RunArgs())
		}
		var lines, warnings []string
		// unibox run asks on the terminal about a value and about local
		// changes, pushes only with --push, and refuses anything else: a
		// start it would refuse is not offered, or it only flashes its
		// reason past and leaves an exit status behind.
		unpushed, refused := false, false
		for _, need := range check.Needs {
			text := need.Message
			if need.Repository != "" && !strings.Contains(text, need.Repository) {
				text = need.Repository + ": " + text
			}
			if need.Warning {
				if need.Code != "remote_check_pending" {
					warnings = append(warnings, text)
				}
				continue
			}
			switch {
			case need.Code == "branch_not_pushed" || need.Code == "branch_missing":
				unpushed = true
			case slices.Contains(startAskable, need.Code):
			default:
				refused = true
			}
			lines = append(lines, text)
		}
		if check.Ready && len(warnings) == 0 {
			run(false)
			return
		}
		body := "Start " + agentLabel(start.Agent) + " on " + label + " in " + start.Sandbox + "?"
		if refused {
			body = agentLabel(start.Agent) + " cannot start on " + label + " in " + start.Sandbox + " yet."
		}
		if len(lines) > 0 {
			body += "\n\nunibox says:\n"
			for _, l := range lines {
				body += "  " + glyphDot + " " + esc(l) + "\n"
			}
			switch {
			case refused:
				body += "\nunibox run refuses to start until that is done."
			case unpushed:
				body += "\nunibox run starts once the branch is on the forge; it asks on the terminal about the rest."
			default:
				body += "\nunibox run asks about it on the terminal."
			}
		}
		var choices []choice
		switch {
		case refused:
		case unpushed:
			choices = append(choices, choice{"Push and Start", func() { a.closeModal(pageConfirm); run(true) }})
		default:
			label := "Start"
			if len(lines) > 0 {
				label = "Start Anyway"
			}
			choices = append(choices, choice{label, func() { a.closeModal(pageConfirm); run(false) }})
		}
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

// addTaskRepository picks a checkout of the user's and adds it to a task.
func (a *App) addTaskRepository(r sandboxRow) {
	var items []pickItem
	for _, s := range a.taskSources() {
		if len(s.dirs) != 1 {
			continue
		}
		items = append(items, pickItem{Label: s.label, Sub: s.branch, About: "Add " + tildePath(s.dirs[0]) + " to " + r.task.Name() + ", as the task has its others.", Data: s.dirs[0]})
	}
	if len(items) == 0 {
		a.flash("nothing is on disk to add - clone a repository first")
		return
	}
	a.showPickerWith("Add to "+r.task.Name(), items, pickerOptions{explain: true}, func(it pickItem) {
		a.uniboxDo("Adding to "+r.task.Name(), "task", "add-repo", r.task.ID, "--repo", it.Data.(string))
	})
}

// ------------------------------------------------------- lists in dialogs

// showSandboxPorts lists a sandbox's ports: Enter switches one on or off
// while it runs, w opens a published one, t says whether it is on at each
// start.
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
			a.note(s.Name + " has no ports - the team names them beside its sbxenv file, you under ports in unibox's config.yaml")
			return
		}
		var items []pickItem
		for _, p := range ports {
			state := "off"
			if p.On || len(p.Published) > 0 {
				state = "on"
			}
			if p.Default {
				state += " · at start"
			}
			items = append(items, pickItem{Label: p.Name, Sub: p.Port + " · " + state, About: portLine(p), Data: p})
		}
		browse := a.browserKey("w", "browser", "Open in Browser", "Open the port where it is published on this machine; the list stays open.",
			func(it pickItem) string {
				p := it.Data.(unibox.Port)
				if len(p.Published) == 0 {
					return ""
				}
				return p.Published[0].URL()
			})
		browse.when = func(it pickItem) bool { return len(it.Data.(unibox.Port).Published) > 0 }
		atStart := pickKey{keys: "t", hint: "at start", name: "Switch On at Start", about: "Say, for you, whether the port is published at each start of the env's sandboxes.",
			run: func(it pickItem) {
				p := it.Data.(unibox.Port)
				to := "on"
				if p.Default {
					to = "off"
				}
				a.uniboxDo("Port "+p.Name+" "+to+" at start", "port", "default", p.Name, to, "-e", s.Env)
			}}
		a.showPickerWith("Ports of "+s.Name, items, pickerOptions{pack: true, explain: true, keys: []pickKey{browse, atStart},
			enterName: "Switch On or Off", enterAbout: "Publish the port while the sandbox runs, or take it back; what the files say stays as it is."},
			func(it pickItem) {
				p := it.Data.(unibox.Port)
				if p.On || len(p.Published) > 0 {
					a.uniboxDo("Taking "+p.Name+" back", "port", "off", p.Name, "-s", s.Name)
					return
				}
				a.uniboxDo("Publishing "+p.Name, "port", "on", p.Name, "-s", s.Name)
			})
	})
}

// showSandboxNetwork lists a sandbox's network rules and the hosts it was
// refused lately: a allows a host, x denies one, d takes a rule of yours
// back, Enter on a refused host allows it.
func (a *App) showSandboxNetwork(r sandboxRow) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	s := r.sandbox
	var network unibox.Network
	a.loadThen("Reading the network of "+s.Name, func(func(string)) (string, error) {
		var err error
		network, err = c.Network(s.Name)
		return "", err
	}, func(string) {
		var items []pickItem
		for _, rule := range network.Rules {
			ink := "sandboxes.allowed"
			if rule.Decision != "allow" {
				ink = "sandboxes.denied"
			}
			whose := "every sandbox"
			if rule.Sandbox != "" {
				whose = rule.Sandbox
			}
			items = append(items, pickItem{Label: rule.Host, Sub: rule.Decision + " · " + rule.From, SubRole: ink,
				About: fmt.Sprintf("%s %s for %s, a rule of %s's.", strings.ToUpper(rule.Decision[:1])+rule.Decision[1:], rule.Host, whose, rule.From), Data: rule})
		}
		for _, b := range network.Blocked {
			items = append(items, pickItem{Label: b.Host, Sub: fmt.Sprintf("refused %d×", b.Count), SubRole: "sandboxes.blocked",
				About: "Refused " + humanAge(b.LastSeen) + ": " + b.Reason + ". Enter allows it for " + s.Name + ".", Data: b})
		}
		ask := func(verb string) func(pickItem) {
			return func(pickItem) {
				a.askHost(strings.ToUpper(verb[:1])+verb[1:]+" a host for "+s.Name, func(host string) {
					a.uniboxDo(strings.ToUpper(verb[:1])+verb[1:]+"ing "+host, "network", verb, host, "-s", s.Name)
				})
			}
		}
		yours := func(it pickItem) bool { rule, ok := it.Data.(unibox.NetworkRule); return ok && rule.From == "yours" }
		keys := []pickKey{
			{keys: "a", hint: "allow", name: "Allow Host…", about: "Let the sandbox reach a host its env does not; unibox keeps the rule.", run: ask("allow")},
			{keys: "x", hint: "deny", name: "Deny Host…", about: "Keep the sandbox from a host, even one its env allows.", run: ask("deny")},
			{keys: "d", hint: "remove", name: "Remove Rule", about: "Take a rule of yours back; the env's and sbx's stay.",
				when: yours, run: func(it pickItem) {
					if !yours(it) {
						a.flash("only a rule of yours is taken back here - the env's belong in its kits")
						return
					}
					rule := it.Data.(unibox.NetworkRule)
					a.uniboxDo("Removing the rule for "+rule.Host, "network", "rm", rule.Host, "-s", s.Name)
				}},
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "the env's policy alone", About: "No rules of the sandbox's own, and nothing refused lately: a allows a host, x denies one."})
		}
		a.showPickerWith("Network of "+s.Name, items, pickerOptions{pack: true, explain: true, keys: keys,
			enterName: "Allow Refused Host", enterAbout: "Let the sandbox reach a host it was refused.", enterHint: "allow"},
			func(it pickItem) {
				if b, ok := it.Data.(unibox.BlockedHost); ok {
					a.uniboxDo("Allowing "+b.Host, "network", "allow", b.Host, "-s", s.Name)
				}
			})
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

// askResources asks for a sandbox's own CPUs and memory; empty takes its
// own away for the env's.
func (a *App) askResources(r sandboxRow) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	s := r.sandbox
	var res unibox.Resources
	a.loadThen("Reading the resources of "+s.Name, func(func(string)) (string, error) {
		var err error
		res, err = c.Resources(s.Name)
		return "", err
	}, func(string) {
		const cpusLabel, memoryLabel = "CPUs", "Memory"
		form := tview.NewForm()
		styleForm(form)
		form.AddInputField(cpusLabel, res.Own.CPUs, 0, nil, nil)
		form.AddInputField(memoryLabel, res.Own.Memory, 0, nil, nil)
		form.AddTextView("", fmt.Sprintf("Now %s CPUs, %s memory. A count or a share,\nas 4 or 50%%; memory as 16g or 75%%. Empty\nfor the env's. Applied at the next recreate.",
			firstOf(res.Resources.CPUs, "the env's"), firstOf(res.Resources.Memory, "the env's")), 0, 3, true, false)
		text := func(label string) string {
			return strings.TrimSpace(form.GetFormItemByLabel(label).(*tview.InputField).GetText())
		}
		form.AddButton("Set", func() {
			cpus, memory := firstOf(text(cpusLabel), "-"), firstOf(text(memoryLabel), "-")
			a.closeModal(pageForm)
			a.uniboxDo("Setting the resources of "+s.Name, "sandbox", "resources", s.Name, "--cpus", cpus, "--memory", memory)
		})
		form.AddButton("Cancel", func() { a.closeModal(pageForm) })
		a.showFormModalSized("Resources of "+s.Name, form, 64, 12)
	})
}

// ----------------------------------------------------------- what is read

// showInstructions shows what a task's agent reads when its session
// starts, file by file.
func (a *App) showInstructions(t unibox.Task) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var in unibox.Instructions
	a.loadThen("Reading the instructions of "+t.Name(), func(func(string)) (string, error) {
		var err error
		in, err = c.Instructions(t.ID)
		return "", err
	}, func(string) {
		if len(in.Files) == 0 {
			a.note(t.Name() + " gives its agent no instructions")
			return
		}
		d := &detailBuf{}
		if !in.Read {
			d.sub("the sandbox is not running: what unibox writes alone")
		}
		for _, f := range in.Files {
			head := f.Path
			if f.Link != "" {
				head += " → " + f.Link
			}
			d.section(head)
			if f.Unibox {
				d.sub("written by unibox before each session")
			}
			if f.Link == "" {
				d.raw(esc(strings.TrimRight(f.Content, "\n")) + "\n")
			}
		}
		a.showSandboxText("Instructions · "+t.Name(), d.String())
	})
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

// showDoctor runs unibox doctor and lists its checks, the failed first:
// Enter runs the unibox command a check names to fix it, in this terminal.
func (a *App) showDoctor() {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var checks []unibox.DoctorCheck
	a.loadThen("Asking unibox's doctor", func(func(string)) (string, error) {
		var err error
		checks, err = c.Doctor()
		return "", err
	}, func(string) {
		rank := map[string]int{"fail": 0, "warn": 1, "pass": 2}
		sort.SliceStable(checks, func(i, j int) bool { return rank[checks[i].Status] < rank[checks[j].Status] })
		var items []pickItem
		failed := 0
		for _, ch := range checks {
			mark, ink := glyphCheck, "sandboxes.running"
			switch ch.Status {
			case "fail":
				mark, ink = glyphCross, "sandboxes.failed"
				failed++
			case "warn":
				mark, ink = glyphRing, "sandboxes.problem"
			}
			about := ch.Message
			if ch.Hint != "" {
				about += " - " + ch.Hint
			}
			items = append(items, pickItem{Label: mark + " " + ch.Area + " · " + ch.Name, Sub: trunc(ch.Message, 48), SubRole: ink, About: about, Data: ch})
		}
		if len(items) == 0 {
			a.note("unibox's doctor checked nothing")
			return
		}
		title := "unibox doctor · " + counted(failed, "failure", "failures")
		a.showPickerWith(title, items, pickerOptions{explain: true,
			enterName: "Run the Fix", enterAbout: "Run the unibox command the check names, in this terminal; the doctor is asked again after.", enterHint: "fix"},
			func(it pickItem) {
				ch := it.Data.(unibox.DoctorCheck)
				args := fixCommand(ch.Hint)
				if args == nil {
					a.note(firstOf(ch.Hint, ch.Message))
					return
				}
				a.uniboxHereThen("Fixing "+ch.Name, a.showDoctor, args...)
			})
	})
}

// fixCommand is the unibox command a hint names, as arguments: the first
// "unibox ..." in it, up to the end of the sentence.
func fixCommand(hint string) []string {
	at := strings.Index(hint, "unibox ")
	if at < 0 {
		return nil
	}
	rest := hint[at+len("unibox "):]
	if end := strings.IndexAny(rest, ";,\n"); end >= 0 {
		rest = rest[:end]
	}
	rest = strings.TrimSuffix(strings.TrimSpace(rest), ".")
	fields := strings.Fields(rest)
	for _, f := range fields {
		if strings.ContainsAny(f, "<>") {
			return nil // a placeholder for the user to fill in
		}
	}
	return fields
}

// ---------------------------------------------------- opening on the host

// openTaskCheckout opens the checkout mounted into a task in an editor.
func (a *App) openTaskCheckout(t unibox.Task, ask bool) {
	paths := t.HostPaths()
	if len(paths) == 0 {
		a.flash(t.Name() + " has no checkout of yours mounted - its repositories are cloned in the sandbox")
		return
	}
	what := session.Record{Project: filepath.Base(paths[0]), Mode: session.ModeRepository}
	a.withEditor(ask, func(ed *editors.Editor) { a.openNow(paths[0], what, ed) })
}

// openEnvFiles opens the team's repository an env's files are in, or the
// directory of its first file.
func (a *App) openEnvFiles(e unibox.Env, ask bool) {
	dir := e.Repo
	if dir == "" && len(e.Files) > 0 {
		dir = filepath.Dir(e.Files[0])
	}
	if dir == "" {
		a.flash("env " + e.Name + " names no files")
		return
	}
	what := session.Record{Project: filepath.Base(dir), Mode: session.ModeRepository}
	a.withEditor(ask, func(ed *editors.Editor) { a.openNow(dir, what, ed) })
}
