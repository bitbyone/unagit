package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobola/unagit/internal/config"
)

func TestProjectDirectoryOverride(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	dir := filepath.Join(t.TempDir(), "custom")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Blank restores")
	form := currentForm(a)
	if form == nil {
		t.Fatal("directory dialog did not open")
	}
	assertLegible(t, a, sc, "repository directory")
	setField(t, a, form, 0, "relative/path")
	pressButton(t, a, sc, form, "Save")
	waitFor(t, a, sc, "enter an absolute repository directory")
	setField(t, a, form, 0, dir)
	pressButton(t, a, sc, form, "Save")
	waitFor(t, a, sc, "Clone directory:")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	inst := &cfg.Instances[0]
	if got := cfg.ProjectDir(inst, "acme/gateway"); got != dir {
		t.Fatalf("destination = %q", got)
	}
	if got := onLoop(a, func() string { return a.newManager(inst.ID, "acme/gateway", nil).MRDir("acme/gateway", 7, "feat/rate") }); got != filepath.Join(filepath.Dir(dir), ".unagit", "custom", "7-feat-rate") {
		t.Fatalf("worktree = %q", got)
	}
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Blank restores")
	pressButton(t, a, sc, currentForm(a), "Inherit")
	waitFor(t, a, sc, "Clone directory:")
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Instances[0].ProjectDirs) != 0 {
		t.Fatal("override was not cleared")
	}
	cloneOnDisk(t, a, inst.ID, "acme/gateway")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "repository is already cloned")
}

func TestWorktreeDiskModes(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	id := onLoop(a, func() string { return a.cfg.Instances[0].ID })
	dir := onLoop(a, func() string { return a.projectDir(id, "acme/gateway") })
	for _, path := range []string{
		filepath.Join(filepath.Dir(dir), ".unagit", "gateway", "7-feat-rate"),
		filepath.Join(filepath.Dir(dir), ".unagit", "gateway", "review-8-chore-drop"),
		filepath.Join(dir+".reviews", "7-feat-rate"),
	} {
		if err := os.MkdirAll(filepath.Join(path, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	onLoop(a, func() bool {
		a.refreshDisk()
		info := a.diskOf(id, "acme/gateway")
		if got := info.MRs[7]; !got.Branch || !got.Review {
			t.Errorf("mixed old and new worktrees: %+v", got)
		}
		if got := info.MRs[8]; got.Branch || !got.Review {
			t.Errorf("review was mistaken for branch: %+v", got)
		}
		return true
	})
}
