package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/unibox"
)

// What each env asks of the person and what unibox keeps for it on this
// machine, and unibox's own settings: the tokens, the values, the index of
// repositories. Every change is a unibox command. A token's value never
// passes through unagit unless the user keeps it in unagit's vault:
// otherwise unibox asks for it on a hidden prompt in this terminal and
// hands it to sbx, which lists it by service alone.

// showSecrets lists the tokens an env's sandboxes use and whether sbx has
// each: Enter has unibox ask for one in this terminal, t keeps it in
// unagit, a asks for every missing one, d has sbx forget one.
func (a *App) showSecrets(e unibox.Env) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var secrets []unibox.Secret
	a.loadThen("Reading the tokens of "+e.Name, func(func(string)) (string, error) {
		var err error
		secrets, err = c.Secrets(e.Name)
		return "", err
	}, func(string) {
		var items []pickItem
		for _, s := range secrets {
			sub, role := "stored", "sandboxes.running"
			switch {
			case !s.Stored && s.Optional:
				sub, role = "optional", "sandboxes.stopped"
			case !s.Stored:
				sub, role = "missing", "sandboxes.problem"
			}
			if s.Scope != "" && s.Scope != "global" {
				sub += " · " + s.Scope
			}
			if a.vault != nil {
				if _, ok := a.vault.Credential(s.Service); ok {
					sub += " · in unagit"
				}
			}
			about := firstOf(s.Description, "A token the env's kits use")
			if len(s.Domains) > 0 {
				about += ", sent to " + strings.Join(s.Domains, ", ")
			}
			about += ". Its value is never shown."
			if s.Hint != "" {
				about += " " + s.Hint
			}
			items = append(items, pickItem{Label: s.Service, Sub: sub, SubRole: role, About: about, Data: s})
		}
		if len(items) == 0 {
			a.note("env " + e.Name + " needs no tokens")
			return
		}
		named := func(it pickItem) bool { _, ok := it.Data.(unibox.Secret); return ok }
		keys := []pickKey{
			{keys: "t", hint: "token in unagit", name: "Keep Token in unagit…", about: "Type the token into unagit's vault; sbx asks unagit for it, by unagit credential get, while unagit runs unlocked.",
				when: named, run: func(it pickItem) {
					if s, ok := it.Data.(unibox.Secret); ok {
						a.guideSecretToken(e, s)
					}
				}},
			{keys: "a", hint: "all missing", name: "Store Every Missing Token…", about: "Have unibox ask for each token sbx lacks, one after another, in this terminal.",
				run: func(pickItem) { a.uniboxHere("Storing the tokens of "+e.Name, "secret", "set", "-e", e.Name) }},
			{keys: "d", hint: "remove", name: "Remove Token…", about: "Have sbx forget the token; the env's sandboxes go without it until it is stored again.",
				when: func(it pickItem) bool { s, ok := it.Data.(unibox.Secret); return ok && s.Stored },
				run: func(it pickItem) {
					s, ok := it.Data.(unibox.Secret)
					if !ok || !s.Stored {
						return
					}
					scope := firstOf(s.Scope, "global")
					if strings.HasPrefix(scope, "env ") {
						scope = "env"
					} else if scope != "global" {
						scope = "sandbox"
					}
					a.confirmWith("Remove token", "Have sbx forget the "+scope+" token of "+s.Service+"?\n\nEvery sandbox that takes it goes without until it is stored again.", "Remove", nil, func() {
						a.closeModal(pageConfirm)
						a.uniboxDo("Removing the token of "+s.Service, "secret", "rm", s.Service, "-e", e.Name, "--scope", scope)
					})
				}},
		}
		a.showPickerWith("Tokens of "+e.Name, items, pickerOptions{pack: true, explain: true, keys: keys,
			enterName: "Store Token…", enterAbout: "Have unibox ask for the token in this terminal, on a hidden prompt, offering the page where it is made; sbx keeps it.", enterHint: "store"},
			func(it pickItem) {
				s := it.Data.(unibox.Secret)
				a.uniboxHere("Storing the token of "+s.Service, "secret", "set", s.Service, "-e", e.Name)
			})
	})
}

