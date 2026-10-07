package zoxide

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestScoresKeepPathsWithSpaces(t *testing.T) {
	t.Parallel()
	got, err := parseScores("  12.5 /work/review with spaces\n\t0.25\t/work/a [b] \n\n0 /work/zero\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"/work/review with spaces": 12.5, "/work/a [b] ": .25, "/work/zero": 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scores = %#v, want %#v", got, want)
	}
	for _, line := range []string{"broken", "no /a", "NaN /a", "Inf /a", "-1 /a", "4 relative", "3 "} {
		if _, err := parseScores(line); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}

func TestMissingBinaryIsQuiet(t *testing.T) {
	t.Parallel()
	c := Find(func(string) (string, error) { return "", exec.ErrNotFound })
	if c.Enabled(nil) {
		t.Fatal("missing binary enabled")
	}
	if _, err := c.Scores(); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("scores: %v", err)
	}
	if err := c.Add("/a"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("add: %v", err)
	}
}

func TestCommandsUseArgumentsAndRespectEnvironment(t *testing.T) {
	// PATH and zoxide's exclusion rule belong to the process.
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "zoxide")
	script := "#!/bin/sh\nprintf '%s\\n' \"$_ZO_EXCLUDE_DIRS\" \"$@\" >> \"$ZO_TEST_LOG\"\nif [ \"$1\" = query ]; then printf '9.5 /a path\\n'; fi\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZO_TEST_LOG", log)
	t.Setenv("_ZO_EXCLUDE_DIRS", "/excluded/*")
	c := New()
	off := false
	if !c.Enabled(nil) || c.Enabled(&off) {
		t.Fatal("automatic or explicit setting lost")
	}
	if err := c.Add("/a path"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("/a path"); err != nil {
		t.Fatal(err)
	}
	scores, err := c.Scores()
	if err != nil || scores["/a path"] != 9.5 {
		t.Fatalf("scores = %v, %v", scores, err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "/excluded/*\nadd\n--\n/a path\n/excluded/*\nremove\n--\n/a path\n/excluded/*\nquery\n--list\n--score\n"
	if string(data) != want {
		t.Fatalf("calls = %q, want %q", data, want)
	}
	// The client keeps the executable even if PATH later changes.
	t.Setenv("PATH", "")
	if err := c.Add("/still found"); err != nil {
		t.Fatal(err)
	}
}

func TestSlowToolHasABoundedWait(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "zoxide")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec /bin/sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := Find(func(string) (string, error) { return bin, nil })
	start := time.Now()
	if _, err := c.Scores(); err == nil {
		t.Fatal("slow query succeeded")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("query blocked for %s", elapsed)
	}
}

func TestResolvedPathSurvivesRemoval(t *testing.T) {
	t.Setenv("_ZO_RESOLVE_SYMLINKS", "1")
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(link, "deleted worktree")
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got := Path(dir); got != filepath.Join(want, "deleted worktree") {
		t.Fatalf("path = %q", got)
	}
	t.Setenv("_ZO_RESOLVE_SYMLINKS", "0")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := Path("a directory"); got != filepath.Join(cwd, "a directory") {
		t.Fatalf("relative path = %q", got)
	}
	if got := Path(dir); !strings.HasPrefix(got, link) {
		t.Fatalf("resolved with the setting off: %q", got)
	}
}
