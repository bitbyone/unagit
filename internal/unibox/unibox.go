// Package unibox drives the unibox command line: the sandboxes coding agents
// run in, the profiles they are made from and the tasks in them. unibox is a
// project of its own, and unagit imports nothing of it - it runs the binary
// and reads its versioned --json, decoded here into types of unagit's own,
// written against unibox's internal/unibox/api.
//
// Nothing here reads unibox's YAML or its state files: every answer comes
// from a command, and every change is a command, so what unagit shows is
// what unibox itself would say.
package unibox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// APIVersion is the version of unibox's JSON contract this unagit is written
// against. unibox raises it only when a field changes meaning or goes, so
// another one is refused rather than half understood.
const APIVersion = 1

// readTimeout keeps a unibox that waits on its runtime - a daemon not
// answering - from holding a list for ever.
var readTimeout = 30 * time.Second

// Client runs one unibox binary.
type Client struct {
	Binary string
}

// New is a client of the binary at a path.
func New(binary string) *Client { return &Client{Binary: binary} }

// Error is a failure unibox reported under --json: a code a program can
// act on, its message for people, and the exit status, which says what kind
// of failure it was (3 an image to build, 4 a refusal).
type Error struct {
	Code    string
	Message string
	Status  int
}

func (e *Error) Error() string { return e.Message }

// Refused reports whether unibox refused rather than failed: work it would
// not discard, a sandbox busy with a session, a question it needs answered.
func (e *Error) Refused() bool { return e.Status == 4 }

// VersionError is a unibox speaking a JSON contract this unagit does not
// know.
type VersionError struct{ Got int }

func (e *VersionError) Error() string {
	if e.Got > APIVersion {
		return fmt.Sprintf("unibox speaks API version %d and this unagit knows %d - update unagit", e.Got, APIVersion)
	}
	return fmt.Sprintf("unibox speaks API version %d and this unagit knows %d - update unibox", e.Got, APIVersion)
}

