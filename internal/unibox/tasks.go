package unibox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/tobola/unagit/internal/unibox/api"
)

func taskID(repos []api.Repository) string {
	name := "task"
	if len(repos) > 0 {
		name = repos[0].Name
		if repos[0].HostPath != "" {
			candidate := strings.ToLower(filepath.Base(repos[0].HostPath))
			if ValidName(candidate) == nil {
				name = candidate
			}
		}
	}
	if len(name) > 40 {
		name = name[:40]
	}
	type identity struct{ URL, Branch, Host string }
	ids := []identity{}
	for _, r := range repos {
		ids = append(ids, identity{r.URL, r.Branch, r.HostPath})
	}
	return name + "-" + digest(ids)[:8]
}
func checkNeeds(needs []api.Need) error {
	for _, n := range needs {
		if !n.Warning {
			return Fail(4, n.Code, "%s: %s", n.Repository, n.Message)
		}
	}
	return nil
}

func (m *Manager) task(ctx context.Context, req Request, b *api.Sandbox, p Profile, resume bool) (api.Task, error) {
	// A task resumed by name already knows its agent; asking again would
	// make the record pointless.
	if req.Agent == "" && req.Task != "" && len(req.Repositories) == 0 {
		for _, t := range b.Tasks {
			if t.ID == req.Task {
				req.Agent = t.Agent
			}
		}
	}
	if req.Agent == "" {
		if len(p.Agents) == 1 {
			req.Agent = p.Agents[0]
		} else {
			return api.Task{}, fmt.Errorf("profile %s offers %s; pass --agent", p.Name, strings.Join(p.Agents, ", "))
		}
	}
	if !slices.Contains(p.Agents, req.Agent) {
		return api.Task{}, fmt.Errorf("profile %s does not offer agent %s", p.Name, req.Agent)
	}
	if len(req.Repositories) == 0 && req.Task != "" {
		for _, t := range b.Tasks {
			if t.ID == req.Task {
				t.Agent = req.Agent
				return t, nil
			}
		}
		return api.Task{}, fmt.Errorf("task %s does not exist; supply --repo to prepare it", req.Task)
	}
	if len(req.Repositories) == 0 {
		return api.Task{}, fmt.Errorf("no repository selected; pass --repo <path-or-url> or --task")
	}
	repos, needs, err := ResolveRepositories(ctx, req)
	if err != nil {
		return api.Task{}, err
	}
	if req.Push {
		for _, n := range needs {
			if n.Code == "default_branch" || n.Code == "default_branch_unknown" {
				return api.Task{}, checkNeeds([]api.Need{n})
			}
		}
		if err = PushRepositories(ctx, repos); err != nil {
			return api.Task{}, err
		}
		repos, needs, err = ResolveRepositories(ctx, req)
		if err != nil {
			return api.Task{}, err
		}
	}
	if err = checkNeeds(needs); err != nil {
		return api.Task{}, err
	}
	id := req.Task
	if id == "" {
		id = taskID(repos)
	}
	if err = ValidName(id); err != nil {
		return api.Task{}, err
	}
	t := api.Task{ID: id, Sandbox: b.Name, Directory: "/work/" + id, Repositories: repos, Agent: req.Agent, State: "pending"}
	for _, old := range b.Tasks {
		if old.ID == id {
			if digest(old.Repositories) != digest(repos) {
				return t, fmt.Errorf("task %s belongs to different repositories, branches or authors; choose another --task name", id)
			}
			if resume && old.State == "ready" {
				old.Agent = req.Agent
				return old, nil
			}
		}
	}
	return t, nil
}

func replaceTask(b *api.Sandbox, t api.Task) {
	for i := range b.Tasks {
		if b.Tasks[i].ID == t.ID {
			b.Tasks[i] = t
			return
		}
	}
	b.Tasks = append(b.Tasks, t)
}

