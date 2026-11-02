package ui

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
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

// yourSetting is one thing the person said to unibox, as the overview
// lists it: what kind of thing, its name, its value, what it is for, and
// where it is managed and how it is forgotten.
type yourSetting struct {
	kind, name, value, scope, about string
	open                            func()
	// forget takes it back; nil where it cannot be from here.
	forget func()
}

// yourConfig is what the overview reads besides the overview of the
// screen: unibox's configuration afresh, every env's values and the key
// each env's repository is named under in env_repos.
type yourConfig struct {
	config   unibox.Config
	secrets  []unibox.KeptSecret
	values   map[string][]unibox.Arg
	envRepos map[string][]string
}

// showYourConfig lists everything the person said to unibox, beside what
// the teams' files say, in one place.
func (a *App) showYourConfig() {
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
			a.showYourSettings(a.yourSettings(got))
		})
	})
}

// showYourSettings is the overview's picker.
func (a *App) showYourSettings(settings []yourSetting) {
	table := make([][]string, len(settings))
	items := make([]pickItem, len(settings))
	for i, s := range settings {
		table[i] = []string{tag(colMuted) + esc(s.kind) + tagEnd, tag(role("sandboxes.task")) + esc(s.name) + tagEnd,
			tag(colText) + esc(s.value) + tagEnd, tag(colMuted) + esc(s.scope) + tagEnd}
		items[i] = pickItem{About: s.about, Data: s}
	}
	header, labels := pickTable([]string{"KIND", "NAME", "VALUE", "FOR"}, table)
	for i := range items {
		items[i].Label = labels[i]
	}
	if len(items) == 0 {
		a.note("you have said nothing to Unibox yet - the teams' files say it all")
		return
	}
	forgettable := func(it pickItem) bool { s, ok := it.Data.(yourSetting); return ok && s.forget != nil }
	keys := []pickKey{
		{keys: "d", hint: "forget", name: "Forget…", about: "Take back what you said, where you said it; the teams' word, or Unibox's default, stands again.",
			when: forgettable, run: func(it pickItem) {
				s := it.Data.(yourSetting)
				if s.forget == nil {
					a.flash("this one is not forgotten from here - Enter opens where it is managed")
					return
				}
				s.forget()
			}},
	}
	a.showPickerWith("Your Unibox configuration", items, pickerOptions{wide: true, explain: true, header: header, keys: keys,
		enterName: "Manage…", enterAbout: "Open the dialog where this is managed: the settings, the env's values, ports, index or tokens, the sandbox's network.", enterHint: "manage"},
		func(it pickItem) {
			if s, ok := it.Data.(yourSetting); ok && s.open != nil {
				s.open()
			}
		})
}

