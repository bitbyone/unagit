package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/unibox"
)

// The Sandboxes screen is one UI over unibox: the envs a team describes as
// sbx's env files, the sandbox sbx creates from each, and the tasks in each
// sandbox. Everything on it is read from unibox's --json and everything
// done on it is a unibox command, so it shows what unibox itself would say
// and changes nothing unibox would not. unagit reads none of unibox's files
// - not an sbxenv file, not its state.

const pageSandboxes = "sandboxes"

// uniboxOn says whether the Sandboxes tab is there: what the user chose,
// or, unset, whether unibox is installed.
func (a *App) uniboxOn() bool {
	if on := a.cfg.Integrations.Unibox; on != nil {
		return *on
	}
	_, err := a.executable("unibox")
	return err == nil
}

// uniboxClient is the unibox to run, or why there is none.
func (a *App) uniboxClient() (*unibox.Client, error) {
	bin, err := a.executable("unibox")
	if err != nil {
		return nil, fmt.Errorf("unibox is not on PATH - install it, see Settings › Integrations")
	}
	return unibox.New(bin), nil
}

// uniboxState is what the screen last read of unibox.
type uniboxState struct {
	overview unibox.Overview
	err      error
	read     bool
	loading  bool
	at       time.Time
	// details are what a row's detail shows besides the overview - an
	// env's values and tokens, a sandbox's ports - by row key, read
	// when the row is first looked at and dropped with every refresh.
	details map[string]*sandboxDetail
}

// sandboxDetail is what was read for one row's detail.
type sandboxDetail struct {
	loading bool
	err     error
	// An env's: what it asks of the person, the tokens it needs and the
	// repositories its tasks are given by name.
	args    []unibox.Arg
	secrets []unibox.Secret
	repos   []unibox.IndexedRepo
	// A sandbox's: its ports, its network rules and its resources.
	ports     []unibox.Port
	network   unibox.Network
	resources unibox.Resources
}

type sandboxRowKind int

const (
	rowEnv sandboxRowKind = iota
	rowSandbox
	rowTask
)

// sandboxRow is one row of the tree: an env, the sandbox made from it, or
// a task in that. A row carries what is above it, so its actions need
// nothing else; made says whether the env's sandbox exists.
type sandboxRow struct {
	kind    sandboxRowKind
	env     unibox.Env
	sandbox unibox.Sandbox
	made    bool
	task    unibox.Task
}

// key names a row across refreshes, for the cursor and the details.
func (r sandboxRow) key() string {
	switch r.kind {
	case rowSandbox:
		return "s:" + r.sandbox.Name
	case rowTask:
		return "t:" + r.task.ID
	}
	return "e:" + r.env.Name
}

// working says whether an agent unibox started works on the task now.
func (r sandboxRow) working() bool {
	_, ok := r.sandbox.RunOf(r.task.ID)
	return ok
}

// sandboxName is the sandbox a row stands for: its own, or the one its env
// makes.
func (r sandboxRow) sandboxName() string {
	if r.sandbox.Name != "" {
		return r.sandbox.Name
	}
	return firstOf(r.env.Sandbox, r.env.Name)
}

// sandboxRows lays the overview out as the tree: each env, its sandbox,
// the sandbox's tasks by name. A sandbox whose env unibox no longer has
// still has its env's row, saying so.
func sandboxRows(o unibox.Overview) []sandboxRow {
	envs := append([]unibox.Env(nil), o.Envs...)
	known := map[string]bool{}
	for _, e := range envs {
		known[e.Name] = true
	}
	for _, s := range o.Sandboxes {
		if !known[s.Env] {
			known[s.Env] = true
			envs = append(envs, unibox.Env{Name: s.Env, Sandbox: s.Name, Agent: s.Agent, Error: "unibox has no env of this name any more"})
		}
	}
	var rows []sandboxRow
	for _, e := range envs {
		s, made := sandboxOfEnv(o, e)
		rows = append(rows, sandboxRow{kind: rowEnv, env: e, sandbox: s, made: made})
		if !made {
			continue
		}
		for _, box := range o.Sandboxes {
			if box.Env != e.Name {
				continue
			}
			rows = append(rows, sandboxRow{kind: rowSandbox, env: e, sandbox: box, made: true})
			tasks := append([]unibox.Task(nil), box.Tasks...)
			sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Name() < tasks[j].Name() })
			for _, t := range tasks {
				rows = append(rows, sandboxRow{kind: rowTask, env: e, sandbox: box, made: true, task: t})
			}
		}
	}
	return rows
}

