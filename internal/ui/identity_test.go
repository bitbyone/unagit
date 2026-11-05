package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// localConfig is a key of a clone's own git configuration.
func localConfig(t *testing.T, dir, key string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "config", "--local", "--get", key).Output()
	return strings.TrimSpace(string(out))
}

// waitCommitsAs waits for every clone to commit as name <email>: the
// message saying so may be the one of an earlier save still on screen.
func waitCommitsAs(t *testing.T, dirs map[string]string, name, email string) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for {
		wrong := ""
		for repo, dir := range dirs {
			if n, e := localConfig(t, dir, "user.name"), localConfig(t, dir, "user.email"); n != name || e != email {
				wrong = repo + " commits as " + n + " <" + e + ">"
			}
		}
		if wrong == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(wrong)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestAGitIdentityReachesTheClonesBelowIt: i on a server in Settings ›
// Groups sets who its clones commit as, and every clone on disk has it at
// once; a group's own address wins below it, the server's name still
// inherited, and the detail says where each comes from.
func TestAGitIdentityReachesTheClonesBelowIt(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	inst := onLoop(a, func() string { return a.projects[0].Instance })
	dirs := map[string]string{}
	for _, repo := range []string{"acme/gateway", "acme/billing"} {
		dir := onLoop(a, func() string { return a.projectDir(inst, repo) })
		must(t, os.MkdirAll(dir, 0o755))
		if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		dirs[repo] = dir
	}
	changeOnLoop(a, a.refreshDisk)

	typeRunes(sc, "S")
	waitFor(t, a, sc, "Default root")
	changeOnLoop(a, func() {
		a.settings.selectSection(sectionGroups)
		a.settings.focusContent()
	})
	waitFor(t, a, sc, "incl. subgroups")
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Git identity · ")
	waitFor(t, a, sc, "Blank inherits git's global one")
	typeRunes(sc, "Jan Work")
	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	typeRunes(sc, "jan@work.example")
	pressButton(t, a, sc, frontForm(a), "Save")
	waitFor(t, a, sc, "written into its 2 clones")
	waitCommitsAs(t, dirs, "Jan Work", "jan@work.example")

	typeRunes(sc, "j") // from the server to the group
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Git identity · acme")
	waitFor(t, a, sc, "Blank inherits Jan Work <jan@work.example>")
	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	typeRunes(sc, "jan@acme.example")
	pressButton(t, a, sc, frontForm(a), "Save")
	waitCommitsAs(t, dirs, "Jan Work", "jan@acme.example")
	if got := onLoop(a, func() string { return a.identityLine(inst, "acme/gateway") }); !strings.Contains(got, "name from the server, address from acme") {
		t.Errorf("the detail says %q", got)
	}
}

// TestTheIdentityFormFits: the form of a repository's identity, opened
// from its actions, in its frame and legible at every size.
func TestTheIdentityFormFits(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	for _, size := range []struct{ w, h int }{{140, 40}, {80, 24}, {60, 20}} {
		resizeApp(a, sc, size.w, size.h)
		changeOnLoop(a, func() {
			pr := a.projects[0]
			a.showIdentityForm(pr.Instance, pr.PathWithNamespace, pr.PathWithNamespace)
		})
		waitFor(t, a, sc, "Git identity · ")
		assertFormFits(t, a, sc, frontForm(a), "Name", "Email", "Blank inherits", "Save", "Inherit", "Cancel")
		assertLegible(t, a, sc, "the identity form")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "Git identity · ")
	}
}
