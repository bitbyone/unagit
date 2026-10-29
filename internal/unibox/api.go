package unibox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Version is the unibox found: its release.
type Version struct {
	Version string `json:"version"`
}

func (c *Client) Version() (Version, error) {
	var v Version
	err := c.read(&v, "version")
	return v, err
}

// Overview is everything the Sandboxes screen lists, read at once.
type Overview struct {
	Version   Version
	Envs      []Env
	Default   string
	Context   Context
	Sandboxes []Sandbox
	// Config and Secrets are read beside the rest, but a failure of one
	// leaves the screen standing: Unread says, by what it is, why it is
	// missing.
	Config  Config
	Secrets []KeptSecret
	Unread  map[string]string
}

// Env is the env a sandbox is named after, or nil when no env of unibox's
// names it any more.
func (o Overview) Env(name string) *Env {
	for i := range o.Envs {
		if o.Envs[i].Name == name {
			return &o.Envs[i]
		}
	}
	return nil
}

// Sandbox is a sandbox by its name, if unibox has one.
func (o Overview) Sandbox(name string) (Sandbox, bool) {
	for _, s := range o.Sandboxes {
		if s.Name == name {
			return s, true
		}
	}
	return Sandbox{}, false
}

// Overview reads the envs, the sandboxes with their tasks and the context,
// side by side; then each env as unibox shows it, for its sandbox's name,
// its agent and what it lacks.
func (c *Client) Overview() (Overview, error) {
	var o Overview
	var wg sync.WaitGroup
	errs := make([]error, 4)
	run := func(i int, read func() error) {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = read() }()
	}
	run(0, func() (err error) { o.Version, err = c.Version(); return })
	run(1, func() (err error) { o.Envs, o.Default, err = c.Envs(); return })
	run(2, func() (err error) { o.Sandboxes, err = c.Sandboxes(); return })
	run(3, func() (err error) { o.Context, err = c.Context(); return })
	var extras [2]error
	for i, read := range []func() error{
		func() (err error) { o.Config, err = c.Config(); return },
		func() (err error) { o.Secrets, err = c.KeptSecrets(); return },
	} {
		wg.Add(1)
		go func() { defer wg.Done(); extras[i] = read() }()
	}
	wg.Wait()
	for i, what := range []string{"configuration", "secrets"} {
		if extras[i] != nil {
			o.unread(what, extras[i])
		}
	}
	// The version first: a contract not understood explains every other
	// failure.
	for _, err := range errs {
		var version *VersionError
		if errors.As(err, &version) {
			return o, err
		}
	}
	for _, err := range errs {
		if err != nil {
			return o, err
		}
	}
	// What each env's files declare is read beside the others; an env
	// whose files do not load keeps its row, saying why.
	for i := range o.Envs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := &o.Envs[i]
			shown, err := c.ShowEnv(e.Name)
			if err != nil {
				e.Error = err.Error()
				return
			}
			e.Sandbox, e.Agent, e.Args, e.Kits, e.Missing = shown.Sandbox, shown.SBX.Agent, shown.SBX.Args, shown.SBX.Kits, shown.Missing
		}()
	}
	wg.Wait()
	return o, nil
}

func (o *Overview) unread(what string, err error) {
	if o.Unread == nil {
		o.Unread = map[string]string{}
	}
	o.Unread[what] = err.Error()
}

// Env is one env: the team's sbxenv files a sandbox is created from, the
// sandbox's name, the agent its kit runs and what it asks of the person.
// Files, Repo and Source come from the list; the rest from env show.
type Env struct {
	Name   string   `json:"-"`
	Files  []string `json:"files"`
	Repo   string   `json:"repo,omitempty"`
	Source string   `json:"source,omitempty"`
	// Sandbox is the name of the sandbox the env makes.
	Sandbox string            `json:"-"`
	Agent   string            `json:"-"`
	Args    map[string]EnvArg `json:"-"`
	Kits    []EnvKit          `json:"-"`
	// Missing are the args the env asks for that have no value; a run
	// stops for want of them.
	Missing []string `json:"-"`
	// Error is why env show could not read the env's files.
	Error string `json:"-"`
}