// sandboxOfEnv is the sandbox an env made: the one of its name, else any
// unibox keeps under the env.
func sandboxOfEnv(o unibox.Overview, e unibox.Env) (unibox.Sandbox, bool) {
	if s, ok := o.Sandbox(firstOf(e.Sandbox, e.Name)); ok && s.Env == e.Name {
		return s, true
	}
	for _, s := range o.Sandboxes {
		if s.Env == e.Name {
			return s, true
		}
	}
	return unibox.Sandbox{}, false
}

// refreshSandboxes reads unibox again, behind the interface. It runs on
// the event loop.
func (a *App) refreshSandboxes() {
	if a.ubx.loading {
		return
	}
	c, err := a.uniboxClient()
	if err != nil {
		a.ubx = uniboxState{err: err, read: true, at: time.Now()}
		a.sandboxesPane.reload()
		return
	}
	a.ubx.loading = true
	job := a.startJob("Reading the sandboxes")
	go func() {
		o, err := c.Overview()
		a.tv.QueueUpdateDraw(func() {
			a.endJob(job)
			a.ubx = uniboxState{overview: o, err: err, read: true, at: time.Now(), details: map[string]*sandboxDetail{}}
			a.sandboxRows = sandboxRows(o)
			a.sandboxesPane.reload()
			a.drawTabs()
		})
	}()
}

// sandboxesStale is how long what was read is shown without reading again
// when the tab comes to the front.
const sandboxesStale = 5 * time.Second

// visitSandboxes is the tab coming to the front: read again unless what is
// on screen was read a moment ago.
func (a *App) visitSandboxes() {
	if !a.ubx.read || time.Since(a.ubx.at) > sandboxesStale {
		a.refreshSandboxes()
	}
}

// sandboxRowName is a row as a title names it.
func sandboxRowName(r sandboxRow) string {
	switch r.kind {
	case rowSandbox:
		return "sandbox " + r.sandbox.Name
	case rowTask:
		return "task " + r.task.Name()
	}
	return "env " + r.env.Name
}

// sandboxHay is what a row is found by.
func sandboxHay(r sandboxRow) string {
	switch r.kind {
	case rowSandbox:
		return r.sandbox.Name + " " + r.sandbox.Env + " " + r.sandbox.State + " " + strings.Join(r.sandbox.AgentIDs(), " ")
	case rowTask:
		hay := r.task.Name() + " " + r.task.ID + " " + r.task.Sandbox + " " + r.task.Agent
		for _, repo := range r.task.Repositories {
			hay += " " + repo.Name + " " + repo.Branch
		}
		return hay
	}
	return r.env.Name + " " + r.env.Sandbox + " " + r.env.Agent
}

// agentLabel is an agent as the screen names it: unagit's name for it
// where it knows the agent, unibox's id otherwise.
func agentLabel(id string) string {
	if ag, ok := agents.ByID(id); ok {
		return ag.Name
	}
	return id
}

// sandboxStateWords are a sandbox's state as the screen says it, and the
// role it is drawn in.
func sandboxStateWords(state string) (string, string) {
	switch state {
	case "running":
		return glyphDot + " running", "sandboxes.running"
	case "stopped":
		return glyphRing + " stopped", "sandboxes.stopped"
	case "missing", "unknown", "creating":
		return glyphRing + " " + state, "sandboxes.problem"
	}
	return glyphRing + " " + state, "sandboxes.stopped"
}

// sandboxState is a row's STATE: its words and the role they are drawn in.
// An env says what stands between it and a start, else its sandbox's.
func (a *App) sandboxState(r sandboxRow) (string, string) {
	switch r.kind {
	case rowSandbox:
		return sandboxStateWords(r.sandbox.State)
	case rowTask:
		if r.working() {
			return spinnerGlyph(a.spinFrame) + " working", "sandboxes.running"
		}
		if r.task.State == "ready" {
			return glyphCheck + " ready", "sandboxes.stopped"
		}
		return glyphRing + " " + r.task.State, "sandboxes.problem"
	}
	switch {
	case r.env.Error != "":
		return "does not load", "sandboxes.problem"
	case len(r.env.Missing) > 0:
		return counted(len(r.env.Missing), "value missing", "values missing"), "sandboxes.problem"
	case !r.made:
		return "no sandbox", "sandboxes.stopped"
	}
	return sandboxStateWords(r.sandbox.State)
}

