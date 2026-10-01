package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// fakeHunk puts a hunk on PATH that writes down where it ran, with what, and
// the patch it was handed.
func fakeHunk(t *testing.T) (log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	script := "#!/bin/sh\necho \"$PWD $*\" >> " + log + "\nif [ \"$1\" = patch ]; then cat \"$2\" >> " + log + "; fi\n"
	must(t, os.WriteFile(filepath.Join(dir, "hunk"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func waitForLog(t *testing.T, log string, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(log)
		ok := true
		for _, w := range want {
			ok = ok && strings.Contains(string(data), w)
		}
		if ok {
			return string(data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(log)
	t.Fatalf("hunk was not run with %q; it saw:\n%s", want, data)
	return ""
}

// TestDShowsTheChangesInHunk: D needs the integration; on, it runs hunk diff
// in a clone, and a grouped worktree's repositories as one patch.
func TestDShowsTheChangesInHunk(t *testing.T) {
	log := fakeHunk(t)
	a, sc, _ := newTestAppSrv(t)
	gw, _, form := markBoth(t, a, sc)

	// The group first, made the usual way.
	typeRunes(sc, "feat/look")
	waitFor(t, a, sc, "feat-look")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(a.cfg.Root(), ".unagit", "groups", "feat-look")
	must(t, os.WriteFile(filepath.Join(dir, "gateway", "a.txt"), []byte("changed\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "billing", "new.txt"), []byte("new\n"), 0o644))

	// On by default, since hunk is installed; turned off, D says where to
	// turn it on again.
	off := false
	onLoop(a, func() bool { a.cfg.Integrations.Hunk = &off; return true })
	typeRunes(sc, "D")
	waitFor(t, a, sc, "enable it in Settings")
	onLoop(a, func() bool { a.cfg.Integrations.Hunk = nil; return true })

	typeRunes(sc, "D")
	got := waitForLog(t, log, "patch", "+++ b/gateway/a.txt", "+++ b/billing/new.txt")
	if !strings.Contains(got, "feat-look patch") {
		t.Errorf("hunk did not run in the group's folder:\n%s", got)
	}

	// A clone: hunk diff, in the clone.
	must(t, os.WriteFile(filepath.Join(gw.clone, "a.txt"), []byte("edited\n"), 0o644))
	typeRunes(sc, "R")
	waitFor(t, a, sc, "REPOSITORY")
	typeRunes(sc, "g")
	typeRunes(sc, "D")
	waitForLog(t, log, gw.clone+" diff")
}

// TestIntegrationsKeepTheCardInView: on a short terminal the cards that do not
// fit are left out, so the one with the cursor is whole and nothing is drawn
// over the frame.
func TestIntegrationsKeepTheCardInView(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resize(sc, size.w, size.h)
			openSection(t, a, sc, sectionIntegrations)
			typeRunes(sc, "jj") // Incomm, Editors, Hunk
			waitFor(t, a, sc, "e toggle")
			text := a.screenText(sc)
			for _, want := range []string{"Hunk", "D opens what", "c check"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			lines := strings.Split(text, "\n")
			// The panel's bottom border is the last line but the status line.
			if last := lines[len(lines)-3]; !strings.Contains(last, "╰") {
				t.Errorf("the frame's bottom is drawn over:\n%s", text)
			}
			assertLegible(t, a, sc, "the Hunk card")
		})
	}
}

// TestAltDOffersSinceTheBaseAndEachCommit: D is what is not committed; Alt-D
// lists that, the whole branch since its base, and its commits one by one.
func TestAltDOffersSinceTheBaseAndEachCommit(t *testing.T) {
	log := fakeHunk(t)
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/x")
	gitIn(t, p.clone, "config", "branch.feat/x.unagitBase", "main")
	commitIn(t, dir, "x.txt", "Add the thing")
	sha := gitIn(t, dir, "rev-parse", "HEAD")
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("edited\n"), 0o644))
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "feat/x")

	typeRunes(sc, "D")
	got := waitForLog(t, log, " diff")
	if strings.Contains(got, sha) {
		t.Errorf("D went further than what is not committed: %s", got)
	}

	sc.InjectKey(tcell.KeyRune, 'd', tcell.ModAlt)
	waitFor(t, a, sc, "Show in Hunk")
	for _, want := range []string{"Not committed", "Since origin/main", "Add the thing"} {
		waitFor(t, a, sc, want)
	}
	typeRunes(sc, "jj") // Not committed, Since, the commit
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitForLog(t, log, "show "+sha)
}