// EnvArg is an arg an env declares.
type EnvArg struct {
	Required    bool    `json:"required,omitempty"`
	Default     *string `json:"default,omitempty"`
	Description string  `json:"description,omitempty"`
}

// EnvKit is a kit an env's sandboxes are made with.
type EnvKit struct {
	Source string `json:"source"`
}

// Envs are unibox's envs by name, and the one new sandboxes come from.
func (c *Client) Envs() ([]Env, string, error) {
	var out struct {
		Default string         `json:"default"`
		Envs    map[string]Env `json:"envs"`
	}
	if err := c.read(&out, "env", "ls"); err != nil {
		return nil, "", err
	}
	envs := make([]Env, 0, len(out.Envs))
	for name, e := range out.Envs {
		e.Name = name
		envs = append(envs, e)
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].Name < envs[j].Name })
	return envs, out.Default, nil
}

// ShownEnv is what env show says of an env.
type ShownEnv struct {
	Env     string `json:"env"`
	Sandbox string `json:"sandbox"`
	SBX     struct {
		Agent string            `json:"agent"`
		Args  map[string]EnvArg `json:"args"`
		Kits  []EnvKit          `json:"kits"`
	} `json:"sbxenv"`
	Missing []string `json:"missing"`
}

func (c *Client) ShowEnv(name string) (ShownEnv, error) {
	var out ShownEnv
	err := c.read(&out, "env", "show", name)
	return out, err
}

// Repository is one repository of a task.
type Repository struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	HostPath string `json:"host_path,omitempty"`
	Author   string `json:"author"`
	// Mounted is the host's checkout mounted into the sandbox, rather
	// than a clone of the forge's.
	Mounted bool `json:"mounted,omitempty"`
	// Context is a repository the task reads and leaves as it is.
	Context bool `json:"context,omitempty"`
}

// Task is a piece of work in a sandbox: repositories mounted or cloned,
// and the agent that works on them. Its name is unique across sandboxes.
type Task struct {
	ID           string       `json:"id"`
	Alias        string       `json:"alias,omitempty"`
	Sandbox      string       `json:"sandbox"`
	Directory    string       `json:"directory"`
	Repositories []Repository `json:"repositories"`
	Agent        string       `json:"agent,omitempty"`
	State        string       `json:"state"`
}

// Name is how the task is called: its alias, or its ID.
func (t Task) Name() string {
	if t.Alias != "" {
		return t.Alias
	}
	return t.ID
}

// HostPaths are the directories of the host's mounted into the task.
func (t Task) HostPaths() []string {
	var out []string
	for _, r := range t.Repositories {
		if r.Mounted && r.HostPath != "" {
			out = append(out, r.HostPath)
		}
	}
	return out
}