// valueRow is one of what an env asks of the person.
type valueRow struct {
	arg unibox.Arg
	env string
}

// showValues lists what an env asks of the person, and what they gave:
// Enter sets one, a sets one the env does not declare, d removes one.
func (a *App) showValues(e unibox.Env) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var args []unibox.Arg
	a.loadThen("Reading what "+e.Name+" asks of you", func(func(string)) (string, error) {
		var err error
		args, err = c.Args(e.Name)
		return "", err
	}, func(string) {
		var items []pickItem
		table := make([][]string, len(args))
		for i, v := range args {
			value := tag(colText) + esc(v.Value) + tagEnd
			if v.Missing() {
				value = tag(role("sandboxes.problem")) + "missing" + tagEnd
			}
			table[i] = []string{tag(role("sandboxes.task")) + esc(v.Name) + tagEnd, value, tag(colMuted) + esc(v.From) + tagEnd}
			text := firstOf(v.Description, "A value of yours")
			if v.Missing() {
				text += " - a run stops without it."
			} else {
				text += " - from " + v.From + "."
			}
			items = append(items, pickItem{About: text, Data: valueRow{arg: v, env: e.Name}})
		}
		header, labels := pickTable([]string{"NAME", "VALUE", "FROM"}, table)
		for i := range items {
			items[i].Label = labels[i]
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "it asks for nothing", About: "The env's files declare no args; a sets a value all the same."})
		}
		mine := func(it pickItem) bool {
			r, ok := it.Data.(valueRow)
			return ok && (r.arg.From == "yours" || r.arg.From == "undeclared" || r.arg.From == "global" || r.arg.From == "env-repo")
		}
		keys := []pickKey{
			{keys: "a", hint: "new", name: "New Value…", about: "Set a value of your own by name, for this env, its repository's envs or every env.",
				run: func(pickItem) { a.askValue(valueRow{env: e.Name}, true) }},
			{keys: "d", hint: "remove", name: "Remove Value…", about: "Take your value away where it was set; the env falls back on another, or stops a run for want of it.",
				when: mine, run: func(it pickItem) {
					if !mine(it) {
						return
					}
					r := it.Data.(valueRow)
					a.confirmWith("Remove value", fmt.Sprintf("Remove your %s (%s)?", r.arg.Name, r.arg.From), "Remove", nil, func() {
						a.closeModal(pageConfirm)
						a.uniboxDo("Removing "+r.arg.Name, append([]string{"var", "rm", r.arg.Name}, valueScope(r.arg.From, e.Name)...)...)
					})
				}},
		}
		a.showPickerWith("Values of "+e.Name, items, pickerOptions{wide: true, explain: true, header: header, keys: keys,
			enterName: "Set Value…", enterAbout: "Change the value, or give a missing one; for this env, its repository's envs or every env.", enterHint: "set"},
			func(it pickItem) {
				r, ok := it.Data.(valueRow)
				if !ok {
					a.askValue(valueRow{env: e.Name}, true)
					return
				}
				a.askValue(r, false)
			})
	})
}

// valueScopes are where a value is kept, as the form offers them.
var valueScopes = []string{"this env", "every env of its repository", "every env"}

// valueScope are the flags of var set and var rm for where a value is.
func valueScope(from, env string) []string {
	switch from {
	case "global", "every env":
		return []string{"-g", "-e", env}
	case "env-repo", "every env of its repository":
		return []string{"--env-repo", "-e", env}
	}
	return []string{"-e", env}
}

