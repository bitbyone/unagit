package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/editortest"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

func fakeMux(t *testing.T) (muxtest.Tool, func(*App)) {
	t.Helper()
	tool := muxtest.New(t)
	return tool, func(a *App) { a.findMux = tool.Client }
}

func terminalFavourite(a *App) {
	changeOnLoop(a, func() {
		a.cfg.Editor = "/usr/bin/true"
		a.cfg.EditorArgs = []string{"--test"}
		a.cfg.EditorWindow = false
		a.cfg.FavouriteEditor = editors.Custom
	})
}

// pickMuxAction does an action of the row by typing its name: one kept for
// the filter is listed only then.
func pickMuxAction(t *testing.T, a *App, sc tcell.SimulationScreen, name string) {
	t.Helper()
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · ")
	typeRunes(sc, name)
	waitFor(t, a, sc, name)
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
}

func waitMuxOpened(t *testing.T, a *App, label string) {
	t.Helper()
	waitEditorState(t, a, func() bool {
		return strings.Contains(a.transient, "opened in Zellij: "+label) || strings.Contains(a.dialogSaid.text, "opened in Zellij: "+label)
	})
	waitEditorIdle(t, a)
}

func TestZellijOpensACloneAndSplitsWithoutSuspendingUnagit(t *testing.T) {
	// The fake Neovim changes PATH; Zellij itself is local to this app.
	editortest.Install(t)
	tool, prepare := fakeMux(t)
	visits, _, zprepare := fakeZoxide(t)
	a, sc, _ := newTestAppSrv(t, prepare, zprepare)
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	must(t, os.RemoveAll(p.clone))
	changeOnLoop(a, func() {
		for i := range a.projects {
			if a.projects[i].PathWithNamespace == p.path {
				a.projects[i].HTTPURLToRepo = p.origin
			}
		}
		a.reindexProjects()
	})
	for i, name := range []string{"Open in Zellij Tab", "Open in Zellij Vertical Split", "Open in Zellij Horizontal Split"} {
		if i > 0 {
			// The Neovim of the last pane is closed first: one still running
			// in the directory would be gone to instead of opening another.
			tool.SetPanes(t, nil)
		}
		changeOnLoop(a, a.clearSaid)
		pickMuxAction(t, a, sc, name)
		waitMuxOpened(t, a, p.path)
		if suspended := onLoop(a, func() bool { return a.screen.(*quietScreen).suspended.Load() }); suspended {
			t.Fatal("multiplexer suspended unagit")
		}
		if !workspace.Exists(p.clone) {
			t.Fatal("missing clone was not prepared")
		}
		rows := a.sessions.List()
		if len(rows) != 1 || rows[0].Mux != mux.Zellij || rows[0].MuxSession != "test-session" || rows[0].Mode != session.ModeRepository || rows[0].Editor != editors.Nvim || rows[0].Pane == "" {
			t.Fatalf("multiplexer session: %+v", rows)
		}
	}
	if calls := visitCalls(t, visits, "add", p.clone); calls != 3 {
		t.Fatalf("opened panes recorded %d visits", calls)
	}
	var starts []muxtest.Call
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-pane" || call.Args[3] == "new-tab" {
			starts = append(starts, call)
		}
	}
	if len(starts) != 3 {
		t.Fatalf("opened %d panes", len(starts))
	}
	if !strings.Contains(starts[0].Args[9], `args "."`) || starts[0].Args[7] != "gateway" {
		t.Fatalf("tab command: %v", starts[0].Args)
	}
	for i, direction := range []string{"right", "down"} {
		if starts[i+1].Args[5] != direction || starts[i+1].Args[7] != p.clone {
			t.Fatalf("split: %v", starts[i+1].Args)
		}
	}
	waitEditorState(t, a, func() bool { return a.editorMark(p.clone) != "" })
	waitFor(t, a, sc, glyphEditor)
	assertEditorColour(t, a, sc)
	assertLegible(t, a, sc, "Neovim in a Zellij pane")
	tool.SetPanes(t, nil)
	waitEditorState(t, a, func() bool { return a.editorMark(p.clone) == "" })
}

