package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/workspace"
)

func openPublication(t *testing.T, a *App, sc tcell.SimulationScreen, dir string) *tview.Form {
	t.Helper()
	typeRunes(sc, ":")
	waitFor(t, a, sc, "FILTER")
	typeRunes(sc, "new repository from folder")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Directory")
	form := currentForm(a)
	onLoop(a, func() bool { form.GetFormItemByLabel("Directory").(*tview.InputField).SetText(dir); return true })
	pressButton(t, a, sc, form, "Continue")
	waitFor(t, a, sc, "All branches")
	return currentForm(a)
}
func TestPublishFolderKeepsLocalFilesAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	source := filepath.Join(t.TempDir(), "tool")
	must(t, os.Mkdir(source, 0755))
	source, _ = filepath.EvalSymlinks(source)
	gitIn(t, source, "init", "-q", "--initial-branch=main")
	commitIn(t, source, "file.txt", "Initial commit")
	must(t, os.WriteFile(filepath.Join(source, "scratch"), []byte("local"), 0600))
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, "", "init", "-q", "--bare", origin)
	srv.newRepoURL.Store(origin)
	form := openPublication(t, a, sc, source)
	pressButton(t, a, sc, form, "Continue")
	waitFor(t, a, sc, "Planned path")
	form = currentForm(a)
	assertLegible(t, a, sc, "publication review")
	pressButton(t, a, sc, form, "Publish")
	waitFor(t, a, sc, "Local repository location")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "kept ")
	id := onLoop(a, func() string { return a.cfg.Instances[0].ID })
	actual := onLoop(a, func() string { return a.projectDir(id, "acme/tool") })
	if actual != source {
		t.Fatalf("path %s != %s", actual, source)
	}
	onLoop(a, func() bool { a.refreshProjects(); return true })
	waitFor(t, a, sc, "3 repositories indexed")
	if actual = onLoop(a, func() string { return a.projectDir(id, "acme/tool") }); actual != source {
		t.Fatalf("refresh forgot the published folder: %s", actual)
	}
	if got := gitIn(t, source, "remote", "get-url", "origin"); strings.TrimSpace(got) != origin {
		t.Fatal(got)
	}
	if strings.Contains(gitIn(t, origin, "ls-tree", "--name-only", "main"), "scratch") {
		t.Fatal("uncommitted file pushed")
	}
	body, _ := srv.postedProject.Load().(string)
	if !strings.Contains(body, `"initialize_with_readme":false`) {
		t.Fatal(body)
	}
	cfgDir := onLoop(a, func() string { return a.cfg.Dir() })
	loaded, err := config.LoadFrom(cfgDir)
	must(t, err)
	// Fixtures save their config explicitly only when a restart needs it.
	if len(loaded.Instances) == 0 {
		onLoop(a, func() bool { must(t, a.cfg.Save()); return true })
		loaded, err = config.LoadFrom(cfgDir)
		must(t, err)
	}
	found := false
	for _, p := range Places(loaded) {
		if p.Project == "acme/tool" && p.Dir == source {
			found = true
		}
	}
	if !found {
		t.Fatal("adopted checkout missing from CLI places after restart")
	}
}
func TestPublicationFormsFitAndReturn(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resizeApp(a, sc, size.w, size.h)
			dir := filepath.Join(t.TempDir(), "tool")
			must(t, os.Mkdir(dir, 0755))
			dir, _ = filepath.EvalSymlinks(dir)
			form := openPublication(t, a, sc, dir)
			assertPublicationForm(t, a, sc, form, []string{"Name", "Where", "Description", "Visibility", "First branch", "All branches", "Tags", "Source", "Continue", "Cancel"})
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitFor(t, a, sc, "i type")
			onLoop(a, func() bool { form.SetFocus(form.GetFormItemIndex(labelRepoWhere)); a.tv.SetFocus(form); return true })
			sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
			waitFocus(t, a, func() bool { return form.GetFormItemByLabel(labelRepoWhere).(*tview.DropDown).IsOpen() })
			assertLegible(t, a, sc, "publication destination select")
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitFocus(t, a, func() bool { return !form.GetFormItemByLabel(labelRepoWhere).(*tview.DropDown).IsOpen() })
			pressButton(t, a, sc, form, "Continue")
			waitFor(t, a, sc, "Planned path")
			form = currentForm(a)
			assertPublicationForm(t, a, sc, form, []string{"Remote", "Publish", "Local files", "Source", "Planned path", "Cancel"})
			t.Log("Publication review:\n" + a.screenText(sc))
			pressButton(t, a, sc, form, "Cancel")
			waitFor(t, a, sc, "All branches")
		})
	}
}
func assertPublicationForm(t *testing.T, a *App, sc tcell.SimulationScreen, form *tview.Form, labels []string) {
	t.Helper()
	frame := onLoop(a, func() rect { x, y, w, h := form.GetRect(); return rect{x, y, w, h} })
	for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
		if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
			t.Fatalf("border at row %d: %q\n%s", y, r, a.screenText(sc))
		}
	}
	text := a.screenText(sc)
	for _, label := range labels {
		if !strings.Contains(text, label) {
			t.Errorf("missing %s\n%s", label, text)
		}
	}
	assertLegible(t, a, sc, "publication form")
}
func TestPublicationRecoversMoveBeforeStateWasSaved(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	gitIn(t, dir, "init", "-q", "--initial-branch=main")
	origin := filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, "", "init", "--bare", origin)
	id := onLoop(a, func() string { return a.cfg.Instances[0].ID })
	pr := forge.Project{Instance: id, PathWithNamespace: "acme/tool", HTTPURLToRepo: origin}
	gitIn(t, dir, "remote", "add", "origin", origin)
	gitIn(t, dir, "config", "unagit.localProject", id+"/acme/tool")
	p := workspace.Publication{Directory: dir, Instance: id, Project: &pr, Complete: true, MoveTo: filepath.Join(t.TempDir(), "moved")}
	cfgDir := onLoop(a, func() string { return a.cfg.Dir() })
	must(t, p.Save(workspace.PublicationPath(cfgDir, dir)))
	must(t, os.Rename(dir, p.MoveTo))
	onLoop(a, func() bool { a.loadLocalPublications(); a.reindexProjects(); return true })
	if got := onLoop(a, func() string { return a.projectDir(id, "acme/tool") }); got != p.MoveTo {
		t.Fatal(got)
	}
}