// Run is an agent or a shell unibox started and is still waiting for.
type Run struct {
	ID      string    `json:"id"`
	Sandbox string    `json:"sandbox"`
	Task    string    `json:"task,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	PID     int       `json:"pid"`
	Pane    string    `json:"pane,omitempty"`
	Since   time.Time `json:"since"`
}

// Sandbox is one sandbox sbx created from an env.
type Sandbox struct {
	Name  string   `json:"name"`
	Env   string   `json:"env"`
	Files []string `json:"files"`
	// Agent is the agent the env's kit runs; Agents every agent the
	// sandbox has, by name, with how it is started.
	Agent   string              `json:"agent,omitempty"`
	Agents  map[string][]string `json:"agents,omitempty"`
	Adopted bool                `json:"adopted,omitempty"`
	State   string              `json:"state"`
	Tasks   []Task              `json:"tasks"`
	Runs    []Run               `json:"runs"`
	Ports   map[string]string   `json:"ports,omitempty"`
}

// AgentIDs are the agents a task in the sandbox may run, the kit's own
// first.
func (s Sandbox) AgentIDs() []string {
	var out []string
	if s.Agent != "" {
		out = append(out, s.Agent)
	}
	var rest []string
	for id := range s.Agents {
		if id != s.Agent {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// Has says whether the sandbox can run an agent.
func (s Sandbox) Has(agent string) bool {
	if agent == s.Agent {
		return true
	}
	_, ok := s.Agents[agent]
	return ok
}

// RunOf is the run working on a task, if one is.
func (s Sandbox) RunOf(task string) (Run, bool) {
	for _, r := range s.Runs {
		if r.Task == task {
			return r, true
		}
	}
	return Run{}, false
}

func (c *Client) Sandboxes() ([]Sandbox, error) {
	var out struct {
		Sandboxes []Sandbox `json:"sandboxes"`
	}
	err := c.read(&out, "sandbox", "ls")
	return out.Sandboxes, err
}

// Context is the sandbox and env unibox works in when not told, and what
// it falls back on.
type Context struct {
	Sandbox string `json:"sandbox"`
	Env     string `json:"env"`
	// Set is what the user chose with unibox context use, as against what
	// there being only one gives.
	Set struct {
		Sandbox string `json:"sandbox,omitempty"`
		Env     string `json:"env,omitempty"`
	} `json:"context"`
}

// Name is how the context is called: the sandbox chosen, else the env
// whose sandbox is to be made; empty while none is chosen.
func (c Context) Name() string {
	if c.Set.Sandbox != "" {
		return c.Set.Sandbox
	}
	return c.Set.Env
}

func (c *Client) Context() (Context, error) {
	var out Context
	err := c.read(&out, "context")
	return out, err
}

// Config is unibox's own configuration as config get gives it; a nil
// switch is unibox's default.
type Config struct {
	DefaultEnv           string `json:"default_env,omitempty"`
	StopIdle             *bool  `json:"stop_idle,omitempty"`
	SSHAgent             *bool  `json:"ssh_agent,omitempty"`
	EditorSSH            *bool  `json:"editor_ssh,omitempty"`
	RememberHostCommands *bool  `json:"remember_host_commands,omitempty"`
	ClaudeRemoteControl  *bool  `json:"claude_remote_control,omitempty"`
	ClipboardImagePaste  *bool  `json:"clipboard_image_paste,omitempty"`
}

func (c *Client) Config() (Config, error) {
	var out struct {
		Value Config `json:"value"`
	}
	err := c.read(&out, "config", "get")
	return out.Value, err
}

// Secret is a token an env's sandboxes use, and whether sbx has it: unibox
// never gives a value, and unagit never asks for one.
type Secret struct {
	Service     string   `json:"service"`
	Description string   `json:"description,omitempty"`
	Optional    bool     `json:"optional,omitempty"`
	Kits        []string `json:"kits,omitempty"`
	Stored      bool     `json:"stored"`
	Domains     []string `json:"domains,omitempty"`
	// Scope is where it is kept: global, or a sandbox's or an env's own.
	Scope string `json:"scope,omitempty"`
	Own   string `json:"own,omitempty"`
	Page  string `json:"page,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// Secrets are the tokens an env's sandboxes use.
func (c *Client) Secrets(env string) ([]Secret, error) {
	var out struct {
		Secrets []Secret `json:"secrets"`
	}
	err := c.read(&out, "secret", "ls", "-e", env)
	return out.Secrets, err
}

// KeptSecret is a token sbx keeps, and the sandboxes that take it.
type KeptSecret struct {
	Service string   `json:"service"`
	Scope   string   `json:"scope"`
	Kept    string   `json:"kept"`
	UsedBy  []string `json:"used_by"`
	Needed  bool     `json:"needed,omitempty"`
}

// KeptSecrets are every token sbx keeps, global or a sandbox's own.
func (c *Client) KeptSecrets() ([]KeptSecret, error) {
	var out struct {
		Secrets []KeptSecret `json:"secrets"`
	}
	err := c.read(&out, "secret", "ls", "--all")
	return out.Secrets, err
}

// Arg is an arg an env asks for and the value it gets, and where that
// comes from: yours, env-repo, global, default, optional, missing - or
// undeclared, a value of yours the env no longer asks for.
type Arg struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	From        string `json:"from"`
	Description string `json:"description,omitempty"`
}

