package ui

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/unibox"
)

// What the person says to unibox, beside what the team's files say: the
// ports of an env, and the whole of their configuration in one list -
// settings, values, ports, repositories, instructions, network rules and
// the tokens sbx keeps - each opened where it is managed, or forgotten
// where it stands. All of it is read from unibox's --json and changed by
// unibox's commands, as everything on the Sandboxes screen is.

// portScopes are where a port of the person's is named, as the form
// offers them.
var portScopes = []string{"this env", "every env of its repository", "every env"}

// portScopeFlags are the flags of port add and port rm for a scope.
func portScopeFlags(scope, env string) []string {
	switch scope {
	case "every env", "yours-all":
		return []string{"-g", "-e", env}
	case "every env of its repository", "yours-env-repo":
		return []string{"--env-repo", "-e", env}
	}
	return []string{"-e", env}
}

// yourPortScope is where the person named a port an env has: this env,
// every env, or - neither - every env of its repository.
func (a *App) yourPortScope(env, name string) string {
	cfg := a.ubx.overview.Config
	if _, ok := cfg.EnvConfigs[env].Ports[name]; ok {
		return "yours"
	}
	if _, ok := cfg.Ports[name]; ok {
		return "yours-all"
	}
	return "yours-env-repo"
}

// showEnvPorts lists an env's ports, the team's and the person's, whether
// a sandbox is made from it or not: Enter says whether one is on at each
// start, a names one of the person's, d forgets one of theirs.
func (a *App) showEnvPorts(e unibox.Env) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	var ports []unibox.EnvPort
	a.loadThen("Reading the ports of "+e.Name, func(func(string)) (string, error) {
		var err error
		ports, err = c.EnvPorts(e.Name)
		return "", err
	}, func(string) {
		var items []pickItem
		table := make([][]string, len(ports))
		for i, p := range ports {
			whose, start := "yours", "off at start"
			if p.Team {
				whose = "team"
			}
			startRole := "sandboxes.stopped"
			if p.Default {
				start, startRole = "on at start", "sandboxes.running"
			}
			table[i] = []string{tag(role("sandboxes.task")) + esc(p.Name) + tagEnd, tag(colText) + esc(p.Port) + tagEnd,
				tag(role(startRole)) + start + tagEnd, tag(colMuted) + whose + tagEnd}
			about := firstOf(p.Description, "A port of "+e.Name+"'s sandboxes") + " - " + p.Port + ", the team's."
			if !p.Team {
				about = firstOf(p.Description, "A port of yours") + " - " + p.Port + ", named by you."
			}
			items = append(items, pickItem{About: about, Data: p})
		}
		header, labels := pickTable([]string{"NAME", "MAPPING", "START", "WHOSE"}, table)
		for i := range items {
			items[i].Label = labels[i]
		}
		if len(items) == 0 {
			items = append(items, pickItem{Label: "no ports named", About: "a names a port of yours for the env, its repository's envs or every env."})
		}
		yours := func(it pickItem) bool { p, ok := it.Data.(unibox.EnvPort); return ok && !p.Team }
		keys := []pickKey{
			{keys: "a", hint: "add", name: "Add Port…", about: "Name a port of yours - a mapping such as 8080 or 8080:3000 - for this env, its repository's envs or every env.",
				run: func(pickItem) { a.askPort(e.Name) }},
			{keys: "d", hint: "forget", name: "Forget Port…", about: "Forget a port you named and stop publishing it; the team's stay - say off at start for those.",
				when: yours, run: func(it pickItem) {
					if !yours(it) {
						a.flash("a port the team names stays - Enter says whether it is on at start, for you")
						return
					}
					p := it.Data.(unibox.EnvPort)
					a.forgetPort(p.Name, e.Name, a.yourPortScope(e.Name, p.Name))
				}},
		}
		a.showPickerWith("Ports of "+e.Name, items, pickerOptions{pack: true, explain: true, header: header, keys: keys,
			enterName: "Switch On at Start", enterAbout: "Say, for you, whether the port is published at each start of the env's sandboxes; a running one keeps its ports until it starts again.", enterHint: "at start"},
			func(it pickItem) {
				p, ok := it.Data.(unibox.EnvPort)
				if !ok {
					a.askPort(e.Name)
					return
				}
				to := "on"
				if p.Default {
					to = "off"
				}
				a.uniboxDo("Port "+p.Name+" "+to+" at start", "port", "default", p.Name, to, "-e", e.Name)
			})
	})
}

