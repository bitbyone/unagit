package ui

import (
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/editortest"
	"github.com/tobola/unagit/internal/session"
)

func shortSessions(t *testing.T, a *App) {
	t.Helper()
	short := filepath.Join(editortest.ShortDir(t), "cfg")
	must(t, os.Symlink(a.cfg.Dir(), short))
	a.cfg.SetDir(short)
	a.sessions = session.New(a.cfg.Dir())
}

func editorLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNeovimCanBePutAsideAndFoundByTheNextUnagit(t *testing.T) {
	_, log := editortest.Install(t)
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	app := newApp(cfg, testVault(t, cfg))
	shortSessions(t, app)
	a, sc, stopped := startAppWithStop(t, app)
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	newRealProject(t, a, "acme/gateway")
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "nvim aside: acme/gateway")
	rows := a.sessions.Running()
	if len(rows) != 1 {
		t.Fatalf("editor not retained: %+v", rows)
	}
	socket := rows[0].Socket
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	// The attach is acknowledged by its own redraw, rather than by the word
	// still visible from the first opening.
	waitForEditorLog(t, log, "attach|"+socket)
	// Files changed while it was aside are read again before it is shown.
	waitForEditorLog(t, log, "checktime|"+socket)
	waitEditorIdle(t, a)
	if got := editorLog(t, log); strings.Count(got, "start|") != 1 {
		t.Fatalf("same directory started twice:\n%s", got)
	}
	a.tv.Stop()
	select {
	case <-stopped:
	case <-time.After(patience):
		t.Fatal("previous unagit did not stop")
	}
	b, screen := startApp(t, newApp(a.cfg, testVault(t, a.cfg)))
	waitFor(t, b, screen, "acme/gateway")
	typeRunes(screen, "E")
	waitFor(t, b, screen, "Running Editors")
	waitFor(t, b, screen, "main")
	assertLegible(t, b, screen, "running editor from the previous unagit")
	typeRunes(screen, "x")
	waitFor(t, b, screen, "closed nvim: acme/gateway")
	if rows := b.sessions.Running(); len(rows) != 0 {
		t.Fatalf("closed server still listed: %+v", rows)
	}
}

func waitForEditorLog(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("editor did not log %q", want)
}

func TestQuittingNeovimRemovesItsBackgroundRecord(t *testing.T) {
	editortest.Install(t)
	t.Setenv("UNAGIT_FAKE_NVIM_CLOSE", "1")
	a, sc, _ := newTestAppSrv(t, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	newRealProject(t, a, "acme/gateway")
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "opened ")
	if rows := a.sessions.List(); len(rows) != 0 {
		t.Fatalf("closed editor still listed: %+v", rows)
	}
}

func TestClosingAnEditorWithChangesAttachesInstead(t *testing.T) {
	_, log := editortest.Install(t)
	a, sc, _ := newTestAppSrv(t, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	newRealProject(t, a, "acme/gateway")
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "nvim aside:")
	rows := a.sessions.Running()
	if len(rows) != 1 {
		t.Fatalf("no background editor: %+v", rows)
	}
	must(t, os.WriteFile(rows[0].Socket+".dirty", []byte("unsaved"), 0600))
	typeRunes(sc, "E")
	waitFor(t, a, sc, "EDITS")
	typeRunes(sc, "x")
	waitForEditorLog(t, log, "attach|"+rows[0].Socket)
	waitEditorIdle(t, a)
	waitFor(t, a, sc, "nvim aside:")
	if !editors.SocketAlive(rows[0].Socket) {
		t.Fatal("unsaved changes were discarded")
	}
}

func TestRunningEditorsPickerFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}, {60, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resizeApp(a, sc, size.w, size.h)
			records := []session.Record{
				{Project: "acme/a-long-repository-name", Branch: "feature/a-long-branch-name", Dir: "/workspace/acme/a-long-repository-name", Since: time.Now().Add(-time.Hour)},
				{Project: "acme/billing", IID: 9, Mode: session.ModeReview, Dir: "/workspace/acme/.unagit/billing/review-9-fix-round", Since: time.Now().Add(-24 * time.Hour)},
			}
			a.tv.QueueUpdateDraw(func() { a.drawRunningEditors(records, []string{"modified", ""}) })
			waitFor(t, a, sc, "Running Editors")
			text := a.screenText(sc)
			t.Logf("rendered running editors:\n%s", text)
			for _, want := range []string{"REPOSITORY", "BRANCH / MR", "DIRECTORY", "AGE", "EDITS", "!9", "attach", "close editor"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q missing:\n%s", want, text)
				}
			}
			frame := onLoop(a, func() rect {
				_, prim := a.pages.GetFrontPage()
				x, y, w, h := prim.(*modalBox).content.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Fatalf("border drawn over on row %d:\n%s", y, text)
				}
			}
			assertLegible(t, a, sc, "running editors")
			sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
			waitFor(t, a, sc, "Attach to Editor")
			assertLegible(t, a, sc, "running editor actions")
		})
	}
}