// askValue asks for a value and where it is kept, and sets it.
func (a *App) askValue(r valueRow, fresh bool) {
	const nameLabel, valueLabel, forLabel = "Name", "Value", "For"
	form := tview.NewForm()
	styleForm(form)
	if fresh {
		form.AddInputField(nameLabel, "", 0, nil, nil)
	}
	value := r.arg.Value
	if r.arg.From == "default" || r.arg.From == "missing" || r.arg.From == "optional" {
		value = ""
	}
	form.AddInputField(valueLabel, value, 0, nil, nil)
	selected := 0
	switch r.arg.From {
	case "global":
		selected = 2
	case "env-repo":
		selected = 1
	}
	scope := addSelect(form, forLabel, valueScopes, selected)
	form.AddButton("Set", func() {
		name := r.arg.Name
		if fresh {
			name = strings.TrimSpace(form.GetFormItemByLabel(nameLabel).(*tview.InputField).GetText())
		}
		value := form.GetFormItemByLabel(valueLabel).(*tview.InputField).GetText()
		if name == "" {
			a.flash("enter the value's name, as the env's args name it")
			return
		}
		at, _ := scope.GetCurrentOption()
		a.closeModal(pageForm)
		a.uniboxDo("Setting "+name, append([]string{"var", "set", name, value}, valueScope(valueScopes[max(at, 0)], r.env)...)...)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	title := "New value · " + r.env
	height := 11
	if !fresh {
		title = "Set " + r.arg.Name + " · " + r.env
		height = 9
	}
	a.showFormModalSized(title, form, 64, height)
}

// showRepoIndex lists the repositories an env's tasks are given by name:
// Enter says whether a new task takes one, a names one of the user's, d
// forgets one of theirs.
func (a *App) showRepoIndex(e unibox.Env) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var repos []unibox.IndexedRepo
	a.loadThen("Reading the repositories of "+e.Name, func(func(string)) (string, error) {
		var err error
		repos, err = c.Repos(e.Name)
		return "", err
	}, func(string) {
		var items []pickItem
		for _, r := range repos {
			sub := firstOf(r.Default, "off") + " · " + firstOf(r.From, "yours")
			about := firstOf(r.Description, r.URL)
			if r.Ref != "" {
				about += " · at " + r.Ref
			}
			if r.Context != nil && *r.Context || r.Default == "always" && r.Context == nil {
				about += " · read only, a context"
			}
			items = append(items, pickItem{Label: r.Name, Sub: sub, About: about + ".", Data: r})
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "no repositories named", About: "a names one of yours - a clone of the list - for the env's tasks; Add to unibox Index… on a repository does too."})
		}
		yours := func(it pickItem) bool {
			r, ok := it.Data.(unibox.IndexedRepo)
			return ok && strings.HasPrefix(r.From, "yours")
		}
		keys := []pickKey{
			{keys: "a", hint: "add", name: "Add Repository…", about: "Name a clone of yours for the env's tasks, mounted where it is.",
				run: func(pickItem) { a.pickIndexSource(e) }},
			{keys: "d", hint: "forget", name: "Forget Repository", about: "Forget a repository you named; the team's stay.",
				when: yours, run: func(it pickItem) {
					if !yours(it) {
						a.flash("a repository the team names stays - say off for you with Enter")
						return
					}
					r := it.Data.(unibox.IndexedRepo)
					args := []string{"repo", "rm", r.Name, "-e", e.Name}
					switch r.From {
					case "yours-all":
						args = append(args, "-g")
					case "yours-env-repo":
						args = append(args, "--env-repo")
					}
					a.uniboxDo("Forgetting "+r.Name, args...)
				}},
		}
		a.showPickerWith("Repository index of "+e.Name, items, pickerOptions{pack: true, explain: true, keys: keys,
			enterName: "Say When a Task Takes It…", enterAbout: "Always, picked to begin with, or only when picked - for you.", enterHint: "default"},
			func(it pickItem) {
				if r, ok := it.Data.(unibox.IndexedRepo); ok {
					a.pickRepoDefault(e.Name, r.Name)
				}
			})
	})
}