// The guard serializes preparation and lifecycle changes. Usage locks survive
// the guard while sessions run, allowing other tasks to prepare beside them.
func (m *Manager) prepare(ctx context.Context, req Request, resume bool) (api.Sandbox, api.Task, Runtime, *FileLock, *FileLock, error) {
	b, r, p, err := m.Definition(req)
	if err != nil {
		return b, api.Task{}, r, nil, nil, err
	}
	guard, err := m.guard(b.Name)
	if err != nil {
		return b, api.Task{}, r, nil, nil, err
	}
	defer guard.Close()
	state, err := m.Paths.ReadState()
	if err != nil {
		return b, api.Task{}, r, nil, nil, err
	}
	if old, ok := state.Sandboxes[b.Name]; ok {
		if req.NewSandbox != "" {
			return b, api.Task{}, r, nil, nil, fmt.Errorf("sandbox %s already exists; use --sandbox", b.Name)
		}
		b = old
	} else {
		if _, exists, err := m.findBox(ctx, r, b); err != nil {
			return b, api.Task{}, r, nil, nil, err
		} else if exists {
			return b, api.Task{}, r, nil, nil, fmt.Errorf("runtime sandbox %s is not owned by unibox; choose another name", b.RuntimeName)
		}
	}
	t, err := m.task(ctx, req, &b, p, resume)
	if err != nil {
		return b, t, r, nil, nil, err
	}
	taskLock, err := LockFile(filepath.Join(m.Paths.Config, "locks", b.Name+"-task-"+t.ID+".lock"), false, false)
	if err != nil {
		return b, t, r, nil, nil, fmt.Errorf("task %s already has an active session; wait for it to finish", t.ID)
	}
	success := false
	var usage *FileLock
	defer func() {
		if !success {
			taskLock.Close()
			if usage != nil {
				usage.Close()
			}
		}
	}()
	if err = m.ensure(ctx, &b, r, p, false, false); err != nil {
		return b, t, r, nil, nil, err
	}
	usage, err = m.usage(b.Name, true)
	if err != nil {
		return b, t, r, nil, nil, err
	}
	// A recreation made every task pending, including a previously ready one.
	for _, saved := range b.Tasks {
		if saved.ID == t.ID && saved.State != "ready" {
			t.State = "pending"
		}
	}
	if !resume || t.State != "ready" {
		t.State = "preparing"
		replaceTask(&b, t)
		if err = m.saveBox(b); err != nil {
			return b, t, r, nil, nil, err
		}
		a, _ := FindAgent(t.Agent)
		fmt.Fprintf(m.Log, "preparing task %s\n", t.ID)
		if err = runLogged(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-s"}, Input: strings.NewReader(prepareScript(t, p, a))}), m.Log); err != nil {
			return b, t, r, nil, nil, err
		}
		t.State = "ready"
	}
	replaceTask(&b, t)
	if err = m.saveBox(b); err != nil {
		return b, t, r, nil, nil, err
	}
	success = true
	return b, t, r, usage, taskLock, nil
}

func (m *Manager) Prepare(ctx context.Context, req Request) (api.Prepared, error) {
	b, t, _, usage, taskLock, err := m.prepare(ctx, req, false)
	if err != nil {
		return api.Prepared{}, err
	}
	defer usage.Close()
	defer taskLock.Close()
	return api.Prepared{APIVersion: api.Version, Sandbox: b.Name, Runtime: b.Runtime, RuntimeName: b.RuntimeName, Task: t.ID, Directory: t.Directory, State: "ready"}, nil
}

func (m *Manager) recordRun(b api.Sandbox, t api.Task, agent string) (api.Run, *FileLock, error) {
	run := api.Run{ID: newRunID(), Sandbox: b.Name, Task: t.ID, Agent: agent, PID: os.Getpid(), Pane: os.Getenv("HERDR_PANE_ID"), Socket: os.Getenv("HERDR_SOCKET_PATH"), Since: time.Now()}
	l, err := m.runLock(run.ID)
	if err != nil {
		return run, nil, err
	}
	err = m.Paths.State(func(s *State) error {
		box := s.Sandboxes[b.Name]
		active := []api.Run{}
		for _, old := range box.Runs {
			if m.runActive(old) {
				active = append(active, old)
			}
		}
		box.Runs = append(active, run)
		s.Sandboxes[b.Name] = box
		return nil
	})
	if err != nil {
		l.Close()
		return run, nil, err
	}
	return run, l, nil
}

func (m *Manager) finishRun(b api.Sandbox, r Runtime, run api.Run, lease, usage, taskLock *FileLock) {
	lease.Close()
	usage.Close()
	if taskLock != nil {
		taskLock.Close()
	}
	if err := m.Paths.State(func(s *State) error {
		box, ok := s.Sandboxes[b.Name]
		if !ok {
			return nil
		}
		box.Runs = slices.DeleteFunc(box.Runs, func(v api.Run) bool { return v.ID == run.ID })
		s.Sandboxes[b.Name] = box
		return nil
	}); err != nil {
		fmt.Fprintln(m.Log, "cannot update session record:", err)
	}
	if !m.Config.StopIdle(r.Name()) {
		return
	}
	guard, err := m.guard(b.Name)
	if err != nil {
		return
	}
	defer guard.Close()
	exclusive, err := m.usage(b.Name, false)
	if err != nil {
		return
	}
	defer exclusive.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := runtimeOutput(ctx, r, b.RuntimeName, "sh", "-c", guestIdleScript); err != nil {
		fmt.Fprintln(m.Log, "sandbox kept running: a guest session may still be active")
		return
	}
	if err = r.Stop(ctx, b.RuntimeName); err != nil {
		fmt.Fprintln(m.Log, "cannot stop idle sandbox:", err)
	} else {
		fmt.Fprintf(m.Log, "sandbox %s stopped: no active sessions\n", b.Name)
	}
}

