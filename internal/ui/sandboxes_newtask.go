package ui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rivo/tview"
	"github.com/tobola/unagit/internal/unibox"
)

// taskPick is something a new task can be given: a clone of the user's,
// mounted; a worktree or a group, cloned from the forge on its branch; a
// repository of the env's index, by its name.
type taskPick struct {
	key, label, sub, about string
	repos                  []unibox.StartRepo
}

// taskDraft is what the New Task dialog holds while its pickers come and
// go: the sandbox, what was chosen to work on, and the form's fields.
type taskDraft struct {
	sandbox      sandboxChoice
	picked       []taskPick
	name, branch string
	clone, push  bool
	agent        string
	// chose is whether the repositories were picked yet: until then the
	// index's defaults and what the dialog was opened on are marked.
	chose bool
	// preset is what the dialog was opened on, marked to begin with.
	preset []string
}

// newTaskAction is New Sandbox Task… from anywhere: the sandbox of
// unibox's context, and what the screen has under its cursor marked.
func (a *App) newTaskAction() uiAction {
	return uiAction{name: "New Sandbox Task…", about: "Start an agent on a new task in a sandbox - the context's, or another - with as many repositories as it needs, its name, branch, how each gets in and whether to push.",
		keys: "Ctrl-S", rank: 505, aliases: []string{"unibox", "task", "sandbox"},
		when: a.uniboxOn, run: func() { a.newTaskFromScreen() }}
}

// newTaskFromScreen opens the dialog with the context's sandbox, marking
// the repository or worktree under the cursor of the list in front.
func (a *App) newTaskFromScreen() {
	if !a.uniboxOn() {
		a.flash("unibox is off - turn it on in Settings › Integrations")
		return
	}
	var preset []string
	switch a.currentTab() {
	case pageProjects:
		if i := a.projectsPane.selectedIndex(); i >= 0 && i < len(a.projects) {
			pr := a.projects[i]
			preset = append(preset, "dir:"+a.projectDir(pr.Instance, pr.PathWithNamespace))
		}
	case pageWorktrees:
		if i := a.worktreesPane.selectedIndex(); i >= 0 && i < len(a.worktrees) {
			preset = append(preset, "wt:"+a.worktrees[i].Dir)
		}
	}
	a.withSandboxes(func() {
		c, ok := a.contextSandbox()
		if !ok {
			choices := a.sandboxChoices()
			if len(choices) == 0 {
				a.flash("unibox has no env yet - I sets it up from your team's sbxenv files")
				return
			}
			c = choices[0]
		}
		a.newTask(c, preset)
	})
}

// newTaskIn opens the dialog for the sandbox of a row of the Sandboxes
// screen, or its env's, made on the way.
func (a *App) newTaskIn(r sandboxRow) {
	c, ok := a.choiceNamed(r.sandboxName())
	if !ok {
		a.flash("unibox has no sandbox " + r.sandboxName() + " now - R reads it again")
		return
	}
	a.newTask(c, nil)
}

// newTask opens the dialog for a sandbox: what to work on first, then the
// rest of the task.
func (a *App) newTask(c sandboxChoice, preset []string) {
	d := &taskDraft{sandbox: c, preset: preset}
	if len(c.agents) > 0 {
		d.agent = c.agents[0]
	}
	a.chooseTaskRepos(d)
}

// taskPicks are what a task can be given in sandbox c: the user's
// worktrees and clones, and the repositories of its env's index.
func (a *App) taskPicks(index []unibox.IndexedRepo) []taskPick {
	var out []taskPick
	for _, r := range index {
		sub := "index"
		if r.Default == "on" || r.Default == "always" {
			sub += " · " + r.Default
		}
		out = append(out, taskPick{key: "index:" + r.Name, label: r.Name, sub: sub,
			about: firstOf(r.Description, r.URL) + " - from the env's index, as the index says it is taken.",
			repos: []unibox.StartRepo{{Source: r.Name}}})
	}
	for _, s := range a.taskSources() {
		p := taskPick{label: s.label, sub: s.branch, about: s.about()}
		if s.clone {
			// A worktree is never mounted: its .git points outside it. It is
			// cloned from the forge on its branch, beside mounted clones.
			p.key, p.sub = "wt:"+s.dirs[0], strings.TrimSpace(s.branch+" · clone")
			for _, dir := range s.dirs {
				p.repos = append(p.repos, unibox.StartRepo{Source: a.forgeAddressOf(dir), Branch: s.branch})
			}
			if slices.ContainsFunc(p.repos, func(r unibox.StartRepo) bool { return r.Source == "" }) {
				continue
			}
		} else {
			p.key = "dir:" + s.dirs[0]
			for _, dir := range s.dirs {
				p.repos = append(p.repos, unibox.StartRepo{Source: dir})
			}
		}
		out = append(out, p)
	}
	return out
}

// forgeAddressOf is the HTTPS address of the repository a directory of
// unagit's is a checkout of, "" when it is none it knows.
func (a *App) forgeAddressOf(dir string) string {
	for _, w := range a.worktrees {
		rows := append([]worktreeRow{w}, w.Members...)
		for _, r := range rows {
			if r.Dir != dir || r.Path == "" {
				continue
			}
			if pr, ok := a.projByKey[projectKey{r.Instance, r.Path}]; ok {
				return a.httpsAddress(pr)
			}
		}
	}
	return ""
}