func TestZellijMakesTheMergeRequestBranchInsteadOfAReview(t *testing.T) {
	t.Parallel()
	tool, prepare := fakeMux(t)
	a, sc, srv := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	terminalFavourite(a)
	p := newRealProject(t, a, "acme/gateway")
	mrOnOrigin(t, srv, p, "Add a token bucket")
	var mr forge.MergeRequest
	changeOnLoop(a, func() {
		for _, item := range a.mrs {
			if item.IID == 7 {
				mr = item
				break
			}
		}
	})
	typeRunes(sc, "2g")
	waitFor(t, a, sc, "Rate limiting")
	pickMuxAction(t, a, sc, "Open in Zellij Tab")
	waitMuxOpened(t, a, p.path+" !7")
	branch := onLoop(a, func() string { return a.mrDir(mr.Instance, p.path, mr.IID, mr.SourceBranch) })
	review := onLoop(a, func() string { return a.reviewDir(mr.Instance, p.path, mr.IID, mr.SourceBranch) })
	if !workspace.Exists(branch) || workspace.Exists(review) {
		t.Fatal("multiplexer opening did not follow Ctrl-O's branch preparation")
	}
	rows := a.sessions.List()
	if len(rows) != 1 || rows[0].IID != 7 || rows[0].Mode != session.ModeBranch || !sameDirectory(rows[0].Dir, branch) {
		t.Fatalf("MR context: %+v", rows)
	}
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-tab" && call.Args[7] != "gateway !7" {
			t.Fatalf("tab name: %v", call.Args)
		}
	}
}

func TestZellijUsesTheLitBlockOfAGroupedWorktree(t *testing.T) {
	t.Parallel()
	tool, prepare := fakeMux(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	terminalFavourite(a)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/browse")
	waitFor(t, a, sc, "feat-browse")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	waitFor(t, a, sc, "feat-browse")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "feat-browse · 2 repositories")
	waitFor(t, a, sc, "clean")
	pickMuxAction(t, a, sc, "Open in Zellij Tab")
	waitMuxOpened(t, a, "feat-browse")
	rows := a.sessions.List()
	if len(rows) != 1 || rows[0].Mode != session.ModeGroup {
		t.Fatalf("group context: %+v", rows)
	}
	group := rows[0].Dir
	typeRunes(sc, "j")
	waitEditorState(t, a, func() bool { return a.wtView != nil && a.wtView.at == 1 })
	lit := onLoop(a, func() worktreeRow { return a.wtView.lit() })
	changeOnLoop(a, a.clearSaid)
	pickMuxAction(t, a, sc, "Open in Zellij Vertical Split")
	waitMuxOpened(t, a, lit.Path)
	rows = a.sessions.List()
	if len(rows) != 2 || rows[0].Mode != session.ModeBranch || !sameDirectory(rows[0].Dir, lit.Dir) || rows[0].Dir == group {
		t.Fatalf("lit block context: %+v", rows)
	}
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-tab" && call.Args[7] != "feat-browse" {
			t.Fatalf("group tab name: %v", call.Args)
		}
	}
}

func TestZellijAsksForATerminalEditorAndItsPickerFits(t *testing.T) {
	tool, prepare := fakeMux(t)
	fakeEditors(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	useFavourite(a, editors.Zed)
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		resizeApp(a, sc, size.w, size.h)
		pickMuxAction(t, a, sc, "Open in Zellij Tab")
		waitFor(t, a, sc, "Open with · terminal editors")
		text := a.screenText(sc)
		if !strings.Contains(text, "Neovim") || strings.Contains(text, "Zed") || strings.Contains(text, "VS Code") {
			t.Fatalf("window editors offered:\n%s", text)
		}
		t.Logf("Terminal editor picker at %dx%d:\n%s", size.w, size.h, text)
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
		assertLegible(t, a, sc, "terminal editor chooser")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "Open with · terminal editors")
	}
	pickMuxAction(t, a, sc, "Open in Zellij Tab")
	waitFor(t, a, sc, "Open with · terminal editors")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitMuxOpened(t, a, p.path)
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-tab" && !strings.Contains(call.Args[9], "nvim") {
			t.Fatal("chosen terminal editor was lost")
		}
	}
}

func TestZellijActionsStayHiddenOutsideZellij(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Open With…")
	// Kept for the filter, they would show once typed for.
	typeRunes(sc, "split")
	waitFor(t, a, sc, "split")
	if text := a.screenText(sc); strings.Contains(text, "Zellij") {
		t.Fatalf("Zellij offered outside Zellij:\n%s", text)
	}
}