func RunChild(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case err := <-done:
			return err
		case sig := <-sigs:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}
}

func (m *Manager) Run(ctx context.Context, req Request, in io.Reader, out, errout io.Writer, extra []string) error {
	b, t, r, usage, taskLock, err := m.prepare(ctx, req, true)
	if err != nil {
		return err
	}
	run, lease, err := m.recordRun(b, t, t.Agent)
	if err != nil {
		usage.Close()
		taskLock.Close()
		return err
	}
	defer m.finishRun(b, r, run, lease, usage, taskLock)
	a, _ := FindAgent(t.Agent)
	args := append([]string{a.Command}, a.Args...)
	args = append(args, extra...)
	dir := t.Directory
	if len(t.Repositories) == 1 {
		dir = path.Join(dir, t.Repositories[0].Name)
	}
	cmd := r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-c", sessionScript(run.ID, args)}, Directory: dir, Interactive: true, Input: in, Output: out, Error: errout})
	err = RunChild(cmd)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		if code < 0 {
			code = 128
		}
		return &Failure{code, "agent_exit", fmt.Sprintf("agent exited with status %d", code)}
	}
	return err
}

func (m *Manager) Shell(ctx context.Context, name string, in io.Reader, out, errout io.Writer) error {
	guard, err := m.guard(name)
	if err != nil {
		return err
	}
	b, err := m.sandbox(name)
	if err != nil {
		guard.Close()
		return err
	}
	r, err := m.Runtime(b.Runtime)
	if err != nil {
		guard.Close()
		return err
	}
	if err = r.Available(ctx); err != nil {
		guard.Close()
		return err
	}
	if _, exists, err := m.findBox(ctx, r, b); err != nil || !exists {
		guard.Close()
		if err != nil {
			return err
		}
		return fmt.Errorf("sandbox is missing; run unibox recreate %s", name)
	}
	usage, err := m.usage(name, true)
	guard.Close()
	if err != nil {
		return err
	}
	run, lease, err := m.recordRun(b, api.Task{}, "")
	if err != nil {
		usage.Close()
		return err
	}
	defer m.finishRun(b, r, run, lease, usage, nil)
	return RunChild(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"sh", "-c", sessionScript(run.ID, []string{"bash", "-l"})}, Interactive: true, Input: in, Output: out, Error: errout}))
}

func (m *Manager) Stop(ctx context.Context, name string) error {
	l, err := m.guard(name)
	if err != nil {
		return err
	}
	defer l.Close()
	u, err := m.usage(name, false)
	if err != nil {
		return err
	}
	defer u.Close()
	b, err := m.sandbox(name)
	if err != nil {
		return err
	}
	r, err := m.Runtime(b.Runtime)
	if err != nil {
		return err
	}
	box, exists, err := m.findBox(ctx, r, b)
	if err != nil {
		return err
	}
	if !exists || box.State == "stopped" {
		return nil
	}
	if _, err = runtimeOutput(ctx, r, b.RuntimeName, "sh", "-c", guestIdleScript); err != nil {
		return Fail(4, "guest_busy", "a guest session may still be active; inspect it with unibox shell")
	}
	return r.Stop(ctx, b.RuntimeName)
}
func (m *Manager) Remove(ctx context.Context, name, task string) error {
	l, err := m.guard(name)
	if err != nil {
		return err
	}
	defer l.Close()
	u, err := m.usage(name, false)
	if err != nil {
		return err
	}
	defer u.Close()
	b, err := m.sandbox(name)
	if err != nil {
		return err
	}
	r, err := m.Runtime(b.Runtime)
	if err != nil {
		return err
	}
	_, exists, err := m.findBox(ctx, r, b)
	if err != nil {
		return err
	}
	if exists {
		if err = m.safeBox(ctx, b, r); err != nil {
			return err
		}
	}
	if task != "" {
		if err = ValidName(task); err != nil {
			return err
		}
		index := slices.IndexFunc(b.Tasks, func(t api.Task) bool { return t.ID == task })
		if index < 0 {
			return fmt.Errorf("task %s not found", task)
		}
		if exists {
			if err = runLogged(r.Exec(ctx, b.RuntimeName, Command{Args: []string{"rm", "-rf", "--", b.Tasks[index].Directory}}), m.Log); err != nil {
				return err
			}
		}
		b.Tasks = slices.Delete(b.Tasks, index, index+1)
		return m.saveBox(b)
	}
	if exists {
		if err = r.Remove(ctx, b.RuntimeName); err != nil {
			return err
		}
	}
	return m.Paths.State(func(s *State) error { delete(s.Sandboxes, name); return nil })
}