func TestEditorMarksFollowAnotherInstancesRecords(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/rate")
	mrDir := filepath.Join(filepath.Dir(dir), "7-feat-rate")
	gitIn(t, p.clone, "worktree", "move", dir, mrDir)
	dir = mrDir
	p.rescan()
	socket, err := a.sessions.NewSocket()
	must(t, err)
	listener, err := net.Listen("unix", socket)
	must(t, err)
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	other := session.New(a.cfg.Dir())
	close := other.Open(session.Record{Dir: dir, Editor: editors.Nvim, Socket: socket, Project: "acme/gateway", IID: 7})
	t.Cleanup(close)
	// The timer finds this record without a refresh key or a tab switch.
	waitEditorState(t, a, func() bool { return a.editorMark(dir) != "" })
	if got := onLoop(a, func() string { return a.editorMark(p.clone) }); got != "" {
		t.Fatal("worktree editor marked the clone")
	}
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	waitFor(t, a, sc, glyphEditor)
	assertLegible(t, a, sc, "editor mark on a merge request")
	assertEditorColour(t, a, sc)
	typeRunes(sc, " ")
	assertEditorColour(t, a, sc)
	assertLegible(t, a, sc, "editor mark on a marked merge request under the cursor")
	typeRunes(sc, " ")
	wtDir := p.worktree("feat/other")
	p.rescan()
	wtClose := other.Open(session.Record{Dir: wtDir, Editor: editors.Nvim})
	t.Cleanup(wtClose)
	typeRunes(sc, "3")
	waitFor(t, a, sc, "feat/other")
	waitFor(t, a, sc, glyphEditor)
	assertLegible(t, a, sc, "editor mark on a worktree")
	assertEditorColour(t, a, sc)
	close()
	waitEditorState(t, a, func() bool { return a.editorMark(dir) == "" })
}

func waitEditorState(t *testing.T, a *App, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(patience)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if onLoop(a, ready) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("editor state did not arrive")
		case <-ticker.C:
		}
	}
}

func waitEditorIdle(t *testing.T, a *App) {
	t.Helper()
	waitEditorState(t, a, func() bool {
		if !a.editorMu.TryLock() {
			return false
		}
		a.editorMu.Unlock()
		return true
	})
}

func assertEditorColour(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	cells, _, _ := onLoopCells(a, sc)
	wanted := onLoop(a, func() tcell.Color { return role("mark.editor") })
	glyph := onLoop(a, func() rune { return []rune(glyphEditor)[0] })
	for _, cell := range cells {
		if len(cell.Runes) > 0 && cell.Runes[0] == glyph {
			ink, _, _ := cell.Style.Decompose()
			// Its lightness may follow the band for contrast; its hue must
			// still be the theme's green, rather than the selection's ink.
			actual, expected := toOklab(ink), toOklab(wanted)
			hue := math.Abs(math.Atan2(actual.b, actual.a) - math.Atan2(expected.b, expected.a))
			if hue > 0.1 || math.Hypot(actual.a, actual.b) < math.Hypot(expected.a, expected.b)*0.8 {
				t.Fatalf("editor mark lost its colour: %v, want the hue of %v", ink, wanted)
			}
			return
		}
	}
	t.Fatal("editor mark is not on screen")
}

func TestAWaitingWindowLauncherHoldsNoOtherOpen(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	// A launcher that waits for its window, as code --wait does.
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	launcher := filepath.Join(dir, "editor")
	must(t, os.WriteFile(launcher, []byte("#!/bin/sh\necho $$ > "+pidFile+"\nexec sleep 60\n"), 0o755))
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			var pid int
			fmt.Sscan(string(b), &pid)
			if p, err := os.FindProcess(pid); err == nil && pid > 0 {
				p.Kill()
			}
		}
	})
	changeOnLoop(a, func() {
		a.cfg.Editor, a.cfg.EditorArgs, a.cfg.EditorWindow = launcher, nil, true
		a.cfg.FavouriteEditor = editors.Custom
	})
	newRealProject(t, a, "acme/gateway")
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitEditorState(t, a, func() bool {
		_, err := os.Stat(pidFile)
		return err == nil && strings.Contains(a.transient, "opened in")
	})
	// The launcher still waits for its window; opening is free again.
	waitEditorIdle(t, a)
}
