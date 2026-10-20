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

// The Sandboxes screen is one UI over unibox: its profiles, the sandboxes
// made from each, and the tasks in each sandbox, as one tree. Everything on
// it is read from unibox's --json and everything done on it is a unibox
// command, so it shows what unibox itself would say and changes nothing
// unibox would not. unagit reads none of unibox's files - not a profile's
// YAML, not its state.

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
	// details are what a row's detail shows besides the overview - a
	// profile's agents and services, a sandbox's ports - by row key, read
	// when the row is first looked at and dropped with every refresh.
	details map[string]*sandboxDetail
}

// sandboxDetail is what was read for one row's detail.
type sandboxDetail struct {
	loading  bool
	err      error
	agents   []unibox.Agent
	services []unibox.Service
	network  unibox.Network
	ports    []unibox.Port
	scripts  map[string]string
	asked    []unibox.Asked
}

type sandboxRowKind int

const (
	rowProfile sandboxRowKind = iota
	rowSandbox
	rowTask
)

// sandboxRow is one row of the tree: a profile, a sandbox under it, or a
// task under that. A row carries what is above it, so its actions need
// nothing else.
type sandboxRow struct {
	kind    sandboxRowKind
	profile unibox.Profile
	sandbox unibox.Sandbox
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
	return "p:" + r.profile.Name
}

// working says whether an agent unibox started works on the task now.
func (r sandboxRow) working() bool {
	_, ok := r.sandbox.RunOf(r.task.ID)
	return ok
}

// sandboxRows lays the overview out as the tree: each profile, its
// sandboxes by name, each sandbox's tasks by name. A sandbox whose profile
// is gone from the repository still has its profile's row, saying so.
func sandboxRows(o unibox.Overview) []sandboxRow {
	profiles := append([]unibox.Profile(nil), o.Profiles...)
	known := map[string]bool{}
	for _, p := range profiles {
		known[p.Name] = true
	}
	for _, s := range o.Sandboxes {
		if !known[s.Profile] {
			known[s.Profile] = true
			profiles = append(profiles, unibox.Profile{Name: s.Profile, Error: "not in the environments repository any more"})
		}
	}
	sandboxes := append([]unibox.Sandbox(nil), o.Sandboxes...)
	sort.SliceStable(sandboxes, func(i, j int) bool { return sandboxes[i].Name < sandboxes[j].Name })
	var rows []sandboxRow
	for _, p := range profiles {
		rows = append(rows, sandboxRow{kind: rowProfile, profile: p})
		for _, s := range sandboxes {
			if s.Profile != p.Name {
				continue
			}
			rows = append(rows, sandboxRow{kind: rowSandbox, profile: p, sandbox: s})
			tasks := append([]unibox.Task(nil), s.Tasks...)
			sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Name() < tasks[j].Name() })
			for _, t := range tasks {
				rows = append(rows, sandboxRow{kind: rowTask, profile: p, sandbox: s, task: t})
			}
		}
	}
	return rows
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
	return "profile " + r.profile.Name
}

// sandboxHay is what a row is found by.
func sandboxHay(r sandboxRow) string {
	switch r.kind {
	case rowSandbox:
		return r.sandbox.Name + " " + r.sandbox.Runtime + " " + r.sandbox.State
	case rowTask:
		hay := r.task.Name() + " " + r.task.ID + " " + r.task.Sandbox + " " + r.task.Agent
		for _, repo := range r.task.Repositories {
			hay += " " + repo.Name + " " + repo.Branch
		}
		return hay
	}
	return r.profile.Name + " " + strings.Join(r.profile.Agents, " ")
}

// agentLabel is an agent as the screen names it: unagit's name for it
// where it knows the agent, unibox's id otherwise.
func agentLabel(id string) string {
	if ag, ok := agents.ByID(id); ok {
		return ag.Name
	}
	return id
}

// profileImages is a profile's images by runtime, the default runtime
// first, and whether any of them is not built.
func (a *App) profileImages(p unibox.Profile) (string, bool) {
	var names []string
	for name := range p.Images {
		names = append(names, name)
	}
	def, _ := a.defaultRuntime()
	rank := func(name string) int {
		if name == def {
			return 0
		}
		return 1
	}
	sort.SliceStable(names, func(i, j int) bool {
		if rank(names[i]) != rank(names[j]) {
			return rank(names[i]) < rank(names[j])
		}
		return names[i] < names[j]
	})
	var parts []string
	problem := false
	for _, name := range names {
		state := p.Images[name]
		parts = append(parts, name+" "+state)
		problem = problem || state != "built"
	}
	return strings.Join(parts, " · "), problem
}