// envelope is what every --json answer carries besides its own fields.
type envelope struct {
	APIVersion int    `json:"api_version"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	OK         bool   `json:"ok"`
}

// JSON runs a command with --json and decodes its answer into out, which may
// be nil for a command whose answer is only that it worked.
func (c *Client) JSON(ctx context.Context, out any, args ...string) error {
	_, err := c.jsonAnswer(ctx, out, args...)
	return err
}

// Do runs a command that changes something and returns what unibox said it
// did.
func (c *Client) Do(ctx context.Context, args ...string) (string, error) {
	env, err := c.jsonAnswer(ctx, nil, args...)
	return env.Message, err
}

func (c *Client) jsonAnswer(ctx context.Context, out any, args ...string) (envelope, error) {
	cmd := exec.CommandContext(ctx, c.Binary, append(append([]string{}, args...), "--json")...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		if runErr != nil {
			return env, c.failed(args, runErr, stderr.String())
		}
		return env, fmt.Errorf("unibox %s answered what unagit cannot read - update unagit or unibox", strings.Join(args, " "))
	}
	if env.APIVersion != APIVersion {
		return env, &VersionError{Got: env.APIVersion}
	}
	if runErr != nil {
		status := 1
		var exit *exec.ExitError
		if errors.As(runErr, &exit) {
			status = exit.ExitCode()
		}
		message := env.Message
		if message == "" {
			message = firstLine(stderr.String(), runErr.Error())
		}
		return env, &Error{Code: env.Code, Message: message, Status: status}
	}
	if out != nil {
		if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
			return env, fmt.Errorf("unibox %s answered what unagit cannot read - update unagit or unibox: %v", strings.Join(args, " "), err)
		}
	}
	return env, nil
}

func (c *Client) failed(args []string, err error, stderr string) error {
	var notFound *exec.Error
	if errors.As(err, &notFound) || errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("unibox is not installed at %s - install it, see Settings › Integrations", c.Binary)
	}
	return fmt.Errorf("unibox %s: %s", strings.Join(args, " "), firstLine(stderr, err.Error()))
}

// firstLine is the first line of what a command said, without unibox's own
// "unibox:" before it, or the fallback when it said nothing.
func firstLine(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "unibox:")); line != "" {
			return line
		}
	}
	return fallback
}

// Stream runs a command whose progress is worth reading - a build, a
// recreation - and hands its output to say a line at a time.
func (c *Client) Stream(ctx context.Context, say func(string), args ...string) error {
	cmd := exec.CommandContext(ctx, c.Binary, append(append([]string{}, args...), "--yes")...)
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	var last string
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		scan := bufio.NewScanner(reader)
		scan.Buffer(make([]byte, 64*1024), 1024*1024)
		for scan.Scan() {
			line := strings.TrimRight(scan.Text(), "\r")
			if strings.TrimSpace(line) != "" {
				mu.Lock()
				last = line
				mu.Unlock()
			}
			say(line)
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
	}
	_ = writer.Close()
	<-done
	if err != nil {
		mu.Lock()
		defer mu.Unlock()
		return c.failed(args, err, last)
	}
	return nil
}

// Command is a command run on a terminal: an agent, a shell, a sign-in that
// asks for a token where unagit never sees it.
func (c *Client) Command(args ...string) *exec.Cmd {
	return exec.Command(c.Binary, args...)
}

// read is JSON with the timeout a list is given.
func (c *Client) read(out any, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	return c.JSON(ctx, out, args...)
}

// Version is the unibox found: its release, the profile format it reads and
// the API version it speaks.
type Version struct {
	Version       string `json:"version"`
	ProfileFormat int    `json:"profile_format"`
}

func (c *Client) Version() (Version, error) {
	var v Version
	err := c.read(&v, "version")
	return v, err
}

// Overview is everything the Sandboxes screen lists, read at once.
type Overview struct {
	Version      Version
	Environments string
	Context      Context
	Runtimes     []Runtime
	Profiles     []Profile
	Sandboxes    []Sandbox
}

// Overview reads the profiles, the sandboxes with their tasks, the runtimes
// and where the environments repository is, side by side.
func (c *Client) Overview() (Overview, error) {
	var o Overview
	var wg sync.WaitGroup
	errs := make([]error, 6)
	run := func(i int, read func() error) {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = read() }()
	}
	run(0, func() (err error) { o.Version, err = c.Version(); return })
	run(1, func() (err error) { o.Profiles, err = c.Profiles(); return })
	run(2, func() (err error) { o.Sandboxes, err = c.Sandboxes(); return })
	run(3, func() (err error) { o.Runtimes, err = c.Runtimes(); return })
	run(4, func() (err error) { o.Environments, err = c.Environments(); return })
	run(5, func() (err error) { o.Context, err = c.Context(); return })
	wg.Wait()
	// The version first: a contract not understood explains every other
	// failure.
	for _, err := range errs {
		var version *VersionError
		if errors.As(err, &version) {
			return o, err
		}
	}
	// A repository not yet chosen leaves the profiles unreadable and says
	// why on its own; the rest still stands.
	if errs[4] != nil {
		return o, errs[4]
	}
	for _, err := range errs {
		if err != nil {
			return o, err
		}
	}
	return o, nil
}

// Profile is one profile of the environments repository.
type Profile struct {
	Name     string   `json:"name"`
	Agents   []string `json:"agents"`
	Revision string   `json:"revision,omitempty"`
	// Images says, by runtime, whether the profile's image is built,
	// outdated or missing.
	Images     map[string]string `json:"images"`
	Error      string            `json:"error,omitempty"`
	LastCommit string            `json:"last_commit,omitempty"`
}

func (c *Client) Profiles() ([]Profile, error) {
	var out struct {
		Profiles []Profile `json:"profiles"`
	}
	err := c.read(&out, "profile", "ls")
	return out.Profiles, err
}

// Repository is one clone of a task.
type Repository struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	HostPath string `json:"host_path,omitempty"`
	Author   string `json:"author"`
	Mounted  bool   `json:"mounted,omitempty"`
}

// Task is a piece of work in a sandbox: clones on a branch and the agent
// that works on them. Its name is unique across sandboxes.
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

// Settings are what a sandbox was made with, as much of them as unagit
// shows.
type Settings struct {
	Dockerfile string `json:"dockerfile,omitempty"`
	Image      string `json:"image,omitempty"`
	CPUs       string `json:"cpus,omitempty"`
	Memory     string `json:"memory,omitempty"`
}

// Sandbox is one VM made from a profile.
type Sandbox struct {
	Name         string            `json:"name"`
	Runtime      string            `json:"runtime"`
	RuntimeName  string            `json:"runtime_name"`
	Profile      string            `json:"profile"`
	Instructions []string          `json:"instructions"`
	State        string            `json:"state"`
	Memory       string            `json:"memory,omitempty"`
	Tasks        []Task            `json:"tasks"`
	Runs         []Run             `json:"runs"`
	Settings     Settings          `json:"settings"`
	NetworkAllow []string          `json:"network_allow,omitempty"`
	NetworkDeny  []string          `json:"network_deny,omitempty"`
	Ports        map[string]string `json:"ports,omitempty"`
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

// Runtime is one of the runtimes unibox can make sandboxes in.
type Runtime struct {
	Name         string `json:"name"`
	Available    bool   `json:"available"`
	Enabled      bool   `json:"enabled"`
	Default      bool   `json:"default"`
	Experimental bool   `json:"experimental"`
	Error        string `json:"error"`
}

func (c *Client) Runtimes() ([]Runtime, error) {
	var out struct {
		Runtimes []Runtime `json:"runtimes"`
	}
	err := c.read(&out, "runtime", "ls")
	return out.Runtimes, err
}

// Environments is where the environments repository is, empty while none
// is chosen.
func (c *Client) Environments() (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	err := c.read(&out, "config", "get", "environments")
	return out.Value, err
}

// Context is the sandbox and profile unibox works in when not told, and
// what it falls back on.
type Context struct {
	Sandbox string `json:"sandbox"`
	Profile string `json:"profile"`
	// Set is what the user chose with unibox context use, as against what
	// there being only one gives.
	Set struct {
		Sandbox string `json:"sandbox,omitempty"`
		Profile string `json:"profile,omitempty"`
	} `json:"context"`
}

func (c *Client) Context() (Context, error) {
	var out Context
	err := c.read(&out, "context")
	return out, err
}

// Agent is one agent of a profile: how it signs in and how its commits
// credit it.
type Agent struct {
	Agent    string   `json:"agent"`
	Auth     string   `json:"auth"`
	CoAuthor string   `json:"co_author"`
	Args     []string `json:"args"`
}

func (c *Client) Agents(profile string) ([]Agent, error) {
	var out struct {
		Agents []Agent `json:"agents"`
	}
	err := c.read(&out, "agent", "ls", "-p", profile)
	return out.Agents, err
}

// Service is a forge or another host service a profile's sandboxes are
// signed in to. Token says whether its token is stored, never what it is.
type Service struct {
	Name   string   `json:"name"`
	Git    bool     `json:"git"`
	Hosts  []string `json:"hosts"`
	Token  string   `json:"token"`
	Author string   `json:"author,omitempty"`
}

func (c *Client) Services(profile string) ([]Service, error) {
	var out struct {
		Services []Service `json:"services"`
	}
	err := c.read(&out, "service", "ls", "-p", profile)
	return out.Services, err
}

// Network is a profile's rules: hosts allowed and denied beyond the
// runtime's policy, and the hosts its services reach.
type Network struct {
	Allow    []string          `json:"allow"`
	Deny     []string          `json:"deny"`
	Services map[string]string `json:"services"`
}

func (c *Client) Network(profile string) (Network, error) {
	var out Network
	err := c.read(&out, "network", "ls", "-p", profile)
	return out, err
}

// Published is where a sandbox port is reachable on the host.
type Published struct {
	HostIP      string `json:"host_ip"`
	HostPort    int    `json:"host_port"`
	SandboxPort int    `json:"sandbox_port"`
	Protocol    string `json:"protocol"`
}

// Port is one of a sandbox's ports: one its profile names, or one switched
// on for it alone.
type Port struct {
	Name      string      `json:"name"`
	Port      string      `json:"port"`
	Default   bool        `json:"default"`
	Named     bool        `json:"named"`
	On        bool        `json:"on"`
	Published []Published `json:"published"`
}

func (c *Client) Ports(sandbox string) ([]Port, error) {
	var out struct {
		Ports []Port `json:"ports"`
	}
	err := c.read(&out, "network", "port", "ls", "-s", sandbox)
	return out.Ports, err
}

// Scripts are the scripts a sandbox's profile names, by name.
func (c *Client) Scripts(sandbox string) (map[string]string, error) {
	var out struct {
		Scripts map[string]string `json:"scripts"`
	}
	err := c.read(&out, "script", "ls", "-s", sandbox)
	return out.Scripts, err
}

// Need is something that stands in the way of a start, by a code unagit can
// ask about, or only a warning.
type Need struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Repository string `json:"repository,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Service    string `json:"service,omitempty"`
	Warning    bool   `json:"warning,omitempty"`
}

