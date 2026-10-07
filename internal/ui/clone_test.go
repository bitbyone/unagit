package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestCloneRepositoriesWithoutEditor(t *testing.T) {
	t.Parallel()
	origin := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "--initial-branch=main", origin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, out)
	}
	marker := filepath.Join(t.TempDir(), "editor-opened")
	editor := filepath.Join(t.TempDir(), "editor")
	must(t, os.WriteFile(editor, []byte("#!/bin/sh\nprintf opened > \"$1\"\n"), 0o755))
	visits, _, prepare := fakeZoxide(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool {
		a.cfg.FavouriteEditor, a.cfg.Editor, a.cfg.EditorArgs = "custom", editor, []string{marker}
		for i := range a.projects {
			a.projects[i].HTTPURLToRepo = origin
		}
		return true
	})
	for idx := range 2 {
		if idx > 0 {
			typeRunes(sc, "j")
			waitSelected(t, a, a.projectsPane, idx)
		}
		typeRunes(sc, "C")
		deadline := time.Now().Add(patience)
		for !onLoop(a, func() bool {
			pr := a.projects[idx]
			return a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned && !a.modalOpen()
		}) {
			if time.Now().After(deadline) {
				t.Fatal("clone did not finish and close its log")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !onLoop(a, func() bool {
			row, _ := a.projectsPane.table.GetSelection()
			return a.projectsPane.selectedIndex() == idx && strings.Contains(a.projectsPane.table.GetCell(row, 0).Text, "●")
		}) {
			t.Fatal("clone did not update the selected row")
		}
	}
	for idx := range 2 {
		dir := onLoop(a, func() string { pr := a.projects[idx]; return a.projectDir(pr.Instance, pr.PathWithNamespace) })
		if got := visitCalls(t, visits, "add", dir); got != 0 {
			t.Fatalf("clone-only added %s to zoxide %d times", dir, got)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("editor was started: %v", err)
	}
}

func TestCloneFailureKeepsLogOpen(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing.git")
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool { a.projects[0].HTTPURLToRepo = missing; return true })
	typeRunes(sc, "C")
	waitFor(t, a, sc, "Press Esc to close.")
	if !onLoop(a, func() bool {
		pr := a.projects[0]
		return a.modalOpen() && !a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned
	}) {
		t.Fatal("failed clone did not stay in the log")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/billing")
}