// repoDefaults are when a new task takes a repository of the index.
var repoDefaults = []struct{ value, label, about string }{
	{"always", "Always", "Every new task of the env takes it, mounted or cloned; a context repository stays on its ref."},
	{"on", "Picked to Begin With", "unibox task new has it picked to begin with; run takes it with --defaults."},
	{"off", "Only When Picked", "A task takes it only when it is picked or named."},
	{"-", "As the Team Says", "Forget what you said; the team's word, or off."},
}

func (a *App) pickRepoDefault(env, name string) {
	var items []pickItem
	for _, d := range repoDefaults {
		items = append(items, pickItem{Label: d.label, About: d.about, Data: d.value})
	}
	a.showPickerWith(name+" · when a task takes it", items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		a.uniboxDo("Saying when "+name+" is taken", "repo", "default", name, it.Data.(string), "-e", env)
	})
}

// pickIndexSource lists the clones on disk to name in an env's index.
func (a *App) pickIndexSource(e unibox.Env) {
	var items []pickItem
	for _, s := range a.taskSources() {
		if s.clone || len(s.dirs) != 1 {
			continue
		}
		items = append(items, pickItem{Label: s.label, Sub: tildePath(s.dirs[0]), About: "Name " + tildePath(s.dirs[0]) + " for the tasks of " + e.Name + ", mounted where it is.", Data: s.dirs[0]})
	}
	if len(items) == 0 {
		a.flash("no clone on disk to name - clone a repository first")
		return
	}
	a.showPickerWith("Add to the index of "+e.Name, items, pickerOptions{explain: true}, func(it pickItem) {
		a.uniboxDo("Adding to the index of "+e.Name, "repo", "add", it.Data.(string), "-e", e.Name)
	})
}

// indexActions are a repository's actions on unibox's index: naming it
// for an env's tasks, or every env's, so a task is given it by name.
func (a *App) indexActions(label string, source func() string, ok func() bool) []uiAction {
	can := func() bool { return a.uniboxOn() && (ok == nil || ok()) && source() != "" }
	return []uiAction{{name: "Add to unibox Index…", about: "Name it in unibox's index of repositories, for one env's tasks or every env's, so a task is given it by name: which env, then how.",
		rank: 650, aliases: []string{"unibox", "repo add", "index"}, when: can,
		run: func() {
			a.withSandboxes(func() { a.pickIndexEnv(label, source()) })
		}}}
}

// pickIndexEnv asks which env's index a repository goes into, every env's
// included, then when a task takes it.
func (a *App) pickIndexEnv(label, source string) {
	items := []pickItem{{Label: "Every Env", About: "Name it for every env's tasks.", Data: ""}}
	for _, e := range a.ubx.overview.Envs {
		items = append(items, pickItem{Label: e.Name, Sub: tildePath(e.Repo), About: "Name it for the tasks of " + e.Name + ".", Data: e.Name})
	}
	a.showPickerWith(label+" · into which index", items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
		env := it.Data.(string)
		var defaults []pickItem
		for _, d := range repoDefaults[:3] {
			defaults = append(defaults, pickItem{Label: d.label, About: d.about, Data: d.value})
		}
		a.showPickerWith(label+" · when a task takes it", defaults, pickerOptions{pack: true, explain: true}, func(d pickItem) {
			args := []string{"repo", "add", source, "--default", d.Data.(string)}
			if env == "" {
				args = append(args, "-g")
			} else {
				args = append(args, "-e", env)
			}
			a.uniboxDo("Adding "+label+" to unibox's index", args...)
		})
	})
}

// uniboxSwitch is one of unibox's settings that is on or off: its key for
// config set, how the form and the panel say it, and its default.
type uniboxSwitch struct {
	key, label, short string
	get               func(unibox.Config) *bool
	unset             bool
}

