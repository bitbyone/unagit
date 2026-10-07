package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/editortest"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

func yaziQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

type fakeYaziTool struct{ log, cwd, chosen, gate, init string }

func fakeYazi(t *testing.T) (fakeYaziTool, func(*App)) {
	t.Helper()
	dir := t.TempDir()
	f := fakeYaziTool{filepath.Join(dir, "calls"), filepath.Join(dir, "cwd"), filepath.Join(dir, "chosen"), filepath.Join(dir, "gate"), filepath.Join(dir, "init.lua")}
	bin := filepath.Join(dir, "yazi")
	script := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" >> " + yaziQuote(f.log) + "\nwhile [ -e " + yaziQuote(f.gate) + " ]; do sleep 0.02; done\ncat " + yaziQuote(f.cwd) + " > \"$2\"\ncat " + yaziQuote(f.chosen) + " > \"$4\"\n"
	must(t, os.WriteFile(bin, []byte(script), 0o755))
	for _, path := range []string{f.cwd, f.chosen, f.init} {
		must(t, os.WriteFile(path, nil, 0o600))
	}
	t.Cleanup(func() { os.Remove(f.gate) })
	return f, func(a *App) {
		previous := a.findExecutable
		a.findExecutable = func(name string) (string, error) {
			if name == "yazi" {
				return bin, nil
			}
			if previous != nil {
				return previous(name)
			}
			if name == "zoxide" {
				return "", exec.ErrNotFound
			}
			return exec.LookPath(name)
		}
		a.yaziInitPath = func() string { return f.init }
	}
}

func yaziFavourite(t *testing.T, a *App) string {
	t.Helper()
	dir := t.TempDir()
	bin, log := filepath.Join(dir, "editor"), filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > " + yaziQuote(log) + "\n"
	must(t, os.WriteFile(bin, []byte(script), 0o755))
	changeOnLoop(a, func() {
		a.cfg.Editor = bin
		a.cfg.EditorArgs = []string{"--test"}
		a.cfg.FavouriteEditor = editors.Custom
	})
	return log
}

func TestYaziChoosesAFileAndRemembersWhereItEnded(t *testing.T) {
	t.Parallel()
	visits, _, zprepare := fakeZoxide(t)
	y, prepare := fakeYazi(t)
	a, sc, _ := newTestAppSrv(t, zprepare, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	editor := yaziFavourite(t, a)
	last := filepath.Join(t.TempDir(), "another folder ")
	must(t, os.Mkdir(last, 0o700))
	file := filepath.Join(p.clone, "a file's.go")
	must(t, os.WriteFile(y.cwd, []byte(last+"\n"), 0o600))
	must(t, os.WriteFile(y.chosen, []byte(file+"\n"), 0o600))
	must(t, os.WriteFile(y.gate, nil, 0o600))
	must(t, os.RemoveAll(p.clone))
	changeOnLoop(a, func() {
		for i := range a.projects {
			if a.projects[i].PathWithNamespace == p.path {
				a.projects[i].HTTPURLToRepo = p.origin
			}
		}
		a.reindexProjects()
		a.browseProject(a.projByKey[projectKey{a.cfg.Instances[0].ID, p.path}])
	})
	logged := waitForLog(t, y.log, "--chooser-file")
	must(t, os.WriteFile(file, []byte("package main\n"), 0o600))
	rows := a.sessions.List()
	if len(rows) != 1 || rows[0].Editor != "yazi" || !sameDirectory(rows[0].Dir, p.clone) || rows[0].Mode != session.ModeRepository {
		t.Fatalf("browser session: %+v", rows)
	}
	args := strings.Split(strings.TrimSuffix(logged, "\n"), "\n")
	tmp := filepath.Dir(args[2])
	fi, err := os.Stat(tmp)
	must(t, err)
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("temporary directory mode: %v", fi.Mode())
	}
	must(t, os.Remove(y.gate))
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "opened "+p.clone) })
	waitEditorIdle(t, a)
	got := editorLog(t, editor)
	if got != p.clone+"\n--test\n"+file+"\n" {
		t.Fatalf("editor arguments: %q", got)
	}
	if visitCalls(t, visits, "add", last) != 1 {
		t.Fatalf("last directory was not recorded:\n%s", editorLog(t, visits))
	}
	if visitCalls(t, visits, "add", p.clone) != 2 {
		t.Fatal("browser and editor did not each record the original directory")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temporary directory remains: %v", err)
	}
	if rows := a.sessions.List(); len(rows) != 0 {
		t.Fatalf("finished terminal sessions remain: %+v", rows)
	}
}