// sandboxAgents is a row's AGENT column: the agents at work in a sandbox,
// a task's own, an env's kit's.
func sandboxAgents(r sandboxRow) string {
	switch r.kind {
	case rowSandbox:
		var at []string
		for _, run := range r.sandbox.Runs {
			if run.Agent != "" {
				at = append(at, run.Agent)
			}
		}
		return strings.Join(at, ", ")
	case rowTask:
		return r.task.Agent
	}
	return r.env.Agent
}

// sandboxRepos is a task's REPOS column: its repositories by name.
func sandboxRepos(r sandboxRow) string {
	if r.kind != rowTask {
		return ""
	}
	var names []string
	for _, repo := range r.task.Repositories {
		names = append(names, repo.Name)
	}
	return strings.Join(names, ", ")
}

// sandboxBranch is a task's BRANCH column: the branch of its first
// repository, with a + when another is on a branch of its own.
func sandboxBranch(r sandboxRow) string {
	if r.kind != rowTask || len(r.task.Repositories) == 0 {
		return ""
	}
	branch := r.task.Repositories[0].Branch
	for _, repo := range r.task.Repositories[1:] {
		if repo.Branch != branch {
			return branch + " +"
		}
	}
	return branch
}

// drawSandboxTasks fills the tasks: every sandbox's, by sandbox and name.
func (a *App) drawSandboxTasks(p *pane) {
	v := a.sandboxes
	previous := p.selectedIndex()
	p.table.Clear()
	p.kept.reset()

	// The message in place of rows says what is missing and where it is set.
	if problem := a.sandboxesProblem(); problem != "" {
		p.table.SetCell(0, 0, tview.NewTableCell("").SetSelectable(false))
		p.table.SetCell(1, 0, tview.NewTableCell(" "+tag(role("sandboxes.problem"))+esc(problem)+tagEnd).SetSelectable(false).SetExpansion(1))
		return
	}

	var names, boxes, states, agentsW, repos, branches []int
	for _, r := range v.tasks {
		names = append(names, cells(r.task.Name()))
		boxes = append(boxes, cells(r.sandbox.Name))
		state, _ := a.sandboxState(r)
		states = append(states, cells(state))
		agentsW = append(agentsW, cells(agentLabel(r.task.Agent))+2)
		repos = append(repos, cells(sandboxRepos(r)))
		branches = append(branches, cells(sandboxBranch(r)))
	}
	nameCol := flexColumn("TASK", names, 10, 2)
	boxCol := flexColumn("SANDBOX", boxes, 6, 1)
	stateCol := flexColumn("STATE", states, 8, 1)
	agentCol := flexColumn("AGENT", agentsW, 7, 1)
	reposCol := flexColumn("REPOS", repos, 8, 1)
	branchCol := flexColumn("BRANCH", branches, 10, 1.5)
	reposCol.drop, boxCol.drop, agentCol.drop = 1, 2, 3
	layoutColumns(p.contentWidth()-1, fixedColumn(1), nameCol, boxCol, stateCol, agentCol, reposCol, branchCol)

	heading := role("sandboxes.header")
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(withHeadingIcons([]field{
		{width: 1},
		{text: "TASK", width: nameCol.width, colour: heading},
		{text: "SANDBOX", width: boxCol.width, colour: heading},
		{text: "STATE", width: stateCol.width, colour: heading},
		{text: "AGENT", width: agentCol.width, colour: heading},
		{text: "REPOS", width: reposCol.width, colour: heading},
		{text: "BRANCH", width: branchCol.width, colour: heading},
	}))).SetSelectable(false).SetExpansion(1))

	for i, r := range v.tasks {
		state, stateRole := a.sandboxState(r)
		agent := agentLabel(r.task.Agent)
		if icon := agentIcons[r.task.Agent]; icon != "" {
			agent = icon + " " + agent
		}
		p.table.SetCell(i+1, 0, tview.NewTableCell(rowText([]field{
			{width: 1},
			{text: r.task.Name(), width: nameCol.width, colour: role("sandboxes.task")},
			{text: r.sandbox.Name, width: boxCol.width, colour: role("sandboxes.sandbox")},
			{text: state, width: stateCol.width, colour: role(stateRole)},
			{text: agent, width: agentCol.width, colour: role("sandboxes.agent")},
			{text: sandboxRepos(r), width: reposCol.width, colour: role("sandboxes.task")},
			{text: sandboxBranch(r), width: branchCol.width, colour: role("sandboxes.branch"), shorten: shortenBranch},
		})).SetReference(i).SetExpansion(1))
	}
	if len(v.tasks) == 0 {
		empty := "No task yet - n on a sandbox or an env starts one, Sandbox with… on a repository or a worktree too."
		if p.query != "" {
			empty = "No task matches."
		}
		p.table.SetCell(1, 0, tview.NewTableCell(" "+tag(colMuted)+esc(empty)+tagEnd).SetSelectable(false).SetExpansion(1))
		return
	}
	p.selectRow(previous, 1)
}