// Check is what unibox task check answers.
type Check struct {
	Ready        bool         `json:"ready"`
	Needs        []Need       `json:"needs"`
	Repositories []Repository `json:"repositories"`
}

// Start is a new task to run: the sandbox it goes into (made from Profile
// when it does not exist yet), the agent, and the directories it clones.
type Start struct {
	Sandbox, Profile, Agent string
	Dirs                    []string
	Push                    bool
}

// Args are the arguments task check and run share.
func (s Start) Args() []string {
	var args []string
	args = append(args, s.Dirs...)
	if s.Sandbox != "" {
		args = append(args, "-s", s.Sandbox)
	}
	if s.Profile != "" {
		args = append(args, "-p", s.Profile)
	}
	if s.Agent != "" {
		args = append(args, "--agent", s.Agent)
	}
	if s.Push {
		args = append(args, "--push")
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

// Instructions are the instructions a sandbox's agents get, as unibox
// assembles them.
func (c *Client) Instructions(sandbox, profile string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	args := []string{"instructions"}
	if sandbox != "" {
		args = append(args, "-s", sandbox)
	} else {
		args = append(args, "-p", profile)
	}
	out, err := exec.CommandContext(ctx, c.Binary, args...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("%s", firstLine(string(exit.Stderr), err.Error()))
		}
		return "", err
	}
	return string(out), nil
}

// Diff is what changed in a profile since its image was last built, as a
// unified diff.
func (c *Client) Diff(profile string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Binary, "profile", "diff", profile).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("%s", firstLine(string(exit.Stderr), err.Error()))
		}
		return "", err
	}
	return string(out), nil
}