func TestYaziCancelKeepsTheEditorClosedAndDisabledDoesNotClone(t *testing.T) {
	t.Parallel()
	y, prepare := fakeYazi(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	editor := yaziFavourite(t, a)
	changeOnLoop(a, func() { a.browseProject(a.projByKey[projectKey{a.cfg.Instances[0].ID, p.path}]) })
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "browsed "+p.clone) })
	waitEditorIdle(t, a)
	if _, err := os.Stat(editor); !os.IsNotExist(err) {
		t.Fatal("cancel started an editor")
	}
	before := editorLog(t, y.log)
	changeOnLoop(a, func() {
		off := false
		a.cfg.Integrations.Yazi = &off
		a.browseProject(a.projByKey[projectKey{a.cfg.Instances[0].ID, "acme/billing"}])
	})
	waitFor(t, a, sc, "yazi is disabled")
	if got := editorLog(t, y.log); got != before {
		t.Fatal("disabled integration launched yazi")
	}
	billing := onLoop(a, func() string { return a.projectDir(a.cfg.Instances[0].ID, "acme/billing") })
	if workspace.Exists(billing) {
		t.Fatal("disabled integration cloned the repository")
	}
}

func TestYaziPreparesAMissingReviewAndPrefersItToTheBranch(t *testing.T) {
	t.Parallel()
	y, prepare := fakeYazi(t)
	a, sc, srv := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	mrOnOrigin(t, srv, p, "Add rate limiting")
	must(t, os.RemoveAll(p.clone))
	var mr forge.MergeRequest
	changeOnLoop(a, func() {
		for i := range a.projects {
			if a.projects[i].PathWithNamespace == p.path {
				a.projects[i].HTTPURLToRepo = p.origin
			}
		}
		a.reindexProjects()
		for _, item := range a.mrs {
			if item.IID == 7 {
				mr = item
				break
			}
		}
		a.browseMR(mr)
	})
	review := onLoop(a, func() string { return a.reviewDir(a.cfg.Instances[0].ID, p.path, mr.IID, mr.SourceBranch) })
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "browsed "+review) })
	waitEditorIdle(t, a)
	if !workspace.Exists(p.clone) || !workspace.Exists(review) {
		t.Fatal("review preparation did not clone and create a worktree")
	}
	branch := onLoop(a, func() string { return a.mrDir(a.cfg.Instances[0].ID, p.path, mr.IID, mr.SourceBranch) })
	gitIn(t, p.clone, "worktree", "add", "-q", "--detach", branch)
	must(t, os.WriteFile(y.log, nil, 0o600))
	changeOnLoop(a, func() { a.clearSaid(); a.browseMR(mr) })
	waitForLog(t, y.log, review)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "browsed "+review) })
	waitEditorIdle(t, a)
	if strings.Contains(editorLog(t, y.log), branch) {
		t.Fatal("branch was used when a review exists")
	}
	must(t, onLoop(a, func() *workspace.Manager { return a.newManager(a.cfg.Instances[0].ID, p.path, nil) }).RemoveWorktreeDir(p.path, review))
	changeOnLoop(a, func() { a.clearSaid(); a.browseMR(mr) })
	waitForLog(t, y.log, branch)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "browsed "+branch) })
}