func TestZellijFailureDoesNotRecordAnOpening(t *testing.T) {
	t.Parallel()
	tool, prepare := fakeMux(t)
	visits, _, zprepare := fakeZoxide(t)
	a, sc, _ := newTestAppSrv(t, prepare, zprepare)
	waitFor(t, a, sc, "acme/gateway")
	terminalFavourite(a)
	p := newRealProject(t, a, "acme/gateway")
	must(t, os.WriteFile(tool.Failure, []byte("cannot create pane"), 0600))
	pickMuxAction(t, a, sc, "Open in Zellij Horizontal Split")
	waitFor(t, a, sc, "zellij request failed")
	waitEditorIdle(t, a)
	if rows := a.sessions.List(); len(rows) != 0 {
		t.Fatalf("failed opening recorded: %+v", rows)
	}
	if count := visitCalls(t, visits, "add", p.clone); count != 0 {
		t.Fatal("failed opening counted as a visit")
	}
	if running := onLoop(a, func() bool {
		for _, job := range a.jobs {
			if job.title == "Opening editor in Zellij" {
				return true
			}
		}
		return false
	}); running {
		t.Fatal("failed opening left its job running")
	}
}

func TestZellijPaneIsMarkedByTheNextUnagit(t *testing.T) {
	tool := muxtest.New(t)
	fakeEditors(t)
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	app := newApp(cfg, testVault(t, cfg))
	app.findMux = tool.Client
	a, sc, stopped := startAppWithStop(t, app)
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	pickMuxAction(t, a, sc, "Open in Zellij Tab")
	waitMuxOpened(t, a, p.path)
	a.tv.Stop()
	select {
	case <-stopped:
	case <-time.After(patience):
		t.Fatal("first unagit did not stop")
	}
	b, screen := startApp(t, newApp(cfg, testVault(t, cfg)))
	waitFor(t, b, screen, "acme/gateway")
	waitEditorState(t, b, func() bool { return b.editorMark(p.clone) != "" })
	waitFor(t, b, screen, glyphEditor)
	assertEditorColour(t, b, screen)
	tool.SetPanes(t, nil)
	waitEditorState(t, b, func() bool { return b.editorMark(p.clone) == "" })
	if rows := b.sessions.List(); len(rows) != 0 {
		t.Fatal("closed pane session survived its marker")
	}
}

func TestZellijFocusFailureKeepsTheEditorSession(t *testing.T) {
	t.Parallel()
	tool, prepare := fakeMux(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	terminalFavourite(a)
	p := newRealProject(t, a, "acme/gateway")
	must(t, os.WriteFile(tool.Failure, []byte("focus"), 0600))
	pickMuxAction(t, a, sc, "Open in Zellij Vertical Split")
	waitFor(t, a, sc, "could not focus its pane")
	waitEditorIdle(t, a)
	if rows := a.sessions.List(); len(rows) != 1 || !sameDirectory(rows[0].Dir, p.clone) {
		t.Fatalf("unfocused editor was lost: %+v", rows)
	}
}

func TestOpeningADirectoryWithANeovimPaneGoesToThatPane(t *testing.T) {
	// The fake Neovim changes PATH; Zellij itself is local to this app.
	_, log := editortest.Install(t)
	tool, prepare := fakeMux(t)
	a, sc, _ := newTestAppSrv(t, prepare, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	pickMuxAction(t, a, sc, "Open in Zellij Tab")
	waitMuxOpened(t, a, p.path)
	rows := a.sessions.List()
	if len(rows) != 1 || rows[0].Socket == "" || rows[0].Pane == "" {
		t.Fatalf("Neovim in a pane has no server to reach: %+v", rows)
	}
	pane := rows[0].Pane
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-tab" && !strings.Contains(strings.Join(call.Args, " "), "--listen") {
			t.Fatalf("Neovim in a pane does not listen: %v", call.Args)
		}
	}

	// Ctrl-O and the splits find the pane rather than a second Neovim.
	changeOnLoop(a, a.clearSaid)
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "went to the Neovim of "+p.path) })
	changeOnLoop(a, a.clearSaid)
	pickMuxAction(t, a, sc, "Open in Zellij Vertical Split")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "went to the Neovim of "+p.path) })
	waitEditorIdle(t, a)
	focused, started := 0, 0
	for _, call := range tool.Calls(t) {
		switch call.Args[3] {
		case "focus-pane-id":
			if call.Args[4] == pane {
				focused++
			}
		case "new-tab", "new-pane":
			started++
		}
	}
	// The first focus is the new tab's own.
	if focused != 3 || started != 1 {
		t.Fatalf("focused the pane %d times, opened %d panes", focused, started)
	}
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "start|") {
		t.Fatalf("a second Neovim started in the same directory:\n%s", b)
	}
}

