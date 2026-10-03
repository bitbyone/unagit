package chezmoi

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestKeyNamesOneRepositoryWhateverTheProtocol(t *testing.T) {
	same := []string{
		"https://github.com/jantobola/dotfiles.git",
		"https://github.com/jantobola/dotfiles",
		"git@github.com:jantobola/dotfiles.git",
		"ssh://git@github.com:22/jantobola/dotfiles.git",
		"https://user@GitHub.com/JanTobola/dotfiles/",
	}
	for _, s := range same {
		if got := Key(s); got != "github.com/jantobola/dotfiles" {
			t.Errorf("Key(%q) = %q", s, got)
		}
	}
	if Same("https://github.com/a/dotfiles", "https://github.com/b/dotfiles") {
		t.Error("two owners are one repository")
	}
	if Same("https://gitlab.com/a/dotfiles", "https://github.com/a/dotfiles") {
		t.Error("two servers are one repository")
	}
	for _, s := range []string{"", "dotfiles", "/home/me/dotfiles", "https://github.com/"} {
		if Key(s) != "" {
			t.Errorf("Key(%q) = %q, want nothing", s, Key(s))
		}
	}
}

// TestFindAsksChezmoiForItsWorkingTree runs a chezmoi that only knows `git`,
// in a real repository, the way chezmoi itself would.
func TestFindAsksChezmoiForItsWorkingTree(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:me/dotfiles.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	bin := filepath.Join(t.TempDir(), "chezmoi")
	script := "#!/bin/sh\n[ \"$1\" = --no-tty ] && [ \"$2\" = git ] && [ \"$3\" = -- ] || exit 2\nshift 3\ncd '" + repo + "' && exec git \"$@\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Find(bin)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(repo)
	if dir, _ := filepath.EvalSymlinks(got.Dir); dir != want || got.Origin != "git@github.com:me/dotfiles.git" {
		t.Errorf("Find = %+v, want %s with its origin", got, want)
	}

	if out, err := exec.Command("git", "-C", repo, "remote", "remove", "origin").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := Find(bin); err == nil {
		t.Error("a checkout without an origin was found")
	}
}