// defaultRuntime is the runtime unibox starts a sandbox in when not told,
// while the runtimes have been read.
func (a *App) defaultRuntime() (string, bool) {
	for _, rt := range a.ubx.overview.Runtimes {
		if rt.Default {
			return rt.Name, true
		}
	}
	return "", false
}

// sandboxState is a row's STATE: its words and the role they are drawn in.
func (a *App) sandboxState(r sandboxRow) (string, string) {
	switch r.kind {
	case rowSandbox:
		switch r.sandbox.State {
		case "running":
			return glyphDot + " running", "sandboxes.running"
		case "stopped":
			return glyphRing + " stopped", "sandboxes.stopped"
		case "missing", "unavailable":
			return glyphRing + " " + r.sandbox.State, "sandboxes.problem"
		}
		return glyphRing + " " + r.sandbox.State, "sandboxes.stopped"
	case rowTask:
		if r.working() {
			return spinnerGlyph(a.spinFrame) + " working", "sandboxes.running"
		}
		if r.task.State == "ready" {
			return glyphCheck + " ready", "sandboxes.stopped"
		}
		return glyphRing + " " + r.task.State, "sandboxes.problem"
	}
	if r.profile.Error != "" {
		return "does not load", "sandboxes.problem"
	}
	// The row says what the default runtime would start, which is what
	// needs doing; the detail has every runtime's image. An image another
	// runtime built once - msb's, tried and left - is not something to do,
	// and on the row it read as the profile's own.
	if rt, ok := a.defaultRuntime(); ok {
		switch state := r.profile.Images[rt]; state {
		case "":
			return "no image", "sandboxes.problem"
		case "built":
			return "built", "sandboxes.stopped"
		default:
			return state, "sandboxes.problem"
		}
	}
	var behind []string
	for _, name := range sortedKeys(r.profile.Images) {
		if state := r.profile.Images[name]; state != "built" {
			behind = append(behind, name+" "+state)
		}
	}
	switch {
	case len(r.profile.Images) == 0:
		return "no image", "sandboxes.problem"
	case len(behind) > 0:
		return strings.Join(behind, " · "), "sandboxes.problem"
	}
	return "built", "sandboxes.stopped"
}

// sandboxAgents is a row's AGENT column: a profile's agents, the agents at
// work in a sandbox, a task's own.
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
	return strings.Join(r.profile.Agents, ", ")
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
		empty := "No task yet - n on a sandbox or a profile starts one, Open in Sandbox… on a worktree too."
		if p.query != "" {
			empty = "No task matches."
		}
		p.table.SetCell(1, 0, tview.NewTableCell(" "+tag(colMuted)+esc(empty)+tagEnd).SetSelectable(false).SetExpansion(1))
		return
	}
	p.selectRow(previous, 1)
}

// sandboxName is the NAME of a row, its indent left out.
func sandboxName(r sandboxRow) string {
	switch r.kind {
	case rowSandbox:
		return r.sandbox.Name
	case rowTask:
		return r.task.Name()
	}
	return r.profile.Name
}

// sandboxesProblem is why the screen has no rows: unibox missing, a
// contract not understood, no environments repository - each with where to
// set it.
func (a *App) sandboxesProblem() string {
	switch {
	case !a.ubx.read:
		return ""
	case a.ubx.err != nil:
		return a.ubx.err.Error()
	case a.ubx.overview.Environments == "" && len(a.sandboxRows) == 0:
		return "no environments repository yet - choose one with Set Environments Repository… (:)"
	case len(a.sandboxRows) == 0:
		return "no profiles in " + tildePath(a.ubx.overview.Environments) + " - add one with unibox init <profile>"
	}
	return ""
}

