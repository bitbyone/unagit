package ui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/unibox/uniboxtest"
)

// doctorSaysWebIsOlder is unibox's doctor finding sandbox web older than
// its env, and the sandbox gone from sbx that only prune forgets.
const doctorSaysWebIsOlder = `{"api_version":2,"checks":[
{"area":"env web","name":"sandbox web","status":"warn","message":"the env changed since the sandbox was created","hint":"unibox sandbox recreate web"},
{"area":"Sandboxes","name":"old","status":"warn","message":"sbx no longer has it","hint":"unibox prune forgets it, or unibox sandbox recreate old makes it again"},
{"area":"sbx","name":"signed in","status":"pass","message":"yes"}]}`

// waitUnibox reads unibox again until what it said holds. A read under
// way may have begun before the answer changed, and a refresh asked
// meanwhile is dropped, so it asks again each time none is under way.
func waitUnibox(t *testing.T, a *App, holds func(unibox.Overview) bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for !onLoop(a, func() bool {
		if a.ubx.loading {
			return false
		}
		if a.ubx.read && holds(a.ubx.overview) {
			return true
		}
		a.refreshSandboxes()
		return false
	}) {
		if time.Now().After(deadline) {
			t.Fatal("unibox never said what the test waits for")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDriftIsWhatRecreatingFixes(t *testing.T) {
	t.Parallel()
	var out struct {
		Checks []unibox.DoctorCheck `json:"checks"`
	}
	must(t, json.Unmarshal([]byte(doctorSaysWebIsOlder), &out))
	got := driftOf(out.Checks)
	if len(got) != 1 || !got["web"] {
		t.Errorf("drift = %v, want web alone: a sandbox sbx lost is prune's, not a recreate's", got)
	}
}

func TestMountedWarningsNameTheTasks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	boxes := []unibox.Sandbox{{Name: "web",
		Tasks: []unibox.Task{
			{ID: "t1", Alias: "rate", Repositories: []unibox.Repository{{HostPath: dir, Mounted: true}}},
			{ID: "t2", Repositories: []unibox.Repository{{HostPath: filepath.Join(dir, "sub"), Mounted: true}}},
			{ID: "t3", Repositories: []unibox.Repository{{HostPath: dir + "-other", Mounted: true}}},
			{ID: "t4", Repositories: []unibox.Repository{{HostPath: dir}}},
		},
		Runs: []unibox.Run{{Task: "t2"}}}}
	got := mountedWarnings(boxes, dir)
	if len(got) != 2 || !strings.HasPrefix(got[0], "task t2 ") || !strings.Contains(got[0], "session") || !strings.HasPrefix(got[1], "task rate ") {
		t.Errorf("warnings = %q, want t2 with its session first, then rate; a sibling directory and a clone are no mounts of it", got)
	}
}

// TestDeletingAMountedCloneWarns: the question before deleting a clone
// names the task that mounts it.
func TestDeletingAMountedCloneWarns(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	boxes := strings.ReplaceAll(uniboxtest.Overview()["sandbox ls --json"].Out, "/src/gateway", p.clone)
	fake.Answer(t, "sandbox ls --json", boxes)
	waitUnibox(t, a, func(o unibox.Overview) bool {
		s, ok := o.Sandbox("web")
		return ok && len(s.Tasks) > 0 && len(s.Tasks[0].HostPaths()) > 0 && s.Tasks[0].HostPaths()[0] == p.clone
	})
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete repository")
	waitFor(t, a, sc, "task rate in sandbox web")
}

// TestPullingAnEnvRepositoryOffersRecreate: a pull that moves the team's
// repository an env is read from asks the doctor, which finds the
// sandbox older; the question recreates it, and the tab counts it until
// then.
func TestPullingAnEnvRepositoryOffersRecreate(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{"doctor --json": uniboxtest.JSON(doctorSaysWebIsOlder)})
	a, sc, _ := newTestAppSrv(t, prepare)
	resizeApp(a, sc, 140, 40)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	envs := strings.Replace(uniboxtest.Overview()["env ls --json"].Out, `"repo":"/team"`, `"repo":"`+p.clone+`"`, 1)
	fake.Answer(t, "env ls --json", envs)
	waitUnibox(t, a, func(o unibox.Overview) bool { e := o.Env("web"); return e != nil && e.Repo == p.clone })
	// api lacks a value of the person's: one thing, before anything moved.
	waitFor(t, a, sc, "Sandboxes [5] ● web  "+glyphTabProblem+"1")

	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if out, err := exec.Command("git", "clone", "-q", p.origin, elsewhere).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	commitIn(t, elsewhere, "web.sbxenv.yaml", "the team changed the env")
	gitIn(t, elsewhere, "push", "-q", "origin", "HEAD")

	typeRunes(sc, "p")
	waitCalled(t, a, fake, "doctor --json")
	waitFor(t, a, sc, "Env changed")
	waitFor(t, a, sc, glyphTabProblem+"2")
	if n := onLoop(a, a.sandboxProblems); n != 2 {
		t.Errorf("problems = %d, want api's value and web older than its env", n)
	}
	typeRunes(sc, "y")
	waitCalled(t, a, fake, "sandbox recreate web")
	deadline := time.Now().Add(patience)
	for onLoop(a, a.sandboxProblems) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("web is still counted once it was recreated")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