// paneNeovim opens the clone in a new Zellij tab and starts the fake
// Neovim's server on the socket that pane was given, as the pane would.
func paneNeovim(t *testing.T, a *App, sc tcell.SimulationScreen, path string) session.Record {
	t.Helper()
	pickMuxAction(t, a, sc, "Open in Zellij Tab")
	waitMuxOpened(t, a, path)
	rows := a.sessions.Running()
	if len(rows) != 1 || rows[0].Socket == "" || rows[0].Pane == "" {
		t.Fatalf("Neovim in a pane: %+v", rows)
	}
	must(t, exec.Command("nvim", "--listen", rows[0].Socket).Run())
	return rows[0]
}

func countIn(log, what string) int {
	b, _ := os.ReadFile(log)
	return strings.Count(string(b), what)
}

func TestNeovimInAPaneCanBePutAsideAndBroughtBack(t *testing.T) {
	// The fake Neovim changes PATH; Zellij itself is local to this app.
	_, log := editortest.Install(t)
	tool, prepare := fakeMux(t)
	a, sc, _ := newTestAppSrv(t, prepare, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	r := paneNeovim(t, a, sc, p.path)
	var started string
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-tab" {
			started = strings.Join(call.Args, " ")
		}
	}
	if !strings.Contains(started, "--listen") || !strings.Contains(started, "<cmd>detach<cr>") {
		t.Fatalf("Neovim in a pane cannot be put aside: %s", started)
	}

	// Ctrl-Z in the pane: it closes, the server runs on, and E has it as
	// an editor aside.
	tool.SetPanes(t, nil)
	waitEditorState(t, a, func() bool {
		rows := a.sessions.Running()
		return len(rows) == 1 && rows[0].Pane == "" && rows[0].Socket == r.Socket
	})

	// A split on its directory brings it back in a pane of its own.
	changeOnLoop(a, a.clearSaid)
	pickMuxAction(t, a, sc, "Open in Zellij Vertical Split")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "attached in Zellij: "+p.path) })
	waitEditorIdle(t, a)
	calls := tool.Calls(t)
	var split []string
	for _, call := range calls {
		if call.Args[3] == "new-pane" {
			split = call.Args
		}
	}
	if joined := strings.Join(split, " "); !strings.Contains(joined, "--remote-ui") || !strings.Contains(joined, r.Socket) {
		t.Fatalf("the split did not attach to the Neovim aside: %v", split)
	}
	rows := a.sessions.Running()
	if len(rows) != 1 || rows[0].Pane == "" || rows[0].Socket != r.Socket {
		t.Fatalf("the Neovim back in a pane: %+v", rows)
	}
	if n := countIn(log, "checktime|"+r.Socket); n == 0 {
		t.Fatal("the Neovim aside did not read changed files before it was shown")
	}

	// Put aside again, Ctrl-O attaches it in unagit's own terminal.
	tool.SetPanes(t, nil)
	waitEditorState(t, a, func() bool {
		rows := a.sessions.Running()
		return len(rows) == 1 && rows[0].Pane == ""
	})
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitForEditorLog(t, log, "attach|"+r.Socket)
	waitEditorIdle(t, a)
	if n := countIn(log, "start|"); n != 1 {
		t.Fatalf("Neovim started %d times for one directory", n)
	}
}