func TestYaziCardAndSelectionActionFit(t *testing.T) {
	t.Parallel()
	y, prepare := fakeYazi(t)
	must(t, os.WriteFile(y.init, []byte("require(\"zoxide\"):setup { update_db = true }\n"), 0o600))
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		resizeApp(a, sc, size.w, size.h)
		openSection(t, a, sc, sectionIntegrations)
		changeOnLoop(a, func() {
			v := a.settings.integrations
			v.current = len(v.cards) - 1
			a.tv.SetFocus(v)
			v.paintFocus(true)
		})
		waitFor(t, a, sc, "Yazi zoxide update_db found")
		text := a.screenText(sc)
		for _, want := range []string{"Yazi", "Browse Files", "e toggle", "c check"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%q absent:\n%s", want, text)
			}
		}
		lines := strings.Split(text, "\n")
		if !strings.Contains(lines[len(lines)-3], "╰") {
			t.Fatalf("bottom border overwritten:\n%s", text)
		}
		t.Logf("Yazi at %dx%d:\n%s", size.w, size.h, text)
		assertLegible(t, a, sc, "Yazi integration")
	}
	typeRunes(sc, "e")
	waitFor(t, a, sc, "disabled")
	saved, err := config.LoadFrom(onLoop(a, func() string { return a.cfg.Dir() }))
	must(t, err)
	if saved.Integrations.Yazi == nil || *saved.Integrations.Yazi {
		t.Fatal("disabled choice was not saved")
	}
	typeRunes(sc, "e")
	waitFor(t, a, sc, "enabled")
	typeRunes(sc, "1")
	sc.InjectKey(tcell.KeyCtrlA, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Browse Files")
	assertLegible(t, a, sc, "Browse Files action")
}

func TestYaziChosenFileAttachesToTheRunningNeovim(t *testing.T) {
	_, log := editortest.Install(t)
	y, prepare := fakeYazi(t)
	a, sc, _ := newTestAppSrv(t, prepare, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "nvim aside:")
	waitEditorIdle(t, a)
	file := filepath.Join(p.clone, "a file's.go")
	must(t, os.WriteFile(y.chosen, []byte(file+"\n"), 0o600))
	changeOnLoop(a, func() { a.clearSaid(); a.browseProject(a.projByKey[projectKey{a.cfg.Instances[0].ID, p.path}]) })
	waitForEditorLog(t, log, "file|execute('tabedit ")
	waitForEditorLog(t, log, "attach|")
	waitFor(t, a, sc, "nvim aside:")
	waitEditorIdle(t, a)
	if strings.Count(editorLog(t, log), "start|") != 1 {
		t.Fatal("chosen file started a second Neovim")
	}
}

func TestYaziChosenFileSurvivesChoosingAnEditor(t *testing.T) {
	t.Parallel()
	y, prepare := fakeYazi(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	editor := yaziFavourite(t, a)
	file := filepath.Join(p.clone, "chosen.go")
	must(t, os.WriteFile(y.chosen, []byte(file+"\n"), 0o600))
	changeOnLoop(a, func() {
		a.cfg.FavouriteEditor = askEveryTime
		a.browseProject(a.projByKey[projectKey{a.cfg.Instances[0].ID, p.path}])
	})
	waitFor(t, a, sc, "Open with")
	typeRunes(sc, "/Custom:")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitForLog(t, editor, file)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "opened "+p.clone) })
	waitEditorIdle(t, a)
	if got := editorLog(t, editor); got != p.clone+"\n--test\n"+file+"\n" {
		t.Fatalf("selected editor lost the file: %q", got)
	}
}

func TestYaziBrowsesWorktreesAndGroupedFolders(t *testing.T) {
	t.Parallel()
	y, prepare := fakeYazi(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/browse")
	rows := []worktreeRow{
		{Path: p.path, Dir: dir, Branch: "feat/browse"},
		{Path: "both", Dir: t.TempDir(), Members: []worktreeRow{{Path: p.path, Dir: dir}}},
	}
	for _, row := range rows {
		must(t, os.WriteFile(y.log, nil, 0o600))
		must(t, os.WriteFile(y.gate, nil, 0o600))
		changeOnLoop(a, func() { a.clearSaid(); a.browseWorktree(row) })
		waitForLog(t, y.log, row.Dir)
		sessions := a.sessions.List()
		mode := session.ModeBranch
		if row.grouped() {
			mode = session.ModeGroup
		}
		if len(sessions) != 1 || sessions[0].Mode != mode || sessions[0].Project != row.Path || !sameDirectory(sessions[0].Dir, row.Dir) {
			t.Fatalf("worktree context lost: %+v", sessions)
		}
		must(t, os.Remove(y.gate))
		waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "browsed "+row.Dir) })
		waitEditorIdle(t, a)
	}
}
