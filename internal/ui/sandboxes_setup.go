package ui

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/unibox"
)

// Setting unibox up is a list of what a first sandbox needs, read from
// unibox as it is: unibox itself, the tab, the default runtime ready to run,
// the environments repository, a profile, its image, and whatever stands
// between a profile and a start - a login, a value, a token. Each step says
// how it stands, and Enter does it; the list is opened again with I, and
// reads everything afresh, so it is never a wizard to finish in one go.

// setupStep is one step of setting unibox up.
type setupStep struct {
	name, state, about string
	done               bool
	// run does the step - or does it again, for one done that can be
	// done more than once, such as adding a profile; nil when there is
	// nothing to do.
	run func()
}

// setupNeedsLeftOut are what task check says of the repository it is
// given rather than of the profile; the guide checks with the
// environments repository, whose branch says nothing of a task's.
var setupNeedsLeftOut = []string{"branch_not_pushed", "branch_missing", "uncommitted", "local_commits"}

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
	needs := map[string][]unibox.Need{}
	a.loadThen("Reading where unibox stands", func(step func(string)) (string, error) {
		var err error
		if o, err = c.Overview(); err != nil {
			return "", err
		}
		if o.Environments == "" {
			return "", nil
		}
		for _, p := range o.Profiles {
			if p.Error != "" {
				continue
			}
			step("Checking " + p.Name)
			for _, id := range p.Agents {
				check, err := c.Check(unibox.Start{Profile: p.Name, Agent: id, Dirs: []string{o.Environments}})
				if err != nil {
					needs[p.Name] = append(needs[p.Name], unibox.Need{Code: "check_failed", Message: err.Error(), Agent: id})
					continue
				}
				for _, n := range check.Needs {
					if !n.Warning && !slices.Contains(setupNeedsLeftOut, n.Code) && !hasNeed(needs[p.Name], n) {
						needs[p.Name] = append(needs[p.Name], n)
					}
				}
			}
		}
		return "", nil
	}, func(string) {
		a.ubx = uniboxState{overview: o, read: true, at: time.Now(), details: map[string]*sandboxDetail{}}
		a.sandboxRows = sandboxRows(o)
		a.sandboxesPane.reload()
		a.showSetupSteps(a.setupSteps(o, needs))
	})
}

func hasNeed(needs []unibox.Need, n unibox.Need) bool {
	for _, have := range needs {
		if have.Code == n.Code && have.Message == n.Message {
			return true
		}
	}
	return false
}