func TestPublishPlainFolderThroughChangesAndMove(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	source := filepath.Join(t.TempDir(), "tool")
	must(t, os.Mkdir(source, 0755))
	source, _ = filepath.EvalSymlinks(source)
	must(t, os.WriteFile(filepath.Join(source, "chosen"), []byte("publish this"), 0644))
	must(t, os.WriteFile(filepath.Join(source, "leave"), []byte("keep local"), 0600))
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, "", "init", "-q", "--bare", origin)
	srv.newRepoURL.Store(origin)
	form := openPublication(t, a, sc, source)
	pressButton(t, a, sc, form, "Continue")
	waitFor(t, a, sc, "Planned path")
	pressButton(t, a, sc, currentForm(a), "Changes")
	waitFor(t, a, sc, "Unversioned Files")
	waitFocus(t, a, func() bool { return a.changes != nil && a.changes.loaded })
	onLoop(a, func() bool { a.changes.included["chosen"] = true; a.commitChanges(a.changes); return true })
	waitFor(t, a, sc, "Message")
	form = currentForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel("Message").(*tview.TextArea).SetText("First commit", false)
		return true
	})
	pressButton(t, a, sc, form, "Commit")
	waitFor(t, a, sc, "New repository from folder · review")
	pressButton(t, a, sc, currentForm(a), "Publish")
	waitFor(t, a, sc, "Local repository location")
	for _, size := range []struct{ w, h int }{{100, 30}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		assertPublicationForm(t, a, sc, currentForm(a), []string{"Current folder", "Planned folder", "Keep here", "Move"})
	}
	t.Log("Location choice:\n" + a.screenText(sc))
	pressButton(t, a, sc, currentForm(a), "Move")
	waitFor(t, a, sc, "moved to ")
	target := onLoop(a, func() string { return a.projectDir(a.cfg.Instances[0].ID, "acme/tool") })
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("original folder remains")
	}
	for _, name := range []string{"chosen", "leave"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.TrimSpace(gitIn(t, origin, "ls-tree", "--name-only", "main")); got != "chosen" {
		t.Fatal(got)
	}
	// The key help remains a usable list on the changed tab.
	typeRunes(sc, "?")
	waitFor(t, a, sc, "Repositories")
	assertLegible(t, a, sc, "repository help after publication")
	t.Log("Repository help:\n" + a.screenText(sc))
}

func TestPublicationUsesYaziDirectoryWithoutOpeningAFile(t *testing.T) {
	t.Parallel()
	fake, setup := fakeYazi(t)
	a, sc, _ := newTestAppSrv(t, setup)
	waitFor(t, a, sc, "acme/gateway")
	dir := t.TempDir()
	must(t, os.WriteFile(fake.cwd, []byte(dir+"\n"), 0600))
	must(t, os.WriteFile(fake.chosen, []byte(filepath.Join(dir, "file")+"\n"), 0600))
	onLoop(a, func() bool { a.showPublishSource(t.TempDir()); return true })
	waitFor(t, a, sc, "Browse")
	form := currentForm(a)
	pressButton(t, a, sc, form, "Browse")
	waitFocus(t, a, func() bool { return form.GetFormItemByLabel("Directory").(*tview.InputField).GetText() == dir })
	waitFor(t, a, sc, "Directory")
	if got := onLoop(a, func() string { return form.GetFormItemByLabel("Directory").(*tview.InputField).GetText() }); got != dir {
		t.Fatal(got)
	}
}
