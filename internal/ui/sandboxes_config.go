package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/unibox"
)

// What unibox keeps for the profiles on this machine, and its own
// settings: the secrets, the values each person sets, the runtimes. Every
// change is a unibox command. A secret's value never passes through
// unagit: unibox asks for it on a hidden prompt in this terminal, and
// lists the secrets by name alone.

// showSecrets lists the secrets by name: Enter stores a new value under
// one, a stores another, d removes one.
func (a *App) showSecrets() {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var secrets unibox.Secrets
	a.loadThen("Reading the secrets", func(func(string)) (string, error) {
		var err error
		secrets, err = c.Secrets()
		return "", err
	}, func(string) {
		store := firstOf(secrets.Store, "platform's store")
		var items []pickItem
		for _, name := range secrets.Names {
			items = append(items, pickItem{Label: name, Sub: "secret:" + name,
				About: "Kept in the " + store + "; a profile names it as secret:" + name + ". Its value is never shown.", Data: name})
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "no secrets stored", About: "a stores one: unibox asks for it in this terminal, on a hidden prompt."})
		}
		named := func(it pickItem) bool { return it.Data != nil }
		keys := []pickKey{
			{keys: "a", hint: "store", name: "Store Secret…", about: "Keep a new token under a name; unibox asks for it in this terminal, on a hidden prompt.",
				run: func(pickItem) {
					a.askText("Store a secret", "Name", "", "Store", func(name string) string {
						if name == "" {
							return "enter a name, like artifactory - profiles name it as secret:<name>"
						}
						return ""
					}, func(name string) {
						a.uniboxSignIn("Storing secret "+name, "secret", "set", name)
					})
				}},
			{keys: "d", hint: "remove", name: "Remove Secret…", about: "Take the token out of the store; profiles that name it stop at their next run.",
				when: named, run: func(it pickItem) {
					if it.Data == nil {
						return
					}
					name := it.Data.(string)
					a.confirmWith("Remove secret", "Remove secret "+name+" from the "+store+"?\n\nA profile that names secret:"+name+" stops at its next run until it is stored again.", "Remove", nil, func() {
						a.closeModal(pageConfirm)
						a.uniboxDo("Removing secret "+name, "secret", "rm", name)
					})
				}},
		}
		a.showPickerWith("Secrets · "+store, items, pickerOptions{pack: true, explain: true, keys: keys,
			enterName: "Replace Secret…", enterAbout: "Store a new value under the name; unibox asks for it in this terminal, on a hidden prompt.", enterHint: "replace"},
			func(it pickItem) {
				if it.Data == nil {
					return
				}
				name := it.Data.(string)
				a.uniboxSignIn("Replacing secret "+name, "secret", "set", name)
			})
	})
}

// valueRow is one of the user's values, or one a profile asks for that
// has none.
type valueRow struct {
	name, value string
	// profile is the profile the value is for, "" for every profile.
	profile string
	missing bool
	about   string
}

func (v valueRow) scope() string {
	if v.profile == "" {
		return "every profile"
	}
	return "profile " + v.profile
}

// showValues lists the user's values and those the profiles ask for that
// are missing: Enter sets one, a sets a new one, d removes one.
func (a *App) showValues() {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var profiles []string
	for _, p := range a.ubx.overview.Profiles {
		if p.Error == "" {
			profiles = append(profiles, p.Name)
		}
	}
	sort.Strings(profiles)
	var values unibox.Values
	asked := map[string][]unibox.Asked{}
	a.loadThen("Reading your values", func(step func(string)) (string, error) {
		var err error
		if values, err = c.Values(); err != nil {
			return "", err
		}
		for _, p := range profiles {
			step("What " + p + " asks for")
			if asked[p], err = c.Asked(p); err != nil {
				return "", err
			}
		}
		return "", nil
	}, func(string) {
		about := map[string]string{}
		var rows []valueRow
		for _, p := range profiles {
			for _, v := range asked[p] {
				if v.Description != "" {
					about[v.Name] = v.Description
				}
				if v.Missing() {
					rows = append(rows, valueRow{name: v.Name, profile: p, missing: true, about: v.Description})
				}
			}
		}
		for _, name := range sortedKeys(values.Shared) {
			rows = append(rows, valueRow{name: name, value: values.Shared[name], about: about[name]})
		}
		var withOwn []string
		for p := range values.Profiles {
			withOwn = append(withOwn, p)
		}
		sort.Strings(withOwn)
		for _, p := range withOwn {
			for _, name := range sortedKeys(values.Profiles[p]) {
				rows = append(rows, valueRow{name: name, value: values.Profiles[p][name], profile: p, about: about[name]})
			}
		}
		var items []pickItem
		table := make([][]string, len(rows))
		for i, r := range rows {
			value := tag(colText) + esc(r.value) + tagEnd
			if r.missing {
				value = tag(role("sandboxes.problem")) + "missing" + tagEnd
			}
			table[i] = []string{tag(role("sandboxes.task")) + esc(r.name) + tagEnd, value, tag(colMuted) + esc(r.scope()) + tagEnd}
			text := firstOf(r.about, "A value of yours")
			if r.missing {
				text += " - " + r.profile + " asks for it, and a run stops without it."
			} else {
				text += " - for " + r.scope() + "."
			}
			items = append(items, pickItem{About: text, Data: r})
		}
		header, labels := pickTable([]string{"NAME", "VALUE", "FOR"}, table)
		for i := range items {
			items[i].Label = labels[i]
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "no values set", About: "a sets one: a name, a login, whatever a profile's vars ask of you."})
		}
		set := func(it pickItem) bool { return it.Data != nil && !it.Data.(valueRow).missing }
		keys := []pickKey{
			{keys: "a", hint: "new", name: "New Value…", about: "Set a value of your own, for every profile or for one.",
				run: func(pickItem) { a.askValue(valueRow{}, profiles, true) }},
			{keys: "d", hint: "remove", name: "Remove Value…", about: "Take the value away; a profile that asks for it falls back on the shared one, or stops.",
				when: set, run: func(it pickItem) {
					if !set(it) {
						return
					}
					r := it.Data.(valueRow)
					a.confirmWith("Remove value", fmt.Sprintf("Remove %s for %s?", r.name, r.scope()), "Remove", nil, func() {
						a.closeModal(pageConfirm)
						a.uniboxDo("Removing "+r.name, append([]string{"var", "rm", r.name}, scopeArgs(r.profile)...)...)
					})
				}},
		}
		title := "Your values"
		if values.File != "" {
			title += " · " + tildePath(values.File)
		}
		a.showPickerWith(title, items, pickerOptions{wide: true, explain: true, header: header, keys: keys,
			enterName: "Set Value…", enterAbout: "Change the value, or give a missing one; for every profile or for one.", enterHint: "set"},
			func(it pickItem) {
				if it.Data == nil {
					a.askValue(valueRow{}, profiles, true)
					return
				}
				a.askValue(it.Data.(valueRow), profiles, false)
			})
	})
}