// TestRunningEditorsAskWhereToBringNeovimBack: a in E asks where - this
// terminal or a tab or split of the multiplexer - and the next time starts
// on the place chosen most, so a Enter goes there again; Enter alone
// attaches in this terminal.
func TestRunningEditorsAskWhereToBringNeovimBack(t *testing.T) {
	_, log := editortest.Install(t)
	tool, prepare := fakeMux(t)
	a, sc, _ := newTestAppSrv(t, prepare, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	r := paneNeovim(t, a, sc, p.path)
	splits := func() (n int, last string) {
		for _, call := range tool.Calls(t) {
			if call.Args[3] == "new-pane" {
				n, last = n+1, strings.Join(call.Args, " ")
			}
		}
		return n, last
	}
	putAside := func() {
		t.Helper()
		tool.SetPanes(t, nil)
		waitEditorState(t, a, func() bool {
			rows := a.sessions.Running()
			return len(rows) == 1 && rows[0].Pane == ""
		})
		changeOnLoop(a, a.clearSaid)
		typeRunes(sc, "E")
		waitFor(t, a, sc, "Enter attach")
		typeRunes(sc, "a")
		waitFor(t, a, sc, "Attach acme/gateway · where")
		for _, want := range []string{"Zellij Tab", "Zellij Split Right", "Zellij Split Below"} {
			waitFor(t, a, sc, want)
		}
		waitFor(t, a, sc, "NORMAL")
		if strings.Contains(a.screenText(sc), "This Terminal") {
			t.Fatal("this terminal is offered; Enter is for that")
		}
	}

	putAside()
	assertLegible(t, a, sc, "where to bring Neovim back")
	typeRunes(sc, "j") // Zellij Split Right
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "attached in Zellij: "+p.path) })
	waitEditorIdle(t, a)
	if n, last := splits(); n != 1 || !strings.Contains(last, "--remote-ui") || !strings.Contains(last, r.Socket) {
		t.Fatalf("the split did not attach to the Neovim aside: %d splits, last %s", n, last)
	}

	// The split is now the usual place, listed first: a Enter goes there.
	putAside()
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "attached in Zellij: "+p.path) })
	waitEditorIdle(t, a)
	if n, _ := splits(); n != 2 {
		t.Fatalf("a Enter did not go to the split again: %d splits", n)
	}
	if got := onLoop(a, func() int { return a.cfg.State.PlaceUses[placeOfAttach]["zellij-right"] }); got != 2 {
		t.Fatalf("the split counted %d times", got)
	}
	if n := countIn(log, "attach|"); n != 0 {
		t.Fatal("attached in unagit's own terminal as well")
	}

	// Enter asks nothing: this terminal.
	tool.SetPanes(t, nil)
	waitEditorState(t, a, func() bool {
		rows := a.sessions.Running()
		return len(rows) == 1 && rows[0].Pane == ""
	})
	typeRunes(sc, "E")
	waitFor(t, a, sc, "Enter attach")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitForEditorLog(t, log, "attach|"+r.Socket)
	waitEditorIdle(t, a)
}

func TestANeovimPaneElsewhereCanBeAttachedOrTakenOver(t *testing.T) {
	// The fake Neovim changes PATH; Zellij itself is local to this app.
	_, log := editortest.Install(t)
	_, prepare := fakeMux(t)
	a, sc, _ := newTestAppSrv(t, prepare, func(a *App) { shortSessions(t, a) })
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	r := paneNeovim(t, a, sc, p.path)
	windows := r.Socket + ".uis"
	must(t, os.WriteFile(windows, []byte("1"), 0600))
	// This unagit is now in another Zellij session than the pane.
	changeOnLoop(a, func() {
		other := *a.multiplexer
		other.Session = "elsewhere"
		a.multiplexer = &other
	})

	title := "Neovim in Zellij session test-session"
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
		waitFor(t, a, sc, title)
		waitFor(t, a, sc, "Take Over")
		text := a.screenText(sc)
		if !strings.Contains(text, "Attach Here Too") {
			t.Fatalf("no way to attach at %dx%d:\n%s", size.w, size.h, text)
		}
		t.Logf("pane elsewhere at %dx%d:\n%s", size.w, size.h, text)
		assertLegible(t, a, sc, "Neovim in a pane elsewhere")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, title)
		waitEditorIdle(t, a)
	}
	if n := countIn(log, "attach|"); n != 0 {
		t.Fatal("Esc attached anyway")
	}

	// Attached here too: a second window, the pane keeps its own.
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Attach Here Too")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitForEditorLog(t, log, "attach|"+r.Socket)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "stays open in its other window") })
	waitEditorIdle(t, a)
	if n := countIn(log, "detach|"); n != 0 {
		t.Fatal("attaching too put the pane's window aside")
	}

	// Taken over: the pane's window is put aside first.
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Take Over")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitForEditorLog(t, log, "detach|"+r.Socket)
	waitEditorState(t, a, func() bool { return countIn(log, "attach|"+r.Socket) == 2 })
	waitEditorIdle(t, a)

	// With two windows Neovim would put aside whichever was used last, so
	// it is not offered.
	must(t, os.WriteFile(windows, []byte("2"), 0600))
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Attach Here Too")
	if strings.Contains(a.screenText(sc), "Take Over") {
		t.Fatal("taking over was offered with two windows")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, title)
}
