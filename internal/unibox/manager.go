package unibox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/unibox/api"
)

type Manager struct {
	Paths    Paths
	Config   Config
	Runtimes map[string]Runtime
	Log      io.Writer
}

func Open(paths Paths, log io.Writer) (*Manager, error) {
	c, err := paths.LoadConfig()
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = io.Discard
	}
	sbx := &SBX{Cache: filepath.Join(paths.Cache, "sbx")}
	if home, err := os.UserHomeDir(); err == nil {
		sbx.Bindings = filepath.Join(home, ".config", "sbx", "credentials.yaml")
	}
	return &Manager{paths, c, map[string]Runtime{"sbx": sbx}, log}, nil
}

func (m *Manager) Runtime(name string) (Runtime, error) {
	if name == "" {
		name = m.Config.DefaultRuntime
	}
	if name == "" {
		for id := range m.Runtimes {
			if m.Config.Enabled(id) {
				if name != "" {
					return nil, fmt.Errorf("more than one runtime is enabled; pass --runtime or set default_runtime")
				}
				name = id
			}
		}
	}
	r, ok := m.Runtimes[name]
	if !ok {
		return nil, fmt.Errorf("runtime %q is not supported; use unibox runtimes to list adapters", name)
	}
	if !m.Config.Enabled(name) {
		return nil, fmt.Errorf("runtime %s is disabled; enable it with unibox config set runtimes.%s.on true", name, name)
	}
	return r, nil
}
func (m *Manager) profile(name string) (Profile, error) {
	return LoadProfile(m.Config.Environments, name)
}
func (m *Manager) Approve(name string) error {
	p, err := m.profile(name)
	if err != nil {
		return err
	}
	return m.Paths.State(func(s *State) error {
		s.Approvals[p.ApprovalKey()] = p.AccessHash()
		s.ApprovedFields[p.ApprovalKey()] = p.HostAccess
		return nil
	})
}
func (m *Manager) Approval(name string) (HostAccess, HostAccess, error) {
	p, err := m.profile(name)
	if err != nil {
		return HostAccess{}, HostAccess{}, err
	}
	s, err := m.Paths.ReadState()
	return s.ApprovedFields[p.ApprovalKey()], p.HostAccess, err
}
func (m *Manager) approved(p Profile) error {
	if !p.HostFacing() {
		return nil
	}
	s, err := m.Paths.ReadState()
	if err != nil {
		return err
	}
	if s.Approvals[p.ApprovalKey()] != p.AccessHash() {
		return Fail(2, "approval_required", "profile %s has unapproved host access; inspect it with unibox check and run unibox approve %s", p.Name, p.Name)
	}
	return nil
}
func buildKey(p Profile, r Runtime) string { return p.ApprovalKey() + ":" + r.Name() }
func imageTag(p Profile) string            { return "unibox/" + p.Name + ":" + p.Revision[:16] }

func (m *Manager) Built(ctx context.Context, p Profile, r Runtime) (BuildRecord, error) {
	s, err := m.Paths.ReadState()
	if err != nil {
		return BuildRecord{}, err
	}
	record, ok := s.Builds[buildKey(p, r)]
	if !ok || record.Revision != p.Revision {
		return BuildRecord{}, Fail(3, "image_required", "profile %s needs a build; run unibox build %s --runtime %s", p.Name, p.Name, r.Name())
	}
	images, err := r.Images(ctx)
	if err != nil {
		return BuildRecord{}, err
	}
	for _, im := range images {
		if im.Reference == imageTag(p) {
			return record, nil
		}
	}
	return BuildRecord{}, Fail(3, "image_required", "profile %s image is missing from %s; run unibox build %s", p.Name, r.Name(), p.Name)
}
func (m *Manager) Build(ctx context.Context, name, rt string, force bool) error {
	p, err := m.profile(name)
	if err != nil {
		return err
	}
	r, err := m.Runtime(rt)
	if err != nil {
		return err
	}
	l, err := LockFile(filepath.Join(m.Paths.Config, "locks", "build-"+buildKey(p, r)+".lock"), false, false)
	if err != nil {
		return err
	}
	defer l.Close()
	if !force {
		if _, err = m.Built(ctx, p, r); err == nil {
			fmt.Fprintf(m.Log, "image for %s is current\n", name)
			return nil
		}
	}
	fmt.Fprintf(m.Log, "building %s for %s\n", name, r.Name())
	id, err := r.Build(ctx, p, imageTag(p), m.Log)
	if err != nil {
		return err
	}
	return m.Paths.State(func(s *State) error { s.Builds[buildKey(p, r)] = BuildRecord{p.Revision, id, p.Files}; return nil })
}