// scopeArgs are a value's profile, as unibox var takes it.
func scopeArgs(profile string) []string {
	if profile == "" {
		return nil
	}
	return []string{"-p", profile}
}

// askValue asks for a value and whom it is for, and sets it. A missing one
// is offered for every profile first: what one profile asks of a person
// the next usually does too.
func (a *App) askValue(r valueRow, profiles []string, fresh bool) {
	const nameLabel, valueLabel, forLabel = "Name", "Value", "For"
	form := tview.NewForm()
	styleForm(form)
	if fresh {
		form.AddInputField(nameLabel, r.name, 0, nil, nil)
	}
	form.AddInputField(valueLabel, r.value, 0, nil, nil)
	options := []string{"every profile"}
	selected := 0
	for i, p := range profiles {
		options = append(options, "profile "+p)
		if p == r.profile && !r.missing {
			selected = i + 1
		}
	}
	scope := addSelect(form, forLabel, options, selected)
	form.AddButton("Set", func() {
		name := r.name
		if fresh {
			name = strings.TrimSpace(form.GetFormItemByLabel(nameLabel).(*tview.InputField).GetText())
		}
		value := form.GetFormItemByLabel(valueLabel).(*tview.InputField).GetText()
		if name == "" {
			a.flash("enter the value's name, as the profile's vars name it")
			return
		}
		at, _ := scope.GetCurrentOption()
		profile := ""
		if at > 0 {
			profile = profiles[at-1]
		}
		a.closeModal(pageForm)
		a.uniboxDo("Setting "+name, append([]string{"var", "set", name, value}, scopeArgs(profile)...)...)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	title := "New value"
	height := 11
	if !fresh {
		title = "Set " + r.name
		height = 9
	}
	a.showFormModalSized(title, form, 64, height)
}

// showUniboxSettings is unibox's own configuration as a form: the runtime
// new sandboxes get, and for each runtime whether it may be used and
// whether a sandbox stops when its last session ends. Only what changed
// is set.
func (a *App) showUniboxSettings() {
	o := a.ubx.overview
	if len(o.Runtimes) == 0 {
		a.flash("unibox names no runtimes - Refresh (r) reads them again")
		return
	}
	form := tview.NewForm()
	styleForm(form)
	var names []string
	selected := 0
	for i, rt := range o.Runtimes {
		names = append(names, rt.Name)
		if rt.Name == o.Config.DefaultRuntime || o.Config.DefaultRuntime == "" && rt.Default {
			selected = i
		}
	}
	defaultRuntime := addSelect(form, "Default runtime", names, selected)
	type runtimeBoxes struct {
		name         string
		on, stopIdle *tview.Checkbox
		wasOn, wasSI bool
	}
	var boxes []runtimeBoxes
	for _, rt := range o.Runtimes {
		set := o.Config.Runtimes[rt.Name]
		on := set.On == nil || *set.On
		stopIdle := set.StopIdle == nil || *set.StopIdle
		boxes = append(boxes, runtimeBoxes{
			name:     rt.Name,
			on:       addCheckbox(form, rt.Name+" may be used", on),
			stopIdle: addCheckbox(form, rt.Name+" stops when idle", stopIdle),
			wasOn:    on, wasSI: stopIdle,
		})
	}
	form.AddButton("Save", func() {
		var changes [][]string
		at, _ := defaultRuntime.GetCurrentOption()
		if at >= 0 && names[at] != names[selected] {
			changes = append(changes, []string{"config", "set", "default_runtime", names[at]})
		}
		for _, b := range boxes {
			if on := b.on.IsChecked(); on != b.wasOn {
				changes = append(changes, []string{"config", "set", "runtimes." + b.name + ".on", fmt.Sprint(on)})
			}
			if si := b.stopIdle.IsChecked(); si != b.wasSI {
				changes = append(changes, []string{"config", "set", "runtimes." + b.name + ".stop_idle", fmt.Sprint(si)})
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
	// A line for each item and one between them, the buttons and the
	// hint under them, the border.
	a.showFormModalSized("unibox settings", form, 64, 2*(1+2*len(boxes))+5)
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