// chooseTaskRepos lists what the task can be given, several marked at
// once; Enter goes on with the marked, or the one under the cursor.
func (a *App) chooseTaskRepos(d *taskDraft) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var index []unibox.IndexedRepo
	a.loadThen("Reading the repositories of "+d.sandbox.env, func(func(string)) (string, error) {
		var err error
		if d.sandbox.env != "" {
			index, err = c.Repos(d.sandbox.env)
		}
		return "", err
	}, func(string) {
		picks := a.taskPicks(index)
		if len(picks) == 0 {
			a.flash("nothing to work on - clone a repository, make a worktree, or add one to the env's index (i on the env)")
			return
		}
		marks := &pickMarks{at: map[int]bool{}}
		var items []pickItem
		for i, p := range picks {
			items = append(items, pickItem{Label: p.label, Sub: p.sub, About: p.about, Data: p})
			switch {
			case d.chose:
				if slices.ContainsFunc(d.picked, func(q taskPick) bool { return q.key == p.key }) {
					marks.at[i] = true
				}
			case slices.Contains(d.preset, p.key), strings.HasSuffix(p.sub, " · on"), strings.HasSuffix(p.sub, " · always"):
				marks.at[i] = true
			}
		}
		next := func(chosen []pickItem) {
			d.picked, d.chose = nil, true
			for _, it := range chosen {
				d.picked = append(d.picked, it.Data.(taskPick))
			}
			a.taskForm(d)
		}
		a.showPickerWith("New task in "+d.sandbox.name+" · what to work on", items, pickerOptions{explain: true, marks: marks, enterMarked: true,
			enterName: "Go On", enterAbout: "Go on to the task's name, branch and agent with the marked, or with the one under the cursor when none is.", enterHint: "go on"},
			func(it pickItem) {
				if picked := marks.marked(); len(picked) > 0 {
					next(picked)
					return
				}
				next([]pickItem{it})
			})
	})
}

// taskModes are how the task's checkouts get into the sandbox.
var taskModes = []string{"mount my checkouts", "clone all from the forge"}

// taskForm asks the rest of the task: its name, the branch, how the
// checkouts get in, whether to push, the agent; then where it runs.
func (a *App) taskForm(d *taskDraft) {
	const nameLabel, branchLabel, modeLabel, pushLabel, agentLabelText = "Name", "Branch", "Checkouts", "Push the branch", "Agent"
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(nameLabel, d.name, 0, nil, nil)
	form.AddInputField(branchLabel, d.branch, 0, nil, nil)
	mode := 0
	if d.clone {
		mode = 1
	}
	modes := addSelect(form, modeLabel, taskModes, mode)
	push := addCheckbox(form, pushLabel, d.push)
	var agentNames []string
	at := 0
	for i, id := range d.sandbox.agents {
		agentNames = append(agentNames, agentLabel(id))
		if id == d.agent {
			at = i
		}
	}
	if len(agentNames) == 0 {
		a.flash(d.sandbox.name + " names no agent - its env's kit starts one")
		return
	}
	agentSelect := addSelect(form, agentLabelText, agentNames, at)
	keep := func() {
		d.name = strings.TrimSpace(form.GetFormItemByLabel(nameLabel).(*tview.InputField).GetText())
		d.branch = strings.TrimSpace(form.GetFormItemByLabel(branchLabel).(*tview.InputField).GetText())
		m, _ := modes.GetCurrentOption()
		d.clone, d.push = m == 1, push.IsChecked()
		i, _ := agentSelect.GetCurrentOption()
		d.agent = d.sandbox.agents[max(i, 0)]
	}
	form.AddButton("Start", func() {
		keep()
		a.closeModal(pageForm)
		start, label := d.start()
		a.pickPlace(placeOfAgent, label+" with "+agentLabel(d.agent)+" · where", a.agentPlaces(agentFor(d.agent)), func(place editorPlace) {
			a.checkAndStart(start, label, place)
		})
	})
	form.AddButton("Repositories", func() {
		keep()
		a.closeModal(pageForm)
		a.chooseTaskRepos(d)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	var names []string
	for _, p := range d.picked {
		names = append(names, p.label)
	}
	a.showFormModalSized("New task in "+d.sandbox.name+" · "+shortList(names, 3), form, 72, 15)
}

// start is the draft as unibox is asked for it, and what the task is
// called on the way: its name, else what it works on.
func (d *taskDraft) start() (unibox.Start, string) {
	s := unibox.Start{Sandbox: d.sandbox.name, Agent: d.agent, Branch: d.branch, Task: d.name,
		Clone: d.clone, Push: d.push, Only: true}
	if !d.sandbox.made {
		s.Env = d.sandbox.env
	}
	var names []string
	for _, p := range d.picked {
		s.Repos = append(s.Repos, p.repos...)
		names = append(names, p.label)
	}
	return s, firstOf(d.name, shortList(names, 2))
}

// shortList names the first n of names, and how many more there are.
func shortList(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:n], ", ") + " +" + strconv.Itoa(len(names)-n)
}
