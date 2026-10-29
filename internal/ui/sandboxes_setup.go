package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/unibox"
)

// Setting unibox up is a list of what a first sandbox needs, read from
// unibox as it is: unibox itself, the tab, the team's repository of sbxenv
// files and the envs unibox init made of it, and for each env what stands
// between it and a start - a value of the person's, a token sbx lacks, its
// sandbox - then unibox's doctor. Each step says how it stands, and Enter
// does it; the list is opened again with I, and reads everything afresh,
// so it is never a wizard to finish in one go.

// setupStep is one step of setting unibox up.
type setupStep struct {
	name, state, about string
	done               bool
	// run does the step - or does it again, for one done that can be
	// done more than once, such as adding a team's repository; nil when
	// there is nothing to do.
	run func()
}

// showUniboxSetup reads where unibox stands and lists the steps.
func (a *App) showUniboxSetup() {
	if _, err := a.executable("unibox"); err != nil {
		a.showSetupSteps([]setupStep{{
			name: "Install unibox", state: "not installed",
			about: "unagit runs unibox from PATH: brew install unibox, or make install in its repository. Enter copies the brew command; open this again with I once it is installed.",
			run:   func() { a.yank("Install command", "brew install unibox") },
		}})
		return
	}
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var o unibox.Overview
	secrets := map[string][]unibox.Secret{}
	a.loadThen("Reading where unibox stands", func(step func(string)) (string, error) {
		var err error
		if o, err = c.Overview(); err != nil {
			return "", err
		}
		for _, e := range o.Envs {
			if e.Error != "" {
				continue
			}
			step("The tokens of " + e.Name)
			if list, err := c.Secrets(e.Name); err == nil {
				secrets[e.Name] = list
			}
		}
		return "", nil
	}, func(string) {
		a.ubx = uniboxState{overview: o, read: true, at: time.Now(), details: map[string]*sandboxDetail{}}
		a.sandboxRows = sandboxRows(o)
		a.sandboxesPane.reload()
		a.drawTabs()
		a.showSetupSteps(a.setupSteps(o, secrets))
	})
}

// setupSteps are the steps, as unibox stands.
func (a *App) setupSteps(o unibox.Overview, secrets map[string][]unibox.Secret) []setupStep {
	steps := []setupStep{{name: "unibox", state: "installed " + o.Version.Version, done: true,
		about: fmt.Sprintf("unibox %s runs the sandboxes over Docker Sandboxes (sbx); unagit drives it from PATH.", o.Version.Version)}}

	tab := setupStep{name: "Sandboxes tab", state: "on", done: true, about: "The Sandboxes tab [5] lists the sandboxes, their tasks and the envs."}
	if !a.uniboxOn() {
		tab.state, tab.done = "off", false
		tab.about = "The unibox integration is turned off. Enter turns it on, as Settings › Integrations does."
		tab.run = func() {
			on := true
			a.cfg.Integrations.Unibox = &on
			a.saveConfig()
			a.drawTabs()
			a.sandboxes.sync()
			a.showUniboxSetup()
		}
	}
	steps = append(steps, tab)

	var names []string
	for _, e := range o.Envs {
		names = append(names, e.Name)
	}
	envs := setupStep{name: "Envs", state: strings.Join(names, ", "), done: len(names) > 0,
		about: "An env is a team's sbxenv files a sandbox is made from. Enter sets up another team's repository with unibox init, in this terminal.",
		run:   a.chooseTeamRepository}
	if len(names) == 0 {
		envs.state = "none"
		envs.about = "An env is a team's sbxenv files a sandbox is made from. Enter picks your team's repository of them - a clone, or a git address - and walks unibox init through it in this terminal: sbx, the envs, your values, tokens, sandboxes."
	}
	steps = append(steps, envs)

	for _, e := range o.Envs {
		if e.Error != "" {
			steps = append(steps, setupStep{name: "Env " + e.Name, state: "does not load",
				about: e.Error + ". Enter opens its files.", run: func() { a.openEnvFiles(e, false) }})
			continue
		}
		values := setupStep{name: "Values of " + e.Name, state: "set", done: true,
			about: "Everything " + e.Name + " asks of you has a value. Enter lists them.", run: func() { a.showValues(e) }}
		if len(e.Missing) > 0 {
			values.state, values.done = counted(len(e.Missing), "missing", "missing"), false
			values.about = e.Name + " asks you for " + strings.Join(e.Missing, ", ") + "; a run stops without it. Enter sets them."
		}
		steps = append(steps, values)

		var missing []string
		for _, s := range secrets[e.Name] {
			if !s.Stored && !s.Optional {
				missing = append(missing, s.Service)
			}
		}
		tokens := setupStep{name: "Tokens of " + e.Name, state: "stored", done: true,
			about: "sbx has every token " + e.Name + " needs. Enter lists them.", run: func() { a.showSecrets(e) }}
		if len(missing) > 0 {
			tokens.state, tokens.done = counted(len(missing), "missing", "missing"), false
			tokens.about = "sbx lacks the tokens of " + strings.Join(missing, ", ") + ". Enter lists them: unibox asks for each in this terminal, or unagit keeps it."
		}
		steps = append(steps, tokens)

		r := sandboxRow{kind: rowEnv, env: e}
		r.sandbox, r.made = sandboxOfEnv(o, e)
		box := setupStep{name: "Sandbox " + r.sandboxName(), state: r.sandbox.State, done: true,
			about: "The sandbox of " + e.Name + " is made; a task starts in it at once."}
		if !r.made {
			box.state, box.done = "not made", false
			box.about = "The sandbox of " + e.Name + " is made by the first task, or now: Enter creates it in this terminal, where sbx asks to approve the env."
			box.run = func() {
				a.uniboxHereThen("Creating "+r.sandboxName(), a.showUniboxSetup, "sandbox", "create", "-e", e.Name)
			}
		}
		steps = append(steps, box)
	}
	if len(names) > 0 {
		steps = append(steps, setupStep{name: "unibox doctor", state: "ask", about: "Check sbx, every env and the sandboxes, each with the command that fixes it.", run: a.showDoctor})
	}
	return steps
}