// sandboxesProblem is why the screen has no rows: unibox missing, a
// contract not understood, no env yet - each with where to set it.
func (a *App) sandboxesProblem() string {
	switch {
	case !a.ubx.read:
		return ""
	case a.ubx.err != nil:
		return a.ubx.err.Error()
	case len(a.sandboxRows) == 0:
		return "no env yet - I sets unibox up from your team's repository of sbxenv files"
	}
	return ""
}

// readSandboxDetail reads what a row's detail shows besides the overview -
// an env's args, secrets and repositories, a sandbox's ports, network and
// resources - the first time the row is looked at, and calls then on the
// event loop once it is there.
func (a *App) readSandboxDetail(r sandboxRow, then func()) {
	if r.kind == rowTask || r.kind == rowEnv && r.env.Error != "" {
		return
	}
	key := r.key()
	if a.ubx.details == nil {
		a.ubx.details = map[string]*sandboxDetail{}
	}
	if _, ok := a.ubx.details[key]; ok {
		return
	}
	d := &sandboxDetail{loading: true}
	a.ubx.details[key] = d
	c, err := a.uniboxClient()
	if err != nil {
		d.loading, d.err = false, err
		return
	}
	go func() {
		got := &sandboxDetail{}
		var errs []error
		keep := func(err error) {
			if err != nil {
				errs = append(errs, err)
			}
		}
		if r.kind == rowEnv {
			var err error
			got.args, err = c.Args(r.env.Name)
			keep(err)
			got.secrets, err = c.Secrets(r.env.Name)
			keep(err)
			got.repos, err = c.Repos(r.env.Name)
			keep(err)
		} else {
			var err error
			got.ports, err = c.Ports(r.sandbox.Name)
			keep(err)
			got.network, err = c.Network(r.sandbox.Name)
			keep(err)
			got.resources, err = c.Resources(r.sandbox.Name)
			keep(err)
		}
		if len(errs) > 0 {
			got.err = errs[0]
		}
		a.tv.QueueUpdateDraw(func() {
			if a.ubx.details[key] != d {
				return // refreshed since
			}
			a.ubx.details[key] = got
			then()
		})
	}()
}