var uniboxSwitches = []uniboxSwitch{
	{"stop_idle", "Stop a sandbox when idle", "stop idle", func(c unibox.Config) *bool { return c.StopIdle }, true},
	{"ssh_agent", "Sandboxes use your SSH agent", "SSH agent", func(c unibox.Config) *bool { return c.SSHAgent }, false},
	{"editor_ssh", "Editors reach sandboxes over SSH", "editor SSH", func(c unibox.Config) *bool { return c.EditorSSH }, false},
	{"remember_host_commands", "Remember approved host commands", "", func(c unibox.Config) *bool { return c.RememberHostCommands }, false},
	{"claude_remote_control", "Claude Code remote control", "", func(c unibox.Config) *bool { return c.ClaudeRemoteControl }, false},
	{"clipboard_image_paste", "Paste images from the clipboard", "", func(c unibox.Config) *bool { return c.ClipboardImagePaste }, false},
}

// switchWord says a switch as the detail does.
func switchWord(v *bool, unset bool) string {
	switch {
	case v == nil && unset:
		return "on, unibox's default"
	case v == nil:
		return "off, not chosen"
	case *v:
		return "on"
	}
	return "off"
}

// showUniboxSettings is unibox's own configuration as a form: the default
// env and its switches. Only what changed is set.
func (a *App) showUniboxSettings() {
	o := a.ubx.overview
	form := tview.NewForm()
	styleForm(form)
	envs := []string{"none"}
	selected := 0
	for i, e := range o.Envs {
		envs = append(envs, e.Name)
		if e.Name == o.Config.DefaultEnv {
			selected = i + 1
		}
	}
	defaultEnv := addSelect(form, "Default env", envs, selected)
	boxes := make([]*tview.Checkbox, len(uniboxSwitches))
	was := make([]bool, len(uniboxSwitches))
	for i, s := range uniboxSwitches {
		v := s.get(o.Config)
		was[i] = v != nil && *v || v == nil && s.unset
		boxes[i] = addCheckbox(form, s.label, was[i])
	}
	form.AddButton("Save", func() {
		var changes [][]string
		if at, _ := defaultEnv.GetCurrentOption(); at > 0 && at != selected {
			changes = append(changes, []string{"config", "set", "default_env", envs[at]})
		}
		for i, s := range uniboxSwitches {
			if on := boxes[i].IsChecked(); on != was[i] {
				changes = append(changes, []string{"config", "set", s.key, fmt.Sprint(on)})
			}
		}
		a.closeModal(pageForm)
		if len(changes) == 0 {
			a.note("nothing changed")
			return
		}
		a.uniboxDoAll("Saving unibox's settings", changes)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	// A line for each item and one between them, the buttons and the hint
	// under them, the border.
	a.showFormModalSized("unibox settings", form, 64, 2*(1+len(uniboxSwitches))+5)
}

// uniboxDoAll runs several commands in turn, as one job, stopping at the
// first that fails.
func (a *App) uniboxDoAll(title string, commands [][]string) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	job := a.startJob(title)
	events := make([]*uniboxEvent, len(commands))
	for i, args := range commands {
		events[i] = a.logUnibox(title, args...)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		said := make([]string, len(commands))
		var failed error
		at := len(commands)
		for i, args := range commands {
			if said[i], failed = c.Do(ctx, args...); failed != nil {
				at = i
				break
			}
		}
		a.tv.QueueUpdateDraw(func() {
			a.endJob(job)
			for i, e := range events {
				switch {
				case i < at:
					e.end(a, said[i], nil, false)
				case i == at:
					e.end(a, "", failed, refusal(failed))
				default:
					e.end(a, "", fmt.Errorf("not run: an earlier change failed"), false)
				}
			}
			a.sayUnibox(title, "", failed)
			a.refreshSandboxes()
		})
	}()
}

// askText asks for one line of text; check says what is wrong with it,
// "" when nothing is.
func (a *App) askText(title, label, value, button string, check func(string) string, then func(string)) {
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(label, value, 0, nil, nil)
	form.AddButton(button, func() {
		text := strings.TrimSpace(form.GetFormItemByLabel(label).(*tview.InputField).GetText())
		if check != nil {
			if problem := check(text); problem != "" {
				a.flash(problem)
				return
			}
		}
		a.closeModal(pageForm)
		then(text)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized(title, form, 60, 7)
}