// setupSteps are the steps, as unibox stands.
func (a *App) setupSteps(o unibox.Overview, needs map[string][]unibox.Need) []setupStep {
	steps := []setupStep{{name: "unibox", state: "installed " + o.Version.Version, done: true,
		about: fmt.Sprintf("unibox %s, profile format %d, runs the sandboxes; unagit drives it from PATH.", o.Version.Version, o.Version.ProfileFormat)}}

	tab := setupStep{name: "Sandboxes tab", state: "on", done: true, about: "The Sandboxes tab [5] lists the sandboxes, their tasks and the profiles."}
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

	var runtime unibox.Runtime
	for _, rt := range o.Runtimes {
		if rt.Default {
			runtime = rt
		}
	}
	rs := setupStep{name: "Runtime " + runtime.Name, state: "ready", done: true,
		about: "New sandboxes run in " + runtime.Name + ", which is ready. U, unibox's settings, chooses another.", run: a.showUniboxSettings}
	switch {
	case runtime.Name == "":
		rs = setupStep{name: "Runtime", state: "none", about: "unibox names no default runtime. Enter opens unibox's settings, where one is chosen.", run: a.showUniboxSettings}
	case !runtime.Available:
		rs.state, rs.done = "not ready", false
		rs.about = runtime.Name + " cannot run sandboxes: " + firstOf(runtime.Error, "unibox does not say why") + "."
		if fix := tryCommand(runtime.Error); fix != "" {
			rs.about += " Enter runs " + fix + " in this terminal."
			rs.run = func() { a.runSetupCommand(fix) }
		} else {
			rs.about += " Enter opens unibox's settings, where another runtime is chosen."
		}
	}
	steps = append(steps, rs)

	env := setupStep{name: "Environments repository", state: tildePath(o.Environments), done: o.Environments != "",
		about: "Your profiles live in " + tildePath(o.Environments) + ", a repository of yours. Enter chooses another clone to lay out and use.",
		run:   a.chooseSetupEnvironments}
	if o.Environments == "" {
		env.state = "none"
		env.about = "The profiles live in a repository of yours. Enter picks a clone to lay out with unibox init, or creates a new repository first - then Init unibox Here on it."
	}
	steps = append(steps, env)
	if o.Environments == "" {
		return steps
	}

	var names []string
	for _, p := range o.Profiles {
		names = append(names, p.Name)
	}
	prof := setupStep{name: "Profiles", state: strings.Join(names, ", "), done: len(names) > 0,
		about: "A profile says what a sandbox is: its image, its agents, what it may reach. Enter adds another.", run: a.askNewProfile}
	if len(names) == 0 {
		prof.state = "none"
		prof.about = "A profile says what a sandbox is: its image, its agents, what it may reach. Enter adds one: its name, its agents, its resources."
	}
	steps = append(steps, prof)

	for _, p := range o.Profiles {
		if p.Error != "" {
			steps = append(steps, setupStep{name: "Profile " + p.Name, state: "does not load",
				about: p.Error + ". Enter migrates it with unibox init " + p.Name + ".",
				run:   func() { a.uniboxStream("Migrating "+p.Name, "init", p.Name) }})
			continue
		}
		image := setupStep{name: "Image of " + p.Name, state: "built", done: true,
			about: "The image of " + p.Name + " for " + runtime.Name + " is built. Enter builds it again.",
			run:   func() { a.uniboxStream("Building "+p.Name, "profile", "build", p.Name) }}
		if state := p.Images[runtime.Name]; state != "built" {
			image.state, image.done = firstOf(state, "not built"), false
			image.about = "The image of " + p.Name + " for " + runtime.Name + " is " + image.state + "; a sandbox starts from it. Enter builds it, in a log."
		}
		steps = append(steps, image)

		ready := setupStep{name: p.Name + " ready to start", state: "ready", done: true,
			about: "Nothing stands between " + p.Name + " and a first task: its agents are signed in, its values set."}
		if ns := needs[p.Name]; len(ns) > 0 {
			ready.done = false
			ready.state = counted(len(ns), "thing to do", "things to do")
			var lines []string
			for _, n := range ns {
				lines = append(lines, n.Message)
			}
			ready.about = strings.Join(lines, "; ") + "."
			ready.run = a.setupNeedRun(p, ns[0])
		}
		steps = append(steps, ready)
	}
	return steps
}

// setupNeedRun is what Enter does for what stands before a start.
func (a *App) setupNeedRun(p unibox.Profile, n unibox.Need) func() {
	switch {
	case n.Code == "login_required" || n.Code == "auth_unsupported":
		return func() { a.signInAgents(p) }
	case n.Code == "var_missing":
		return a.showValues
	case strings.Contains(n.Code, "secret") || strings.Contains(n.Code, "service"):
		return func() { a.showProfileServices(p) }
	case strings.Contains(n.Code, "image") || strings.Contains(n.Code, "build"):
		return func() { a.uniboxStream("Building "+p.Name, "profile", "build", p.Name) }
	}
	return func() { a.note(n.Message) }
}

// tryCommand is the command an error of unibox's suggests, after "try:".
func tryCommand(text string) string {
	at := strings.LastIndex(text, "try: ")
	if at < 0 {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(text[at+len("try: "):], "\n", 2)[0])
}

// runSetupCommand runs what a step suggests in this terminal - a sign-in
// that opens the browser, as sbx login does - then lists the steps again.
func (a *App) runSetupCommand(command string) {
	cmd := exec.Command("/bin/sh", "-c", command)
	go func() {
		err := a.runTerminalEditor(cmd, editors.Editor{Name: command})
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.flash(fmt.Sprintf("%s did not finish: %v", command, err))
				return
			}
			a.showUniboxSetup()
		})
	}()
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