// sandboxDetailText is a row's configuration, as unibox reports it.
func (a *App) sandboxDetailText(r sandboxRow) string {
	d := &detailBuf{}
	extra := a.ubx.details[r.key()]
	read := extra != nil && !extra.loading && extra.err == nil
	reading := func() {
		switch {
		case extra == nil || extra.loading:
			d.sub("reading…")
		case extra.err != nil:
			d.raw(tag(role("sandboxes.problem")) + esc(extra.err.Error()) + tagEnd + "\n")
		}
	}
	problem := func(text string) string { return tag(role("sandboxes.problem")) + esc(text) + tagEnd }
	muted := func(text string) string { return tag(colMuted) + esc(text) + tagEnd }
	switch r.kind {
	case rowEnv:
		e := r.env
		d.title(e.Name)
		d.sub("env")
		if e.Error != "" {
			d.blank()
			d.raw(problem(e.Error) + "\n")
		}
		d.blank()
		state, _ := a.sandboxState(r)
		d.kv("State", esc(state))
		d.kv("Sandbox", esc(r.sandboxName())+muted(map[bool]string{true: " · made", false: " · not made yet"}[r.made]))
		d.kv("Agent", esc(agentLabel(e.Agent)))
		if e.Name == a.ubx.overview.Default {
			d.kv("Default", "new sandboxes come from it")
		}
		d.section("Files")
		for _, f := range e.Files {
			d.kv("", esc(tildePath(f)))
		}
		if e.Repo != "" {
			d.kv("Repository", esc(tildePath(e.Repo)))
		}
		if e.Source != "" {
			d.kv("Cloned from", esc(e.Source))
		}
		if len(e.Kits) > 0 {
			d.section("Kits")
			for _, k := range e.Kits {
				d.kv("", esc(k.Source))
			}
		}
		d.section("Your values")
		if read {
			for _, v := range extra.args {
				switch {
				case v.Missing():
					d.kv(v.Name, problem("missing")+muted(" · "+v.Description))
				default:
					d.kv(v.Name, esc(v.Value)+muted(" · "+v.From))
				}
			}
			if len(extra.args) == 0 {
				d.sub("it asks for none")
			}
			d.section("Tokens")
			for _, s := range extra.secrets {
				state := tag(role("sandboxes.running")) + "stored" + tagEnd
				switch {
				case !s.Stored && s.Optional:
					state = muted("not stored, optional")
				case !s.Stored:
					state = problem("missing")
				}
				if s.Scope != "" && s.Scope != "global" {
					state += muted(" · " + s.Scope)
				}
				d.kv(s.Service, state)
				if s.Description != "" {
					d.kv("", muted(s.Description))
				}
			}
			if len(extra.secrets) == 0 {
				d.sub("it needs none")
			}
			d.section("Repositories")
			for _, repo := range extra.repos {
				line := esc(repo.URL)
				if repo.Default != "" && repo.Default != "off" {
					line += muted(" · " + repo.Default)
				}
				d.kv(repo.Name, line)
			}
			if len(extra.repos) == 0 {
				d.sub("none named")
			}
		} else {
			reading()
		}
	case rowSandbox:
		s := r.sandbox
		d.title(s.Name)
		d.sub("sandbox of " + s.Env)
		state, _ := a.sandboxState(r)
		d.blank()
		d.kv("State", esc(state))
		var names []string
		for _, id := range s.AgentIDs() {
			names = append(names, agentLabel(id))
		}
		d.kv("Agents", esc(strings.Join(names, ", ")))
		if s.Adopted {
			d.kv("Adopted", "made with sbx, taken over by unibox")
		}
		if a.ubx.overview.Context.Set.Sandbox == s.Name {
			d.kv("Context", "unibox works here when not told")
		}
		if read {
			d.kv("CPUs", esc(firstOf(extra.resources.Resources.CPUs, "the env's")))
			d.kv("Memory", esc(firstOf(extra.resources.Resources.Memory, "the env's")))
		}
		d.section("Tasks")
		for _, t := range s.Tasks {
			line := esc(agentLabel(t.Agent))
			if _, ok := s.RunOf(t.ID); ok {
				line += " · " + tag(role("sandboxes.running")) + "working" + tagEnd
			}
			d.kv(t.Name(), line)
		}
		if len(s.Tasks) == 0 {
			d.sub("none - New Task… starts one")
		}
		if read {
			d.section("Ports")
			for _, port := range extra.ports {
				d.kv(port.Name, esc(portLine(port)))
			}
			if len(extra.ports) == 0 {
				d.sub("none")
			}
			d.section("Network")
			for _, rule := range extra.network.Rules {
				ink := "sandboxes.allowed"
				if rule.Decision != "allow" {
					ink = "sandboxes.denied"
				}
				d.kv(rule.Host, tag(role(ink))+esc(rule.Decision)+tagEnd+muted(" · "+rule.From))
			}
			for _, b := range extra.network.Blocked {
				d.kv(b.Host, tag(role("sandboxes.blocked"))+"refused"+tagEnd+muted(fmt.Sprintf(" · %d× · %s", b.Count, humanAge(b.LastSeen))))
			}
			if len(extra.network.Rules)+len(extra.network.Blocked) == 0 {
				d.sub("the env's policy alone")
			}
		} else {
			reading()
		}
	case rowTask:
		t := r.task
		d.title(t.Name())
		d.sub("task in " + t.Sandbox)
		state, _ := a.sandboxState(r)
		d.blank()
		d.kv("State", esc(state))
		d.kv("Agent", esc(agentLabel(t.Agent)))
		if t.Alias != "" {
			d.kv("ID", esc(t.ID))
		}
		d.kv("Directory", esc(t.Directory))
		if run, ok := r.sandbox.RunOf(t.ID); ok {
			d.kv("Since", when(run.Since))
		}
		d.section("Repositories")
		for _, repo := range t.Repositories {
			branch := tag(role("sandboxes.branch")) + esc(repo.Branch) + tagEnd
			if repo.Context {
				branch += muted(" · read only")
			}
			d.kv(repo.Name, branch)
			d.kv("", esc(repo.URL))
			if repo.Mounted {
				d.kv("", "mounted from "+esc(tildePath(repo.HostPath)))
			} else {
				d.kv("", muted("cloned in the sandbox"))
			}
			d.kv("", muted("commits as "+repo.Author))
		}
	}
	return d.String()
}