func (m *Manager) Profiles(ctx context.Context) ([]api.Profile, error) {
	names, err := ProfileNames(m.Config.Environments)
	if err != nil {
		return nil, err
	}
	state, err := m.Paths.ReadState()
	if err != nil {
		return nil, err
	}
	out := []api.Profile{}
	for _, name := range names {
		row := api.Profile{Name: name, Images: map[string]string{}}
		p, err := m.profile(name)
		if err != nil {
			row.Error = err.Error()
			out = append(out, row)
			continue
		}
		row.Agents = p.Agents
		row.Revision = p.Revision
		row.Approved = !p.HostFacing() || state.Approvals[p.ApprovalKey()] == p.AccessHash()
		row.LastCommit, _ = git(ctx, p.Root, "log", "-1", "--format=%h %s", "--", name)
		for id, r := range m.Runtimes {
			if !m.Config.Enabled(id) {
				continue
			}
			row.Images[id] = "not built"
			if b, ok := state.Builds[buildKey(p, r)]; ok {
				row.Images[id] = "outdated"
				if b.Revision == p.Revision {
					row.Images[id] = "built"
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func (m *Manager) sandbox(name string) (api.Sandbox, error) {
	if err := ValidName(name); err != nil {
		return api.Sandbox{}, err
	}
	s, err := m.Paths.ReadState()
	if err != nil {
		return api.Sandbox{}, err
	}
	b, ok := s.Sandboxes[name]
	if !ok {
		return b, fmt.Errorf("sandbox %s is not registered; create it with unibox create", name)
	}
	return b, nil
}
func (m *Manager) saveBox(b api.Sandbox) error {
	return m.Paths.State(func(s *State) error { s.Sandboxes[b.Name] = b; return nil })
}
func (m *Manager) guard(name string) (*FileLock, error) {
	return LockFile(filepath.Join(m.Paths.Config, "locks", name+"-guard.lock"), false, false)
}
func (m *Manager) usage(name string, shared bool) (*FileLock, error) {
	return LockFile(filepath.Join(m.Paths.Config, "locks", name+"-usage.lock"), shared, false)
}

// A task's name is unique across sandboxes, so naming the task is enough to
// find it. State from before that rule may still hold one name twice.
func (m *Manager) taskSandbox(task string, preparing bool) (string, error) {
	s, err := m.Paths.ReadState()
	if err != nil {
		return "", err
	}
	found := []string{}
	for name, b := range s.Sandboxes {
		for _, t := range b.Tasks {
			if t.ID == task {
				found = append(found, name)
			}
		}
	}
	slices.Sort(found)
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) > 1:
		return "", fmt.Errorf("task %s exists in sandboxes %s; remove the one not wanted with unibox rm-task", task, strings.Join(found, ", "))
	case preparing:
		return "", nil
	}
	return "", fmt.Errorf("task %s does not exist; unibox tasks lists them", task)
}

func (m *Manager) Definition(req Request) (api.Sandbox, Runtime, Profile, error) {
	if req.Sandbox == "" && req.NewSandbox == "" && req.Profile == "" && req.Task != "" {
		name, err := m.taskSandbox(req.Task, len(req.Repositories) > 0)
		if err != nil {
			return api.Sandbox{}, nil, Profile{}, err
		}
		req.Sandbox = name
	}
	name := req.Sandbox
	if name == "" {
		name = req.NewSandbox
	}
	if name != "" && req.NewSandbox == "" {
		b, err := m.sandbox(name)
		if err != nil {
			return b, nil, Profile{}, err
		}
		if req.Runtime != "" && req.Runtime != b.Runtime {
			return b, nil, Profile{}, fmt.Errorf("sandbox %s belongs to runtime %s", name, b.Runtime)
		}
		if req.Profile != "" && req.Profile != b.Profile {
			return b, nil, Profile{}, fmt.Errorf("sandbox %s uses profile %s", name, b.Profile)
		}
		r, err := m.Runtime(b.Runtime)
		if err != nil {
			return b, nil, Profile{}, err
		}
		p, err := m.profile(b.Profile)
		return b, r, p, err
	}
	if req.Profile == "" {
		if name == "" {
			return api.Sandbox{}, nil, Profile{}, fmt.Errorf("no sandbox chosen; pass --sandbox (unibox ls lists them) or --profile to make one (unibox profiles)")
		}
		return api.Sandbox{}, nil, Profile{}, fmt.Errorf("new sandbox %s needs --profile; list profiles with unibox profiles", name)
	}
	if name == "" {
		name = req.Profile
	}
	if err := ValidName(name); err != nil {
		return api.Sandbox{}, nil, Profile{}, err
	}
	r, err := m.Runtime(req.Runtime)
	if err != nil {
		return api.Sandbox{}, nil, Profile{}, err
	}
	p, err := m.profile(req.Profile)
	if err != nil {
		return api.Sandbox{}, nil, Profile{}, err
	}
	if _, err = p.InstructionText(req.Instructions); err != nil {
		return api.Sandbox{}, nil, Profile{}, err
	}
	b := api.Sandbox{Name: name, Runtime: r.Name(), RuntimeName: "unibox-" + name, Profile: p.Name, Instructions: req.Instructions, State: "missing", Tasks: []api.Task{}, Runs: []api.Run{}}
	return b, r, p, nil
}

func (m *Manager) Check(ctx context.Context, req Request) (api.Check, error) {
	result := api.Check{APIVersion: api.Version, Ready: true, Needs: []api.Need{}, Repositories: []api.Repository{}}
	b, r, p, err := m.Definition(req)
	if err != nil {
		return result, err
	}
	if err = m.approved(p); err != nil {
		result.Needs = append(result.Needs, api.Need{Code: "approval_required", Message: err.Error()})
	}
	if err = r.Available(ctx); err != nil {
		result.Needs = append(result.Needs, api.Need{Code: "runtime_unavailable", Message: err.Error()})
	} else if _, err = m.Built(ctx, p, r); err != nil {
		result.Needs = append(result.Needs, api.Need{Code: "image_required", Message: err.Error()})
	}
	if req.Agent != "" && !slices.Contains(p.Agents, req.Agent) {
		return result, fmt.Errorf("profile %s does not offer agent %s", p.Name, req.Agent)
	}
	if a, ok := r.(Authenticator); ok {
		result.Needs = append(result.Needs, a.AuthNeeds(ctx, b.RuntimeName, p)...)
	}
	repos, needs, err := ResolveRepositories(ctx, req)
	if err != nil {
		return result, err
	}
	result.Repositories = repos
	result.Needs = append(result.Needs, needs...)
	for _, r := range repos {
		if r.HostPath == "" {
			result.Needs = append(result.Needs, api.Need{Code: "remote_check_pending", Repository: r.Name, Message: "remote branch and credentials will be checked inside the sandbox during preparation", Warning: true})
		}
	}
	for _, n := range result.Needs {
		if !n.Warning {
			result.Ready = false
		}
	}
	return result, nil
}

// Login takes an agent's sign-in as the profile declares it, on the user's
// terminal, through the runtime that keeps it.
func (m *Manager) Login(ctx context.Context, req Request, agent string, in io.Reader, out, errout io.Writer) error {
	_, r, p, err := m.Definition(req)
	if err != nil {
		return err
	}
	a, ok := r.(Authenticator)
	if !ok {
		return fmt.Errorf("runtime %s keeps no logins; sign %s in inside the sandbox", r.Name(), agent)
	}
	auth, ok := p.Auth[agent]
	if !ok {
		return fmt.Errorf("profile %s declares no auth for %s", p.Name, agent)
	}
	return a.Login(ctx, agent, auth, in, out, errout)
}

func (m *Manager) findBox(ctx context.Context, r Runtime, b api.Sandbox) (Box, bool, error) {
	boxes, err := r.List(ctx)
	if err != nil {
		return Box{}, false, err
	}
	for _, v := range boxes {
		if v.Name == b.RuntimeName {
			if b.RuntimeID != "" && v.ID != "" && b.RuntimeID != v.ID {
				return v, true, fmt.Errorf("sandbox %s was replaced outside unibox; inspect it before removing its unibox registration", b.Name)
			}
			return v, true, nil
		}
	}
	return Box{}, false, nil
}

func (m *Manager) ensure(ctx context.Context, b *api.Sandbox, r Runtime, p Profile, force, rebuild bool) error {
	if err := r.Available(ctx); err != nil {
		return err
	}
	if err := m.approved(p); err != nil {
		return err
	}
	if rebuild {
		if err := m.Build(ctx, p.Name, r.Name(), true); err != nil {
			return err
		}
	}
	build, err := m.Built(ctx, p, r)
	if err != nil {
		return err
	}
	instructions, err := p.InstructionText(b.Instructions)
	if err != nil {
		return err
	}
	env := map[string]string{}
	for _, k := range p.Env {
		value, ok := os.LookupEnv(k)
		if !ok {
			return fmt.Errorf("profile requires %s; set it before starting", k)
		}
		env[k] = value
	}
	settings := p
	settings.Instructions = nil
	revision := digest(struct {
		Profile Profile
		Image   string
		Env     map[string]string
	}{settings, build.Image, env})
	instructionRevision := digest(instructions)
	var exclusive *FileLock
	defer func() {
		if exclusive != nil {
			exclusive.Close()
		}
	}()
	_, exists, err := m.findBox(ctx, r, *b)
	if err != nil {
		return err
	}
	if force || !exists || b.Revision != revision {
		exclusive, err = m.usage(b.Name, false)
		if err != nil {
			return fmt.Errorf("sandbox %s needs recreation but is in use; retry after its sessions finish", b.Name)
		}
		if exists {
			if err = m.safeBox(ctx, *b, r); err != nil {
				return err
			}
			fmt.Fprintf(m.Log, "recreating sandbox %s\n", b.Name)
			if err = r.Remove(ctx, b.RuntimeName); err != nil {
				return err
			}
		}
		b.RuntimeID = ""
		b.Revision = ""
		b.State = "creating"
		if err = m.saveBox(*b); err != nil {
			return err
		}
		kind := ""
		if len(p.Agents) == 1 {
			kind = p.Agents[0]
		}
		if err = r.Create(ctx, Spec{Name: b.RuntimeName, Image: imageTag(p), CPUs: p.CPUs, Memory: p.Memory, Access: p.HostAccess, Agent: kind, Agents: p.Agents, OAuth: p.OAuthAgent()}, m.Log); err != nil {
			return err
		}
		box, ok, err := m.findBox(ctx, r, *b)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("runtime did not list the new sandbox; inspect it with its own CLI")
		}
		b.RuntimeID = box.ID
		cmd := r.Exec(ctx, b.RuntimeName, Command{User: "root", Args: []string{"sh", "-c", "set -eu; mkdir -p /work /var/lib/unibox/runs; chown -R agent:agent /work /var/lib/unibox"}})
		if err = runLogged(cmd, m.Log); err != nil {
			return err
		}
		for i := range b.Tasks {
			b.Tasks[i].State = "pending"
		}
		b.Revision = revision
		b.InstructionRevision = ""
		b.Memory = p.Memory
		b.State = "running"
		if err = m.saveBox(*b); err != nil {
			return err
		}
	}
	if b.InstructionRevision != instructionRevision && exclusive == nil {
		exclusive, err = m.usage(b.Name, false)
		if err != nil {
			return fmt.Errorf("sandbox instructions changed while a session is active; retry when it finishes")
		}
	}
	if err = runLogged(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-s"}, Input: strings.NewReader(initializeScript(p, instructions))}), m.Log); err != nil {
		return err
	}
	if err = m.seedAgentConfigs(ctx, *b, r, p); err != nil {
		return err
	}
	b.InstructionRevision = instructionRevision
	return m.saveBox(*b)
}

// mergeJSON lays the seed over the current configuration: a key the seed
// names wins, maps merge recursively, and everything else - the agent's own
// state, projects, paths - stays as it is.
func mergeJSON(current, seed map[string]any) map[string]any {
	for key, value := range seed {
		if sm, ok := value.(map[string]any); ok {
			if cm, ok := current[key].(map[string]any); ok {
				current[key] = mergeJSON(cm, sm)
				continue
			}
		}
		current[key] = value
	}
	return current
}

func (m *Manager) seedAgentConfigs(ctx context.Context, b api.Sandbox, r Runtime, p Profile) error {
	for _, id := range p.Agents {
		file, ok := p.AgentConfig[id]
		if !ok {
			continue
		}
		a, _ := FindAgent(id)
		data, err := readInside(p.Root, filepath.Join(p.Dir, file))
		if err != nil {
			return fmt.Errorf("profile agent_config: %w", err)
		}
		var seed map[string]any
		if err = json.Unmarshal(data, &seed); err != nil {
			return fmt.Errorf("agent_config %s must be a JSON object: %v", file, err)
		}
		out, err := runtimeOutput(ctx, r, b.RuntimeName, "sh", "-c", "cat \"$HOME/"+a.Config+"\" 2>/dev/null || true")
		if err != nil {
			return err
		}
		current := map[string]any{}
		if out != "" {
			if err = json.Unmarshal([]byte(out), &current); err != nil {
				return fmt.Errorf("agent %s configuration in sandbox %s is not valid JSON; repair or remove ~/%s before seeding it", id, b.Name, a.Config)
			}
		}
		before, _ := json.Marshal(current)
		mergedMap := mergeJSON(current, seed)
		after, _ := json.Marshal(mergedMap)
		if bytes.Equal(before, after) {
			continue
		}
		merged, err := json.MarshalIndent(mergedMap, "", "  ")
		if err != nil {
			return err
		}
		script := "set -eu\nmkdir -p \"$HOME/" + path.Dir(a.Config) + "\"\nprintf %s " + Quote(base64.StdEncoding.EncodeToString(append(merged, '\n'))) + " | base64 -d > \"$HOME/" + a.Config + "\"\n"
		if err = runLogged(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-s"}, Input: strings.NewReader(script)}), m.Log); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) AttachInstructions(name string, names []string) error {
	guard, err := m.guard(name)
	if err != nil {
		return err
	}
	defer guard.Close()
	b, err := m.sandbox(name)
	if err != nil {
		return err
	}
	p, err := m.profile(b.Profile)
	if err != nil {
		return err
	}
	if _, err = p.InstructionText(names); err != nil {
		return err
	}
	b.Instructions = names
	return m.saveBox(b)
}

func (m *Manager) Create(ctx context.Context, req Request) (api.Sandbox, error) {
	if req.NewSandbox == "" {
		req.NewSandbox = req.Sandbox
		req.Sandbox = ""
	}
	b, r, p, err := m.Definition(req)
	if err != nil {
		return b, err
	}
	l, err := m.guard(b.Name)
	if err != nil {
		return b, err
	}
	defer l.Close()
	s, err := m.Paths.ReadState()
	if err != nil {
		return b, err
	}
	if _, ok := s.Sandboxes[b.Name]; ok {
		return b, fmt.Errorf("sandbox %s already exists; use run or recreate", b.Name)
	}
	if _, exists, err := m.findBox(ctx, r, b); err != nil {
		return b, err
	} else if exists {
		return b, fmt.Errorf("runtime name %s is already in use; choose another sandbox name", b.RuntimeName)
	}
	if err = m.ensure(ctx, &b, r, p, false, false); err != nil {
		return b, err
	}
	return b, nil
}

func (m *Manager) safeBox(ctx context.Context, b api.Sandbox, r Runtime) error {
	if err := runLogged(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-s"}, Input: strings.NewReader(guestIdleScript)}), m.Log); err != nil {
		return Fail(4, "guest_busy", "sandbox %s may still have a running guest session; inspect it with unibox shell", b.Name)
	}
	for _, t := range b.Tasks {
		if err := runLogged(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-s"}, Input: strings.NewReader(safeToRemoveScript(t))}), m.Log); err != nil {
			return Fail(4, "unpushed_work", "task %s may contain unpushed work; preserve it before recreating or removing the sandbox", t.ID)
		}
	}
	return nil
}

func (m *Manager) Recreate(ctx context.Context, name string, rebuild bool) error {
	l, err := m.guard(name)
	if err != nil {
		return err
	}
	defer l.Close()
	b, err := m.sandbox(name)
	if err != nil {
		return err
	}
	r, err := m.Runtime(b.Runtime)
	if err != nil {
		return err
	}
	p, err := m.profile(b.Profile)
	if err != nil {
		return err
	}
	if err = m.approved(p); err != nil {
		return err
	}
	if !rebuild {
		if _, err = m.Built(ctx, p, r); err != nil {
			if err = m.Build(ctx, p.Name, r.Name(), false); err != nil {
				return err
			}
		}
	}
	if err = m.ensure(ctx, &b, r, p, true, rebuild); err != nil {
		return err
	}
	for i := range b.Tasks {
		a, err := FindAgent(b.Tasks[i].Agent)
		if err != nil {
			return err
		}
		if err = runLogged(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-s"}, Input: strings.NewReader(prepareScript(b.Tasks[i], p, a))}), m.Log); err != nil {
			return err
		}
		b.Tasks[i].State = "ready"
		if err = m.saveBox(b); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) List(ctx context.Context) ([]api.Sandbox, error) {
	s, err := m.Paths.ReadState()
	if err != nil {
		return nil, err
	}
	out := []api.Sandbox{}
	for _, b := range s.Sandboxes {
		r, err := m.Runtime(b.Runtime)
		if err != nil {
			b.State = "unavailable"
		} else {
			box, ok, err := m.findBox(ctx, r, b)
			switch {
			case err != nil:
				b.State = "unknown"
			case !ok:
				b.State = "missing"
			default:
				b.State = box.State
				b.Memory = box.Memory
			}
		}
		runs := []api.Run{}
		for _, run := range b.Runs {
			if m.runActive(run) {
				runs = append(runs, run)
			}
		}
		b.Runs = runs
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b api.Sandbox) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (m *Manager) runLock(id string) (*FileLock, error) {
	return LockFile(filepath.Join(m.Paths.Config, "locks", "run-"+id+".lock"), false, false)
}
func (m *Manager) runActive(run api.Run) bool {
	l, err := m.runLock(run.ID)
	if err != nil {
		return true
	}
	l.Close()
	return false
}
func newRunID() string { return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()) }