// askPort asks for a port of the person's - its name, its mapping, where
// it is named, whether it is on at each start - and names it.
func (a *App) askPort(env string) {
	const nameLabel, mappingLabel, forLabel, startLabel = "Name", "Mapping", "For", "On at each start"
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(nameLabel, "", 0, nil, nil)
	form.AddInputField(mappingLabel, "", 0, nil, nil)
	scope := addSelect(form, forLabel, portScopes, 0)
	start := addCheckbox(form, startLabel, true)
	form.AddButton("Add", func() {
		name := strings.TrimSpace(form.GetFormItemByLabel(nameLabel).(*tview.InputField).GetText())
		mapping := strings.TrimSpace(form.GetFormItemByLabel(mappingLabel).(*tview.InputField).GetText())
		if problem := portProblem(name, mapping); problem != "" {
			a.flash(problem)
			return
		}
		at, _ := scope.GetCurrentOption()
		args := append([]string{"port", "add", name, mapping}, portScopeFlags(portScopes[max(at, 0)], env)...)
		if !start.IsChecked() {
			args = append(args, "--off")
		}
		a.closeModal(pageForm)
		a.uniboxDo("Adding port "+name, args...)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("New port · "+env, form, 64, 13)
}

// portMapping is what unibox takes for a mapping:
// [[HOST_IP:]HOST_PORT:]SANDBOX_PORT[/PROTOCOL].
var portMapping = regexp.MustCompile(`^(?:(?:[0-9.]+:)?[0-9]+:)?[0-9]+(?:/(?:tcp|udp))?$`)

// portProblem is what is wrong with a port as typed, "" when nothing is;
// unibox checks it again, this only spares a round trip for a typo.
func portProblem(name, mapping string) string {
	switch {
	case name == "":
		return "enter the port's name, such as api or vite"
	case strings.ContainsAny(name, " \t/="):
		return "a port's name is one word, without / or ="
	case mapping == "":
		return "enter the mapping: 3000 takes any free host port, 8080:3000 host port 8080"
	case !portMapping.MatchString(mapping):
		return "a mapping is [[HOST_IP:]HOST_PORT:]SANDBOX_PORT[/PROTOCOL], such as 3000 or 8080:3000"
	}
	return ""
}

// forgetPort asks, then forgets a port of the person's where they named it.
func (a *App) forgetPort(name, env, scope string) {
	where := map[string]string{"yours": "env " + env, "yours-all": "every env", "yours-env-repo": "every env of " + env + "'s repository"}[scope]
	a.confirmWith("Forget port", fmt.Sprintf("Forget your port %s, named for %s?\n\nA sandbox publishing it stops.", name, where), "Forget", nil, func() {
		a.closeModal(pageConfirm)
		a.uniboxDo("Forgetting port "+name, append([]string{"port", "rm", name}, portScopeFlags(scope, env)...)...)
	})
}

// ----------------------------------------------- the whole configuration

// yourConfig is what the overview reads besides the overview of the
// screen: unibox's configuration afresh, every env's values and the key
// each env's repository is named under in env_repos.
type yourConfig struct {
	config   unibox.Config
	secrets  []unibox.KeptSecret
	values   map[string][]unibox.Arg
	envRepos map[string][]string
}

// yourSection is one tab of the overview: a kind of thing the person said,
// as a table of its own, with what can be added to it.
type yourSection struct {
	name  string
	heads []string
	rows  []yourRow
	// empty says what the tab would hold, while it holds nothing.
	empty string
	// add is the tab's a, nil where nothing is added from here.
	add *pickKey
}

// width is the widest the section's table is laid out, or its word
// while it is empty.
func (s yourSection) width() int {
	if len(s.rows) == 0 {
		return tview.TaggedStringWidth(s.empty)
	}
	table := make([][]string, len(s.rows))
	for i, r := range s.rows {
		table[i] = r.cells
	}
	header, labels := pickTable(s.heads, table)
	w := tview.TaggedStringWidth(header)
	for _, l := range labels {
		w = max(w, tview.TaggedStringWidth(l))
	}
	return w
}

// yourRow is one thing the person said: its cells, a sentence about it,
// where it is managed, and how it is forgotten - nil where it cannot be
// from here.
type yourRow struct {
	cells  []string
	about  string
	open   func()
	forget *yourForget
}

// yourForget is a forget asked first, then run as a unibox command.
type yourForget struct {
	title, question, job string
	args                 []string
}

// showYourConfig lists everything the person said to unibox, beside what
// the teams' files say, a tab for each kind of thing, opened on tab at.
func (a *App) showYourConfig(at int) {
	c, err := a.uniboxClient()
	if err != nil {
		a.flash(err.Error())
		return
	}
	a.withSandboxes(func() {
		envs := a.ubx.overview.Envs
		var got yourConfig
		a.loadThen("Reading your Unibox configuration", func(step func(string)) (string, error) {
			var err error
			if got.config, err = c.Config(); err != nil {
				return "", err
			}
			if got.secrets, err = c.KeptSecrets(); err != nil {
				return "", err
			}
			got.values = map[string][]unibox.Arg{}
			got.envRepos = map[string][]string{}
			for _, e := range envs {
				if e.Error != "" {
					continue
				}
				step("Reading what " + e.Name + " asks of you")
				if got.values[e.Name], err = c.Args(e.Name); err != nil {
					return "", err
				}
				ec := got.config.EnvConfigs[e.Name]
				if key := envRepoKey(ec.Source, firstOf(ec.Repo, e.Repo)); key != "" {
					got.envRepos[key] = append(got.envRepos[key], e.Name)
				}
			}
			return "", nil
		}, func(string) {
			a.showYourSection(a.yourSections(got), at)
		})
	})
}

// showYourSection is the overview on one of its tabs.
func (a *App) showYourSection(sections []yourSection, at int) {
	at = min(max(at, 0), len(sections)-1)
	sec := sections[at]
	tabs := &pickTabs{at: at, open: func(to int) { a.showYourSection(sections, to) }}
	for _, s := range sections {
		tabs.names = append(tabs.names, s.name)
		tabs.counts = append(tabs.counts, len(s.rows))
		tabs.rows = max(tabs.rows, len(s.rows), 1)
		tabs.width = max(tabs.width, s.width())
	}
	header := ""
	var items []pickItem
	if len(sec.rows) == 0 {
		items = []pickItem{{Label: tag(colMuted) + esc(sec.empty) + tagEnd, About: sec.empty}}
	} else {
		table := make([][]string, len(sec.rows))
		for i, r := range sec.rows {
			table[i] = r.cells
		}
		var labels []string
		header, labels = pickTable(sec.heads, table)
		for i, r := range sec.rows {
			items = append(items, pickItem{Label: labels[i], About: r.about, Data: r})
		}
	}
	row := func(it pickItem) (yourRow, bool) { r, ok := it.Data.(yourRow); return r, ok }
	keys := []pickKey{}
	if sec.add != nil {
		keys = append(keys, *sec.add)
	}
	keys = append(keys, pickKey{keys: "d", hint: "forget", name: "Forget…", about: "Take back what you said, where you said it; the teams' word, or Unibox's default, stands again.",
		when: func(it pickItem) bool { r, ok := row(it); return ok && r.forget != nil },
		run: func(it pickItem) {
			r, ok := row(it)
			if !ok || r.forget == nil {
				a.flash("this one is not forgotten from here - Enter opens where it is managed")
				return
			}
			f := r.forget
			a.confirmWith(f.title, f.question, "Forget", nil, func() {
				a.closeModal(pageConfirm)
				a.uniboxDoThen(f.job, func() { a.showYourConfig(at) }, f.args...)
			})
		}})
	a.showPickerWith("Your Unibox configuration", items, pickerOptions{wide: true, explain: true, header: header, keys: keys, tabs: tabs,
		enterName: "Manage…", enterAbout: "Open the dialog where this is managed: the settings, an env's values, ports, index or tokens, a sandbox's network.", enterHint: "manage"},
		func(it pickItem) {
			r, ok := row(it)
			switch {
			case !ok && sec.add != nil:
				sec.add.run(it)
			case ok && r.open != nil:
				r.open()
			case ok:
				a.note("this is written in Unibox's config.yaml - unibox config path names it")
			}
		})
}

// Cells of the overview's tables, each in the colour of what it says.
func nameCell(s string) string  { return tag(role("sandboxes.task")) + esc(s) + tagEnd }
func valueCell(s string) string { return tag(colText) + esc(s) + tagEnd }
func quietCell(s string) string { return tag(colMuted) + esc(s) + tagEnd }

// stateCell is a cell that says on or off: on in the colour of what runs.
func stateCell(s string, on bool) string {
	if on {
		return tag(role("sandboxes.running")) + esc(s) + tagEnd
	}
	return tag(role("sandboxes.stopped")) + esc(s) + tagEnd
}

// scopeWords are a scope as the FOR column says it.
func scopeWords(scope string) string {
	switch {
	case strings.HasPrefix(scope, "env "):
		return strings.TrimPrefix(scope, "env ")
	case strings.HasPrefix(scope, "repo "):
		return "envs of " + strings.TrimPrefix(scope, "repo ")
	}
	return scope
}

// yourSections lays out what was read as the overview's tabs.
func (a *App) yourSections(got yourConfig) []yourSection {
	cfg := got.config
	o := a.ubx.overview
	envNamed := func(name string) (unibox.Env, bool) {
		if e := o.Env(name); e != nil {
			return *e, true
		}
		return unibox.Env{}, false
	}
	// anEnv is an env to manage a scope's things through: the env itself,
	// one of the env repository's, else the context's or the default.
	anEnv := func(scope string) (unibox.Env, bool) {
		switch {
		case strings.HasPrefix(scope, "env "):
			return envNamed(strings.TrimPrefix(scope, "env "))
		case strings.HasPrefix(scope, "repo "):
			for _, name := range got.envRepos[strings.TrimPrefix(scope, "repo ")] {
				if e, ok := envNamed(name); ok {
					return e, true
				}
			}
			return unibox.Env{}, false
		}
		names := []string{o.Context.Set.Env, o.Default, cfg.DefaultEnv}
		if s, ok := o.Sandbox(o.Context.Set.Sandbox); ok {
			names = append([]string{s.Env}, names...)
		}
		for _, name := range names {
			if e, ok := envNamed(name); ok {
				return e, true
			}
		}
		if len(o.Envs) > 0 {
			return o.Envs[0], true
		}
		return unibox.Env{}, false
	}
	through := func(scope string, open func(unibox.Env)) func() {
		return func() {
			e, ok := anEnv(scope)
			if !ok {
				a.flash("Unibox knows no env of " + scopeWords(scope) + " to manage it through")
				return
			}
			open(e)
		}
	}
	// envFlags are -e with an env of a scope, and the flag the scope takes.
	envFlags := func(scope, kind string) ([]string, bool) {
		e, ok := anEnv(scope)
		if !ok {
			return nil, false
		}
		switch kind {
		case "yours-all":
			return []string{"-e", e.Name, "-g"}, true
		case "yours-env-repo":
			return []string{"-e", e.Name, "--env-repo"}, true
		}
		return []string{"-e", e.Name}, true
	}
	defaultEnv, haveEnv := anEnv("every env")
	addKey := func(name, about string, run func(unibox.Env)) *pickKey {
		if !haveEnv {
			return nil
		}
		return &pickKey{keys: "a", hint: "add", name: name, about: about, run: func(pickItem) { run(defaultEnv) }}
	}

	settings := yourSection{name: "Settings", heads: []string{"SETTING", "VALUE", "FOR"},
		empty: "Nothing set - Unibox's defaults stand; U changes them."}
	if cfg.DefaultEnv != "" {
		settings.rows = append(settings.rows, yourRow{cells: []string{nameCell("default env"), valueCell(cfg.DefaultEnv), quietCell("Unibox")},
			about: "The env a sandbox is created from when none is named.", open: a.showUniboxSettings,
			forget: &yourForget{"Forget setting", "Forget the default env? Unibox asks again which env, or takes the only one.", "Forgetting the default env", []string{"config", "unset", "default_env"}}})
	}
	if ctx := o.Context; ctx.Name() != "" {
		settings.rows = append(settings.rows, yourRow{cells: []string{nameCell("context"), valueCell(contextWords(ctx)), quietCell("Unibox")},
			about:  "The sandbox Unibox commands - and Sandbox with… across unagit - work in when they are not told.",
			open:   func() { a.sandboxes.focus(a.sandboxes.env) },
			forget: &yourForget{"Clear context", "Forget the context, " + contextWords(ctx) + "?", "Clearing the context", []string{"context", "clear"}}})
	}
	for _, sw := range uniboxSwitches {
		v := sw.get(cfg)
		if v == nil {
			continue
		}
		name := strings.ToLower(sw.label[:1]) + sw.label[1:]
		settings.rows = append(settings.rows, yourRow{cells: []string{nameCell(name), stateCell(switchWord(v, sw.unset), *v), quietCell("Unibox")},
			about: sw.label + " - " + sw.key + " in Unibox's config.yaml.", open: a.showUniboxSettings,
			forget: &yourForget{"Forget setting", "Forget what you said of " + name + "? Unibox's default stands again.", "Forgetting " + name, []string{"config", "unset", sw.key}}})
	}

	// The values: what each env asks for that the person gave, once for a
	// value given for every env or every env of a repository.
	values := yourSection{name: "Values", heads: []string{"NAME", "VALUE", "FOR"},
		empty: "No value of yours - a sets one; what an env asks for is under V on it."}
	values.add = addKey("New Value…", "Set a value of your own by name, for an env, its repository's envs or every env.",
		func(e unibox.Env) { a.askValue(valueRow{env: e.Name}, true) })
	seen := map[string]bool{}
	for _, env := range sortedKeys(got.values) {
		for _, v := range got.values[env] {
			scope := ""
			switch v.From {
			case "yours", "undeclared":
				scope = "env " + env
			case "global":
				scope = "every env"
			case "env-repo":
				scope = "envs of " + env + "'s repository"
			default:
				continue
			}
			if seen[v.Name+"\x00"+scope] {
				continue
			}
			seen[v.Name+"\x00"+scope] = true
			about := firstOf(v.Description, "A value of yours") + "."
			if v.From == "undeclared" {
				about += " The env no longer asks for it."
			}
			value := valueCell(v.Value)
			if v.Value == "" {
				value = quietCell("empty")
			}
			values.rows = append(values.rows, yourRow{cells: []string{nameCell(v.Name), value, quietCell(scopeWords(scope))}, about: about,
				open: through("env "+env, a.showValues),
				forget: &yourForget{"Remove value", fmt.Sprintf("Remove your %s, set for %s?", v.Name, scopeWords(scope)), "Removing " + v.Name,
					append([]string{"var", "rm", v.Name}, valueScope(v.From, env)...)}})
		}
	}

	// Ports, repositories and instructions, by scope.
	ports := yourSection{name: "Ports", heads: []string{"NAME", "MAPPING", "AT START", "FOR"},
		empty: "No port of yours - a names one; an env's are under p on it."}
	ports.add = addKey("Add Port…", "Name a port of yours - a mapping such as 8080 or 8080:3000 - for an env, its repository's envs or every env.",
		func(e unibox.Env) { a.askPort(e.Name) })
	repos := yourSection{name: "Repositories", heads: []string{"NAME", "ADDRESS", "TAKEN", "FOR"},
		empty: "No repository of yours - a names a clone; Add to Unibox Index… on repositories does too."}
	repos.add = addKey("Add Repository…", "Name a clone of yours for an env's tasks, mounted where it is.", a.pickIndexSource)
	type scoped struct {
		scope, kind string
		cfg         unibox.ScopeConfig
	}
	scopes := []scoped{{"every env", "yours-all", unibox.ScopeConfig{Ports: cfg.Ports, Repos: cfg.Repos, Instructions: cfg.Instructions}}}
	for _, key := range sortedKeys(cfg.EnvRepos) {
		scopes = append(scopes, scoped{"repo " + key, "yours-env-repo", cfg.EnvRepos[key]})
	}
	for _, name := range sortedKeys(cfg.EnvConfigs) {
		scopes = append(scopes, scoped{"env " + name, "yours", cfg.EnvConfigs[name].ScopeConfig})
	}
	for _, sc := range scopes {
		for _, name := range sortedKeys(sc.cfg.Ports) {
			p := sc.cfg.Ports[name]
			start := stateCell("off", false)
			if p.Default {
				start = stateCell("on", true)
			}
			mapping := valueCell(p.Port)
			about := "A port you named for " + scopeWords(sc.scope) + "."
			if p.Port == "" {
				mapping = quietCell("the team's")
				about = "A port the team names, which you switched at start for " + scopeWords(sc.scope) + "."
			}
			r := yourRow{cells: []string{nameCell(name), mapping, start, quietCell(scopeWords(sc.scope))}, about: about,
				open: through(sc.scope, a.showEnvPorts)}
			if flags, ok := envFlags(sc.scope, sc.kind); ok {
				if p.Port != "" {
					r.forget = &yourForget{"Forget port", "Forget your port " + name + ", named for " + scopeWords(sc.scope) + "?\n\nA sandbox publishing it stops.", "Forgetting port " + name,
						append([]string{"port", "rm", name}, flags...)}
				} else if sc.kind == "yours" {
					r.forget = &yourForget{"Forget port", "Take back what you said of " + name + "? The team's word stands.", "Taking back " + name,
						append([]string{"port", "default", name, "-"}, flags...)}
				}
			}
			ports.rows = append(ports.rows, r)
		}
		for _, name := range sortedKeys(sc.cfg.Repos) {
			repo := sc.cfg.Repos[name]
			taken := stateCell(firstOf(repo.Default, "off"), repo.Default == "always" || repo.Default == "on")
			r := yourRow{cells: []string{nameCell(name), valueCell(repo.URL), taken, quietCell(scopeWords(sc.scope))},
				about: firstOf(repo.Description, "A repository you named for the tasks of "+scopeWords(sc.scope)) + ".",
				open:  through(sc.scope, a.showRepoIndex)}
			if flags, ok := envFlags(sc.scope, sc.kind); ok {
				r.forget = &yourForget{"Forget repository", "Forget " + name + ", named for " + scopeWords(sc.scope) + "?", "Forgetting " + name,
					append([]string{"repo", "rm", name}, flags...)}
			}
			repos.rows = append(repos.rows, r)
		}
		if text := strings.TrimSpace(sc.cfg.Instructions); text != "" {
			first, _, _ := strings.Cut(text, "\n")
			settings.rows = append(settings.rows, yourRow{cells: []string{nameCell("instructions"), valueCell(first), quietCell(scopeWords(sc.scope))},
				about: "What you tell every task's agents in " + scopeWords(sc.scope) + ": " + text})
		}
	}

	// The tokens sbx keeps.
	tokens := yourSection{name: "Tokens", heads: []string{"SERVICE", "KEPT", "SCOPE", "USED BY"},
		empty: "sbx keeps no token - K on an env stores what it needs."}
	for _, s := range got.secrets {
		kept := firstOf(s.Kept, "stored")
		if a.vault != nil {
			if _, ok := a.vault.Credential(s.Service); ok {
				kept += " · in unagit"
			}
		}
		used := quietCell(strings.Join(s.UsedBy, ", "))
		about := "A token sbx keeps"
		if len(s.UsedBy) > 0 {
			about += ", used by " + strings.Join(s.UsedBy, ", ")
		}
		if !s.Needed {
			used = quietCell("no env needs it")
			about += "; no env needs it now"
		}
		about += ". Its value is never shown."
		args := []string{"secret", "rm", s.Service}
		switch {
		case strings.HasPrefix(s.Scope, "env "):
			args = append(args, "--scope", "env", "-e", strings.TrimPrefix(s.Scope, "env "))
		case strings.HasPrefix(s.Scope, "sandbox "):
			args = append(args, "--scope", "sandbox", "-s", strings.TrimPrefix(s.Scope, "sandbox "))
		default:
			args = append(args, "--scope", "global")
			if len(s.UsedBy) > 0 {
				args = append(args, "-e", s.UsedBy[0])
			}
		}
		env := ""
		if strings.HasPrefix(s.Scope, "env ") {
			env = strings.TrimPrefix(s.Scope, "env ")
		} else if len(s.UsedBy) > 0 {
			env = s.UsedBy[0]
		}
		tokens.rows = append(tokens.rows, yourRow{cells: []string{nameCell(s.Service), stateCell(kept, true), quietCell(firstOf(s.Scope, "global")), used}, about: about,
			open:   through("env "+env, a.showSecrets),
			forget: &yourForget{"Remove token", "Have sbx forget the " + firstOf(s.Scope, "global") + " token of " + s.Service + "?\n\nEvery sandbox that takes it goes without until it is stored again.", "Removing the token of " + s.Service, args}})
	}

	// The sandboxes' network rules.
	network := yourSection{name: "Network", heads: []string{"HOST", "RULE", "SANDBOX"},
		empty: "No rule of yours - f on a sandbox allows or denies a host."}
	for _, name := range sortedKeys(cfg.Sandboxes) {
		for _, rule := range cfg.Sandboxes[name].Network {
			decision, host, ink := "allow", rule.Allow, "sandboxes.allowed"
			if rule.Deny != "" {
				decision, host, ink = "deny", rule.Deny, "sandboxes.denied"
			}
			network.rows = append(network.rows, yourRow{cells: []string{nameCell(host), tag(role(ink)) + decision + tagEnd, quietCell(name)},
				about: fmt.Sprintf("You %s %s for sandbox %s, over its env's rules.", map[string]string{"allow": "let it reach", "deny": "keep it from"}[decision], host, name),
				open: func() {
					s, ok := o.Sandbox(name)
					if !ok {
						a.flash("Unibox has no sandbox " + name + " now - the rule waits for one of that name")
						return
					}
					a.showSandboxNetwork(sandboxRow{kind: rowSandbox, sandbox: s, made: true})
				},
				forget: &yourForget{"Remove rule", "Take back your rule for " + host + " in sandbox " + name + "?", "Removing the rule for " + host,
					[]string{"network", "rm", host, "-s", name}}})
		}
	}
	return []yourSection{settings, values, ports, repos, tokens, network}
}

// scpAddress is a git address written as scp writes one, git@host:path.
var scpAddress = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+@)?([A-Za-z0-9.-]+):([^/\\][^:]*)$`)

// envRepoKey names the repository an env's files are in as unibox keys
// env_repos: its host and path, from where unibox cloned it or the
// checkout's origin; "" when neither says.
func envRepoKey(source, repo string) string {
	for _, address := range []string{source, gitOrigin(repo)} {
		if address == "" {
			continue
		}
		var host, path string
		if m := scpAddress.FindStringSubmatch(address); m != nil && !strings.Contains(address, "://") {
			host, path = m[1], m[2]
		} else if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
			host, path = u.Hostname(), u.Path
		}
		path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
		if host != "" && path != "" {
			return strings.ToLower(host) + "/" + path
		}
	}
	return ""
}

// gitOrigin is a checkout's origin, "" when it has none.
func gitOrigin(dir string) string {
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