// chooseSetupEnvironments picks the clone to lay out as the environments
// repository, or creates a repository first.
func (a *App) chooseSetupEnvironments() {
	items := []pickItem{{Label: "New Repository…", About: "Create a repository on a server and clone it, as Repositories does; then Init unibox Here on it there."}}
	for _, s := range a.taskSources() {
		if len(s.dirs) != 1 {
			continue
		}
		items = append(items, pickItem{Label: s.label, Sub: tildePath(s.dirs[0]), Data: s.dirs[0],
			About: "Lay " + tildePath(s.dirs[0]) + " out with unibox init and keep the profiles there; what is in it already stays."})
	}
	a.showPickerWith("Environments repository", items, pickerOptions{explain: true}, func(it pickItem) {
		if it.Data == nil {
			a.showNewRepository()
			return
		}
		a.initUniboxHere(it.Data.(string))
	})
}

// initUniboxHere lays a clone out as the environments repository and makes
// it the one unibox uses; another one in use is asked about first.
func (a *App) initUniboxHere(dir string) {
	a.withSandboxes(func() {
		init := func() {
			a.uniboxStream("Laying out "+tildePath(dir), "init", "--dir", dir)
		}
		current := a.ubx.overview.Environments
		if current == "" || sameDirectory(current, dir) {
			init()
			return
		}
		a.confirmWith("Init unibox here", "unibox keeps its profiles in "+tildePath(current)+".\n\nLay out "+tildePath(dir)+" and keep them there from now on? The old repository stays as it is.", "Init", nil, func() {
			a.closeModal(pageConfirm)
			init()
		})
	})
}

// sandboxAgentChoices are the agents unibox installs in a new profile.
var sandboxAgentChoices = []string{"claude", "codex", "copilot"}

// askNewProfile adds a profile: its name, its agents, its resources, and
// a template to copy it from when wanted.
func (a *App) askNewProfile() {
	const nameLabel, cpusLabel, memoryLabel, fromLabel = "Name", "CPUs", "Memory", "Template"
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(nameLabel, "", 0, nil, nil)
	boxes := map[string]*tview.Checkbox{}
	for _, id := range sandboxAgentChoices {
		boxes[id] = addCheckbox(form, agentLabel(id), id == "claude")
	}
	form.AddInputField(cpusLabel, "4", 0, nil, nil)
	form.AddInputField(memoryLabel, "8g", 0, nil, nil)
	form.AddInputField(fromLabel, "", 0, nil, nil)
	form.AddTextView("", "CPUs, memory: a number or a share, as 50%.\nTemplate: a git URL or a directory to copy\nthe profile from; empty for none.", 0, 3, true, false)
	text := func(label string) string {
		return strings.TrimSpace(form.GetFormItemByLabel(label).(*tview.InputField).GetText())
	}
	form.AddButton("Add", func() {
		name := text(nameLabel)
		var agents []string
		for _, id := range sandboxAgentChoices {
			if boxes[id].IsChecked() {
				agents = append(agents, id)
			}
		}
		switch {
		case !credentialName.MatchString(name):
			a.flash("enter the profile's name: letters, digits, dots, dashes - like web")
			return
		case len(agents) == 0 && text(fromLabel) == "":
			a.flash("tick an agent for the profile's sandboxes to run")
			return
		}
		args := []string{"init", name, "--cpus", firstOf(text(cpusLabel), "4"), "--memory", firstOf(text(memoryLabel), "8g")}
		if from := text(fromLabel); from != "" {
			args = append(args, "--from", from)
		} else {
			args = append(args, "--agents", strings.Join(agents, ","))
		}
		a.closeModal(pageForm)
		a.uniboxStream("Adding profile "+name, args...)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("New profile", form, 64, 22)
}

// setupAction lists the steps of setting unibox up; first in the pickers
// while a first sandbox still lacks something.
func (a *App) setupAction() uiAction {
	act := uiAction{name: "Set Up unibox…", about: "What a first sandbox needs - unibox, the runtime, the environments repository, a profile, its image, its logins - each with how it stands; Enter does a step.",
		keys: "I", rank: 60, aliases: []string{"init", "onboarding", "setup"}, run: a.showUniboxSetup}
	if a.setupWanted() {
		act.rank = 1
	}
	return act
}

// setupWanted says whether the first sandbox still needs something unagit
// can see without asking unibox: the screen offers the guide first then.
func (a *App) setupWanted() bool {
	o := a.ubx.overview
	return a.ubx.read && (a.ubx.err != nil || o.Environments == "" || len(o.Profiles) == 0)
}