// showSetupSteps lists the steps; Enter does the one under the cursor.
func (a *App) showSetupSteps(steps []setupStep) {
	var items []pickItem
	for _, s := range steps {
		sub, role := s.state, "sandboxes.problem"
		if s.done {
			role = "sandboxes.running"
		}
		mark := glyphRing + " "
		if s.done {
			mark = glyphCheck + " "
		}
		items = append(items, pickItem{Label: mark + s.name, Sub: sub, SubRole: role, About: s.about, Data: s})
	}
	a.showPickerWith("Set up unibox", items, pickerOptions{pack: true, explain: true,
		enterName: "Do This Step", enterAbout: "Do what the step needs, or do it again; I lists the steps afresh.", enterHint: "do it"},
		func(it pickItem) {
			s := it.Data.(setupStep)
			if s.run == nil {
				a.note(s.name + " needs nothing")
				return
			}
			s.run()
		})
}

// chooseTeamRepository picks the team's repository of sbxenv files to set
// up: a clone of the list, or a git address unibox clones itself.
func (a *App) chooseTeamRepository() {
	items := []pickItem{{Label: "Git Address…", About: "Type the address of the team's repository; unibox clones it under its own data directory and keeps it updated."}}
	for _, s := range a.taskSources() {
		if s.clone || len(s.dirs) != 1 {
			continue
		}
		items = append(items, pickItem{Label: s.label, Sub: tildePath(s.dirs[0]), Data: s.dirs[0],
			About: "Run unibox init on " + tildePath(s.dirs[0]) + ": it finds every sbxenv file there and walks you through them."})
	}
	a.showPickerWith("Team repository of sbxenv files", items, pickerOptions{explain: true}, func(it pickItem) {
		if it.Data == nil {
			a.askText("Team repository", "Address", "", "Set Up", func(text string) string {
				if text == "" {
					return "enter a git address, like git@github.com:team/agent-sandboxes.git"
				}
				return ""
			}, a.initUniboxHere)
			return
		}
		a.initUniboxHere(it.Data.(string))
	})
}

// initUniboxHere walks unibox init through a team's repository - a clone
// or a git address - in this terminal, where it asks, then lists the steps
// again.
func (a *App) initUniboxHere(dir string) {
	a.uniboxHereThen("Setting up "+tildePath(dir), a.showUniboxSetup, "init", dir)
}

// setupAction lists the steps of setting unibox up; first in the pickers
// while a first sandbox still lacks something.
func (a *App) setupAction() uiAction {
	act := uiAction{name: "Set Up unibox…", about: "What a first sandbox needs - unibox, the team's repository and its envs, your values, the tokens, the sandbox - each with how it stands; Enter does a step.",
		keys: "I", rank: 60, aliases: []string{"init", "onboarding", "setup"}, run: a.showUniboxSetup}
	if a.setupWanted() {
		act.rank = 1
	}
	return act
}

// setupWanted says whether the first sandbox still needs something unagit
// can see without asking unibox: the screen offers the guide first then.
func (a *App) setupWanted() bool {
	return a.ubx.read && (a.ubx.err != nil || len(a.ubx.overview.Envs) == 0)
}