// environmentDetailText is what unibox works with on this machine: the
// context, its own settings and the tokens sbx keeps.
func (a *App) environmentDetailText() string {
	o := a.ubx.overview
	d := &detailBuf{}
	d.title("Environment")
	d.sub("unibox " + o.Version.Version)
	d.blank()
	switch set := o.Context; {
	case set.Name() != "":
		d.kv("Context", tag(role("tabs.context"))+esc(contextWords(set))+tagEnd)
	case set.Sandbox != "" || set.Env != "":
		d.kv("Context", esc(strings.Trim(set.Sandbox+" · "+set.Env, " ·"))+tag(colMuted)+" · the only one"+tagEnd)
	default:
		d.kv("Context", tag(colMuted)+"none: commands are told"+tagEnd)
	}
	d.kv("Default env", esc(firstOf(o.Default, "none")))
	d.section("Settings")
	if why := o.Unread["configuration"]; why != "" {
		d.raw(tag(role("sandboxes.problem")) + esc(why) + tagEnd + "\n")
	}
	for _, s := range uniboxSwitches {
		d.kv(s.label, esc(switchWord(s.get(o.Config), s.unset)))
	}
	d.section("Tokens sbx keeps")
	if why := o.Unread["secrets"]; why != "" {
		d.raw(tag(role("sandboxes.problem")) + esc(why) + tagEnd + "\n")
	}
	for _, s := range o.Secrets {
		line := esc(s.Scope)
		if len(s.UsedBy) > 0 {
			line += tag(colMuted) + esc(" · "+strings.Join(s.UsedBy, ", ")) + tagEnd
		}
		d.kv(s.Service, line)
	}
	if len(o.Secrets) == 0 {
		d.sub("none stored")
	} else {
		d.sub("their values never leave sbx")
	}
	return d.String()
}

// contextWords are the context unibox works in when not told: a sandbox,
// or an env whose sandbox is to be made.
func contextWords(c unibox.Context) string {
	if c.Set.Sandbox != "" {
		return c.Set.Sandbox
	}
	return "env " + c.Set.Env
}

// portLine is a port as the detail says it: its mapping, and where it is
// reachable while it is published.
func portLine(p unibox.Port) string {
	text := p.Port
	switch {
	case len(p.Published) > 0:
		var at []string
		for _, pub := range p.Published {
			at = append(at, pub.URL())
		}
		text += " · " + strings.Join(at, ", ")
	case p.On:
		text += " · on at the next start"
	default:
		text += " · off"
	}
	if p.Description != "" {
		text += " · " + p.Description
	}
	return text
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// uniboxFound is what Settings › Integrations says of the unibox found, as
// the Sandboxes tab last read it: its release and its envs.
func (a *App) uniboxFound() string {
	o := a.ubx.overview
	switch {
	case !a.ubx.read:
		return ""
	case a.ubx.err != nil:
		return a.ubx.err.Error()
	}
	return fmt.Sprintf("unibox %s, %s, %s", o.Version.Version, counted(len(o.Envs), "env", "envs"), counted(len(o.Sandboxes), "sandbox", "sandboxes"))
}