// readSandboxDetail reads what a row's detail shows besides the overview -
// a profile's agents, services, network and values, a sandbox's ports and
// scripts - the first time the row is looked at, and calls then on the
// event loop once it is there.
func (a *App) readSandboxDetail(r sandboxRow, then func()) {
	if r.kind == rowTask {
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
		if r.kind == rowProfile {
			var err error
			got.agents, err = c.Agents(r.profile.Name)
			keep(err)
			got.services, err = c.Services(r.profile.Name)
			keep(err)
			got.network, err = c.Network(r.profile.Name)
			keep(err)
			got.asked, err = c.Asked(r.profile.Name)
			keep(err)
		} else {
			var err error
			got.ports, err = c.Ports(r.sandbox.Name)
			keep(err)
			got.scripts, err = c.Scripts(r.sandbox.Name)
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
	reading := func() {
		switch {
		case extra == nil || extra.loading:
			d.sub("reading…")
		case extra.err != nil:
			d.raw(tag(role("sandboxes.problem")) + esc(extra.err.Error()) + tagEnd + "\n")
		}
	}
	switch r.kind {
	case rowProfile:
		pr := r.profile
		d.title(pr.Name)
		d.sub("profile")
		if pr.Error != "" {
			d.blank()
			d.raw(tag(role("sandboxes.problem")) + esc(pr.Error) + tagEnd + "\n")
		}
		d.section("Images")
		images, _ := a.profileImages(pr)
		d.kv("By runtime", esc(images))
		d.kv("Last commit", esc(pr.LastCommit))
		if env := a.ubx.overview.Environments; env != "" {
			d.kv("Directory", esc(tildePath(env+"/sandboxes/"+pr.Name)))
		}
		d.section("Agents")
		if extra != nil && !extra.loading && extra.err == nil {
			for _, ag := range extra.agents {
				d.kv(agentLabel(ag.Agent), esc(ag.Auth))
				d.kv("", tag(colMuted)+esc("credits "+ag.CoAuthor)+tagEnd)
			}
			if len(extra.agents) == 0 {
				d.sub("none")
			}
			d.section("Services")
			for _, s := range extra.services {
				state := tag(role("sandboxes.running")) + "token stored" + tagEnd
				if s.Token != "stored" {
					state = tag(role("sandboxes.problem")) + "token " + esc(s.Token) + tagEnd
				}
				kind := ""
				if s.Git {
					kind = " · git"
				}
				d.kv(s.Name, esc(strings.Join(s.Hosts, ", ")+kind)+" · "+state)
			}
			if len(extra.services) == 0 {
				d.sub("none - Add Service… signs the sandboxes in to a forge")
			}
			d.section("Network")
			d.kv("Allowed", esc(strings.Join(extra.network.Allow, ", ")))
			d.kv("Denied", esc(strings.Join(extra.network.Deny, ", ")))
			if len(extra.network.Allow)+len(extra.network.Deny) == 0 {
				d.sub("the runtime's policy alone")
			}
			d.section("Values")
			for _, v := range extra.asked {
				switch {
				case v.Missing():
					d.kv(v.Name, tag(role("sandboxes.problem"))+"missing"+tagEnd+tag(colMuted)+esc(" · "+v.Description)+tagEnd)
				default:
					d.kv(v.Name, esc(v.Value)+tag(colMuted)+esc(" · "+v.From)+tagEnd)
				}
			}
			if len(extra.asked) == 0 {
				d.sub("it asks for none")
			}
		} else {
			for _, ag := range pr.Agents {
				d.kv(agentLabel(ag), tag(colMuted)+"…"+tagEnd)
			}
			reading()
		}
	case rowSandbox:
		s := r.sandbox
		d.title(s.Name)
		d.sub("sandbox of " + s.Profile)
		state, _ := a.sandboxState(r)
		d.blank()
		d.kv("State", esc(state))
		d.kv("Runtime", esc(s.Runtime+" · "+s.RuntimeName))
		d.kv("CPUs", esc(s.Settings.CPUs))
		d.kv("Memory", esc(firstOf(s.Memory, s.Settings.Memory)))
		d.kv("Image", esc(firstOf(s.Settings.Image, s.Settings.Dockerfile)))
		d.kv("Instructions", esc(strings.Join(s.Instructions, ", ")))
		if a.ubx.overview.Context.Set.Sandbox == s.Name {
			d.kv("Context", "unibox works here when not told")
		}
		d.section("Tasks")
		for _, t := range s.Tasks {
			line := esc(t.Agent)
			if _, ok := s.RunOf(t.ID); ok {
				line += " · " + tag(role("sandboxes.running")) + "working" + tagEnd
			}
			d.kv(t.Name(), line)
		}
		if len(s.Tasks) == 0 {
			d.sub("none - New Task… starts one")
		}
		d.section("Network")
		d.kv("Allowed", esc(strings.Join(s.NetworkAllow, ", ")))
		d.kv("Denied", esc(strings.Join(s.NetworkDeny, ", ")))
		if extra != nil && !extra.loading && extra.err == nil {
			d.section("Ports")
			for _, port := range extra.ports {
				d.kv(port.Name, esc(portLine(port)))
			}
			if len(extra.ports) == 0 {
				d.sub("none")
			}
			d.section("Scripts")
			for _, name := range sortedKeys(extra.scripts) {
				d.kv(name, esc(extra.scripts[name]))
			}
			if len(extra.scripts) == 0 {
				d.sub("none")
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
			d.kv(repo.Name, tag(role("sandboxes.branch"))+esc(repo.Branch)+tagEnd)
			d.kv("", esc(repo.URL))
			if repo.Mounted {
				d.kv("", "mounted from "+esc(tildePath(repo.HostPath)))
			}
			d.kv("", tag(colMuted)+esc("commits as "+repo.Author)+tagEnd)
		}
	}
	return d.String()
}

// environmentDetailText is what unibox works with on this machine: the
// environments repository, the context, the runtimes and what is chosen
// for each, the secrets by name and the values.
func (a *App) environmentDetailText() string {
	o := a.ubx.overview
	d := &detailBuf{}
	d.title("Environment")
	d.sub("unibox " + o.Version.Version + " · profile format " + fmt.Sprint(o.Version.ProfileFormat))
	d.blank()
	d.kv("Repository", esc(firstOf(tildePath(o.Environments), "none - Set Environments Repository… (e)")))
	set := o.Context.Set
	switch {
	case set.Sandbox != "" || set.Profile != "":
		d.kv("Context", esc(strings.Trim(set.Sandbox+" · "+firstOf(set.Profile, o.Context.Profile), " ·")))
	case o.Context.Sandbox != "" || o.Context.Profile != "":
		d.kv("Context", esc(strings.Trim(o.Context.Sandbox+" · "+o.Context.Profile, " ·"))+tag(colMuted)+" · the only one"+tagEnd)
	default:
		d.kv("Context", tag(colMuted)+"none: commands are told"+tagEnd)
	}
	d.section("Runtimes")
	for _, rt := range o.Runtimes {
		var words []string
		switch {
		case !rt.Available:
			words = append(words, "not installed")
		case !rt.Enabled:
			words = append(words, "turned off")
		default:
			words = append(words, "on")
		}
		if rt.Default {
			words = append(words, "the default")
		}
		if rt.Experimental {
			words = append(words, "experimental")
		}
		if set := o.Config.Runtimes[rt.Name]; set.StopIdle != nil {
			if *set.StopIdle {
				words = append(words, "stops when idle")
			} else {
				words = append(words, "keeps running when idle")
			}
		}
		line := esc(strings.Join(words, " · "))
		if rt.Error != "" {
			line += " · " + tag(role("sandboxes.problem")) + esc(rt.Error) + tagEnd
		}
		d.kv(rt.Name, line)
	}
	d.section("Secrets")
	if why := o.Unread["secrets"]; why != "" {
		d.raw(tag(role("sandboxes.problem")) + esc(why) + tagEnd + "\n")
	}
	for _, name := range o.Secrets.Names {
		d.kv(name, tag(colMuted)+"secret:"+esc(name)+tagEnd)
	}
	if len(o.Secrets.Names) == 0 {
		d.sub("none stored")
	} else if o.Secrets.Store != "" {
		d.sub("in the " + o.Secrets.Store + "; their values never leave it")
	}
	d.section("Your values")
	if why := o.Unread["values"]; why != "" {
		d.raw(tag(role("sandboxes.problem")) + esc(why) + tagEnd + "\n")
	}
	for _, name := range sortedKeys(o.Values.Shared) {
		d.kv(name, esc(o.Values.Shared[name])+tag(colMuted)+" · every profile"+tagEnd)
	}
	var profiles []string
	for p := range o.Values.Profiles {
		profiles = append(profiles, p)
	}
	sort.Strings(profiles)
	for _, p := range profiles {
		for _, name := range sortedKeys(o.Values.Profiles[p]) {
			d.kv(name, esc(o.Values.Profiles[p][name])+tag(colMuted)+esc(" · profile "+p)+tagEnd)
		}
	}
	if len(o.Values.Shared)+len(profiles) == 0 {
		d.sub("none set")
	}
	if o.Values.File != "" {
		d.sub("kept in " + tildePath(o.Values.File))
	}
	return d.String()
}

// portLine is a port as the detail says it: its mapping, and where it is
// reachable while it is published.
func portLine(p unibox.Port) string {
	text := p.Port
	switch {
	case len(p.Published) > 0:
		var at []string
		for _, pub := range p.Published {
			at = append(at, fmt.Sprintf("http://%s:%d", firstOf(pub.HostIP, "127.0.0.1"), pub.HostPort))
		}
		text += " · " + strings.Join(at, ", ")
	case p.On:
		text += " · on at the next start"
	default:
		text += " · off"
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
// the Sandboxes tab last read it: its release, the profile format it reads,
// where the profiles are.
func (a *App) uniboxFound() string {
	o := a.ubx.overview
	switch {
	case !a.ubx.read:
		return ""
	case a.ubx.err != nil:
		return a.ubx.err.Error()
	}
	text := fmt.Sprintf("unibox %s, profile format %d", o.Version.Version, o.Version.ProfileFormat)
	if o.Environments != "" {
		text += ", profiles in " + tildePath(o.Environments)
	}
	return text
}