// yourSettings lays out what was read as the overview's rows: the
// settings, then values, ports, repositories, instructions, network rules
// and tokens, each by scope - every env, an env repository's, an env's.
func (a *App) yourSettings(got yourConfig) []yourSetting {
	cfg := got.config
	o := a.ubx.overview
	var out []yourSetting
	envNamed := func(name string) (unibox.Env, bool) {
		if e := o.Env(name); e != nil {
			return *e, true
		}
		return unibox.Env{}, false
	}
	// anEnv is an env to manage a scope's things through: the env itself,
	// one of the env repository's, the context's or the default for every
	// env.
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
		for _, name := range []string{o.Context.Set.Env, o.Default, cfg.DefaultEnv} {
			if e, ok := envNamed(name); ok {
				return e, true
			}
		}
		if s, ok := o.Sandbox(o.Context.Set.Sandbox); ok {
			if e, ok := envNamed(s.Env); ok {
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
				a.flash("Unibox knows no env of " + strings.TrimPrefix(scope, "repo ") + " to manage it through")
				return
			}
			open(e)
		}
	}

	// The settings.
	if cfg.DefaultEnv != "" {
		out = append(out, yourSetting{kind: "setting", name: "default env", value: cfg.DefaultEnv, scope: "Unibox",
			about: "The env a sandbox is created from when none is named.", open: a.showUniboxSettings,
			forget: a.forgetConfig("default_env", "the default env")})
	}
	if ctx := o.Context; ctx.Name() != "" {
		out = append(out, yourSetting{kind: "setting", name: "context", value: contextWords(ctx), scope: "Unibox",
			about: "The sandbox Unibox commands - and Sandbox with… across unagit - work in when they are not told.",
			open:  func() { a.sandboxes.focus(a.sandboxes.env) },
			forget: func() {
				a.confirmWith("Clear context", "Forget the context, "+contextWords(ctx)+"?", "Clear", nil, func() {
					a.closeModal(pageConfirm)
					a.uniboxDo("Clearing the context", "context", "clear")
				})
			}})
	}
	for _, sw := range uniboxSwitches {
		if v := sw.get(cfg); v != nil {
			out = append(out, yourSetting{kind: "setting", name: strings.ToLower(sw.label[:1]) + sw.label[1:], value: switchWord(v, sw.unset), scope: "Unibox",
				about: sw.label + " - " + sw.key + " in Unibox's config.yaml.", open: a.showUniboxSettings,
				forget: a.forgetConfig(sw.key, strings.ToLower(sw.label[:1])+sw.label[1:])})
		}
	}

	// The values: what each env asks for that the person gave, once for a
	// value given for every env or every env of a repository.
	seen := map[string]bool{}
	var envNames []string
	for name := range got.values {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)
	for _, env := range envNames {
		for _, v := range got.values[env] {
			scope := ""
			switch v.From {
			case "yours", "undeclared":
				scope = "env " + env
			case "global":
				scope = "every env"
			case "env-repo":
				scope = "repo of " + env
			default:
				continue
			}
			if seen[v.Name+"\x00"+scope] {
				continue
			}
			seen[v.Name+"\x00"+scope] = true
			v, env := v, env
			about := firstOf(v.Description, "A value of yours") + "."
			if v.From == "undeclared" {
				about += " The env no longer asks for it."
			}
			out = append(out, yourSetting{kind: "value", name: v.Name, value: v.Value, scope: scope, about: about,
				open: through("env "+env, a.showValues),
				forget: func() {
					a.confirmWith("Remove value", fmt.Sprintf("Remove your %s (%s)?", v.Name, scope), "Remove", nil, func() {
						a.closeModal(pageConfirm)
						a.uniboxDo("Removing "+v.Name, append([]string{"var", "rm", v.Name}, valueScope(v.From, env)...)...)
					})
				}})
		}
	}

	// Ports, repositories and instructions, by scope.
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
		sc := sc
		for _, name := range sortedKeys(sc.cfg.Ports) {
			p := sc.cfg.Ports[name]
			value := firstOf(p.Port, "the team's")
			if p.Default {
				value += " · on at start"
			} else {
				value += " · off at start"
			}
			s := yourSetting{kind: "port", name: name, value: value, scope: sc.scope,
				about: "A port you named, or one of the team's you switched, for " + sc.scope + ".",
				open:  through(sc.scope, a.showEnvPorts)}
			if p.Port != "" {
				s.forget = func() {
					e, ok := anEnv(sc.scope)
					if !ok {
						a.flash("Unibox knows no env of " + strings.TrimPrefix(sc.scope, "repo ") + " to forget it through")
						return
					}
					a.forgetPort(name, e.Name, sc.kind)
				}
			} else if e, ok := anEnv(sc.scope); ok && sc.kind == "yours" {
				s.forget = func() { a.uniboxDo("Taking back what you said of "+name, "port", "default", name, "-", "-e", e.Name) }
			}
			out = append(out, s)
		}
		for _, name := range sortedKeys(sc.cfg.Repos) {
			r := sc.cfg.Repos[name]
			value := r.URL
			if r.Default != "" {
				value += " · " + r.Default
			}
			out = append(out, yourSetting{kind: "repository", name: name, value: value, scope: sc.scope,
				about: firstOf(r.Description, "A repository you named for the tasks of "+sc.scope) + ".",
				open:  through(sc.scope, a.showRepoIndex),
				forget: func() {
					e, ok := anEnv(sc.scope)
					if !ok {
						a.flash("Unibox knows no env of " + strings.TrimPrefix(sc.scope, "repo ") + " to forget it through")
						return
					}
					args := []string{"repo", "rm", name, "-e", e.Name}
					switch sc.kind {
					case "yours-all":
						args = append(args, "-g")
					case "yours-env-repo":
						args = append(args, "--env-repo")
					}
					a.confirmWith("Forget repository", "Forget "+name+", named for "+sc.scope+"?", "Forget", nil, func() {
						a.closeModal(pageConfirm)
						a.uniboxDo("Forgetting "+name, args...)
					})
				}})
		}
		if text := strings.TrimSpace(sc.cfg.Instructions); text != "" {
			first, _, _ := strings.Cut(text, "\n")
			out = append(out, yourSetting{kind: "instructions", name: "for agents", value: first, scope: sc.scope,
				about: "What you tell every task's agents in " + sc.scope + ", under instructions in Unibox's config.yaml: " + text})
		}
	}

	// The sandboxes' network rules.
	for _, name := range sortedKeys(cfg.Sandboxes) {
		for _, rule := range cfg.Sandboxes[name].Network {
			decision, host := "allow", rule.Allow
			if rule.Deny != "" {
				decision, host = "deny", rule.Deny
			}
			name := name
			out = append(out, yourSetting{kind: "network", name: host, value: decision, scope: "sandbox " + name,
				about: fmt.Sprintf("You %s %s for sandbox %s, over its env's rules.", map[string]string{"allow": "let it reach", "deny": "keep it from"}[decision], host, name),
				open: func() {
					s, ok := o.Sandbox(name)
					if !ok {
						a.flash("Unibox has no sandbox " + name + " now - the rule waits for one of that name")
						return
					}
					a.showSandboxNetwork(sandboxRow{kind: rowSandbox, sandbox: s, made: true})
				}})
		}
	}

	// The tokens sbx keeps.
	for _, s := range got.secrets {
		s := s
		// sbx says stored, or the command it runs for the token.
		value := firstOf(s.Kept, "stored")
		if value == "sbx" {
			value = "kept by sbx"
		}
		if a.vault != nil {
			if _, ok := a.vault.Credential(s.Service); ok {
				value += " · in unagit"
			}
		}
		about := "A token sbx keeps"
		if len(s.UsedBy) > 0 {
			about += ", used by " + strings.Join(s.UsedBy, ", ")
		}
		if !s.Needed {
			about += "; no env needs it now"
		}
		about += ". Its value is never shown."
		out = append(out, yourSetting{kind: "token", name: s.Service, value: value, scope: s.Scope, about: about,
			open: func() {
				env := ""
				if strings.HasPrefix(s.Scope, "env ") {
					env = strings.TrimPrefix(s.Scope, "env ")
				} else if len(s.UsedBy) > 0 {
					env = s.UsedBy[0]
				}
				through("env "+env, a.showSecrets)()
			},
			forget: func() { a.forgetToken(s) }})
	}
	return out
}

// forgetConfig is the forget of one of unibox's settings: config unset.
func (a *App) forgetConfig(key, what string) func() {
	return func() {
		a.confirmWith("Forget setting", "Forget what you said of "+what+"? Unibox's default stands again.", "Forget", nil, func() {
			a.closeModal(pageConfirm)
			a.uniboxDo("Forgetting "+what, "config", "unset", key)
		})
	}
}

// forgetToken has sbx forget a token where it is kept: globally, or an
// env's or a sandbox's own.
func (a *App) forgetToken(s unibox.KeptSecret) {
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
	a.confirmWith("Remove token", "Have sbx forget the "+firstOf(s.Scope, "global")+" token of "+s.Service+"?\n\nEvery sandbox that takes it goes without until it is stored again.", "Remove", nil, func() {
		a.closeModal(pageConfirm)
		a.uniboxDo("Removing the token of "+s.Service, args...)
	})
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