// Missing says whether a run stops for want of it.
func (a Arg) Missing() bool { return a.From == "missing" }

// Args are what an env asks of the person, and what they gave.
func (c *Client) Args(env string) ([]Arg, error) {
	var out struct {
		Args []Arg `json:"args"`
	}
	err := c.read(&out, "var", "ls", "-e", env)
	return out.Args, err
}

// IndexedRepo is a repository an env's tasks are given by name.
type IndexedRepo struct {
	Name        string `json:"-"`
	URL         string `json:"url"`
	Ref         string `json:"ref,omitempty"`
	Default     string `json:"default,omitempty"`
	Context     *bool  `json:"context,omitempty"`
	Description string `json:"description,omitempty"`
	From        string `json:"from,omitempty"`
}

// Repos are an env's repositories by name.
func (c *Client) Repos(env string) ([]IndexedRepo, error) {
	var out struct {
		Repos map[string]IndexedRepo `json:"repos"`
	}
	if err := c.read(&out, "repo", "ls", "-e", env); err != nil {
		return nil, err
	}
	repos := make([]IndexedRepo, 0, len(out.Repos))
	for name, r := range out.Repos {
		r.Name = name
		repos = append(repos, r)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	return repos, nil
}

// Published is where a sandbox port is reachable on the host.
type Published struct {
	HostIP      string `json:"host_ip"`
	HostPort    int    `json:"host_port"`
	SandboxPort int    `json:"sandbox_port"`
	Protocol    string `json:"protocol"`
}

// URL is where a published port is opened.
func (p Published) URL() string {
	ip := p.HostIP
	if ip == "" || ip == "0.0.0.0" {
		ip = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", ip, p.HostPort)
}

// Port is one of a sandbox's ports: one the team or the person names, or
// one switched on for it alone.
type Port struct {
	Name        string      `json:"name"`
	Port        string      `json:"port"`
	Description string      `json:"description,omitempty"`
	Team        bool        `json:"team,omitempty"`
	From        string      `json:"from,omitempty"`
	Default     bool        `json:"default"`
	Named       bool        `json:"named"`
	On          bool        `json:"on"`
	Published   []Published `json:"published"`
}

func (c *Client) Ports(sandbox string) ([]Port, error) {
	var out struct {
		Ports []Port `json:"ports"`
	}
	err := c.read(&out, "port", "ls", "-s", sandbox)
	return out.Ports, err
}

// NetworkRule is a host a sandbox may or may not reach. From is yours
// (kept by unibox), sbx (added outside unibox), env or default; Sandbox
// is empty for a rule every sandbox has.
type NetworkRule struct {
	Decision string `json:"decision"`
	Host     string `json:"host"`
	Protocol string `json:"protocol"`
	From     string `json:"from"`
	Sandbox  string `json:"sandbox,omitempty"`
	Applied  bool   `json:"applied"`
}

// BlockedHost is a host the policy refused a sandbox lately.
type BlockedHost struct {
	Host     string    `json:"host"`
	Reason   string    `json:"reason"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

// Network is a sandbox's rules beyond its env's, and the hosts it was
// refused.
type Network struct {
	Rules   []NetworkRule `json:"rules"`
	Blocked []BlockedHost `json:"blocked"`
}

func (c *Client) Network(sandbox string) (Network, error) {
	var out Network
	err := c.read(&out, "network", "ls", "-s", sandbox)
	return out, err
}

// Resources are a sandbox's CPUs and memory: what it gets, and what was
// set for it alone.
type Resources struct {
	Own       ResourceValues `json:"own"`
	Resources ResourceValues `json:"resources"`
}

// ResourceValues are a count or share of CPUs and a size or share of
// memory, empty where nothing is said.
type ResourceValues struct {
	CPUs   string `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
}

func (c *Client) Resources(sandbox string) (Resources, error) {
	var out Resources
	err := c.read(&out, "sandbox", "resources", sandbox)
	return out, err
}

// Instructions are the files a task's agent reads its instructions from
// when a session starts, in the order it reads them. Read is false while
// the sandbox is not running, when Files holds only what unibox writes.
type Instructions struct {
	Agent string            `json:"agent"`
	Read  bool              `json:"read"`
	Known bool              `json:"known"`
	Files []InstructionFile `json:"files"`
}

// InstructionFile is one of them; Unibox says unibox writes it before
// each session.
type InstructionFile struct {
	Path    string `json:"path"`
	Link    string `json:"link,omitempty"`
	Unibox  bool   `json:"unibox,omitempty"`
	Content string `json:"content"`
}

func (c *Client) Instructions(task string) (Instructions, error) {
	var out Instructions
	err := c.read(&out, "task", "instructions", task)
	return out, err
}

// DoctorCheck is one thing doctor checked: pass, warn or fail, and the
// unibox command that fixes it.
type DoctorCheck struct {
	Area    string `json:"area"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// Doctor runs unibox's checks of sbx, the envs and the sandboxes. It asks
// sbx a good deal, so it is given longer than a list.
func (c *Client) Doctor() ([]DoctorCheck, error) {
	var out struct {
		Checks []DoctorCheck `json:"checks"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*readTimeout)
	defer cancel()
	err := c.JSON(ctx, &out, "doctor")
	// A failed check is an answer, not a failure: doctor exits non-zero
	// with its checks all the same.
	var failure *Error
	if errors.As(err, &failure) && len(out.Checks) > 0 {
		err = nil
	}
	return out.Checks, err
}

// Herdr is whether herdr and unibox's plugin for it are installed.
type Herdr struct {
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (c *Client) Herdr() (Herdr, error) {
	var out struct {
		Plugin Herdr `json:"plugin"`
	}
	err := c.read(&out, "herdr", "status")
	return out.Plugin, err
}

// Need is something that stands in the way of a start, by a code unagit can
// ask about, or only a warning.
type Need struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Repository string `json:"repository,omitempty"`
	Warning    bool   `json:"warning,omitempty"`
}

// Check is what unibox task check answers.
type Check struct {
	Ready        bool         `json:"ready"`
	Needs        []Need       `json:"needs"`
	Repositories []Repository `json:"repositories"`
}

// Start is a new task to run: the sandbox it goes into (made from Env when
// unibox has none yet), the agent, and the repositories it works on -
// directories mounted, or cloned from the forge when Clone says so, which a
// git worktree and an address always are - on Branch when it is given.
type Start struct {
	Sandbox, Env, Agent string
	Dirs                []string
	Branch              string
	Clone, Push         bool
}

// Args are the arguments task check and run share; run adds the agent.
func (s Start) Args() []string {
	var args []string
	if s.Branch != "" {
		args = append(args, "--branch", s.Branch)
	}
	for _, d := range s.Dirs {
		args = append(args, "--repo", d)
	}
	if s.Sandbox != "" {
		args = append(args, "-s", s.Sandbox)
	}
	if s.Env != "" {
		args = append(args, "-e", s.Env)
	}
	if s.Clone {
		args = append(args, "--clone")
	}
	if s.Push {
		args = append(args, "--push")
	}
	return args
}

// RunArgs are the arguments of unibox run.
func (s Start) RunArgs() []string {
	args := append([]string{"run"}, s.Args()...)
	if s.Agent != "" {
		args = append(args, "--agent", s.Agent)
	}
	return args
}

// Check asks what stands in the way of a start.
func (c *Client) Check(s Start) (Check, error) {
	var out Check
	ctx, cancel := context.WithTimeout(context.Background(), 2*readTimeout)
	defer cancel()
	err := c.JSON(ctx, &out, append([]string{"task", "check"}, s.Args()...)...)
	return out, err
}
