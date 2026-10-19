package editors

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/tobola/unagit/internal/testenv"
)

// TestOnlySupportedNeovimCanDetach reads the version as given, with no
// process: a probe started under the whole suite's load once missed its
// timeout and read as too old.
func TestOnlySupportedNeovimCanDetach(t *testing.T) {
	t.Parallel()
	for _, version := range []struct {
		text string
		want bool
	}{{"NVIM v0.11.9", false}, {"NVIM v0.12.0", true}, {"NVIM v1.0.0", true}, {"VIM 9.1", false}} {
		if got := detachable(version.text + "\nBuild type: Release\n"); got != version.want {
			t.Errorf("%s: detach = %v", version.text, got)
		}
	}
	e := Editor{ID: Custom, Terminal: true, Found: true, Where: "/nonexistent/nvim"}
	if e.CanDetach() {
		t.Error("a custom editor was treated as Neovim")
	}
}

// TestASlowVersionIsAskedAgain: a Neovim that did not say its version in
// time is not taken for one that cannot be put aside for good - the next
// opening asks it again. It swaps the timeout, so it is serial.
func TestASlowVersionIsAskedAgain(t *testing.T) {
	was := versionTimeout
	versionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { versionTimeout = was })
	dir := t.TempDir()
	bin := dir + "/nvim"
	write := func(script string) {
		t.Helper()
		if err := testenv.WriteExecutable(bin+".new", []byte(script)); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(bin+".new", bin); err != nil {
			t.Fatal(err)
		}
	}
	write("#!/bin/sh\nexec sleep 30\n")
	e := Editor{ID: Nvim, Terminal: true, Found: true, Where: bin}
	if e.CanDetach() {
		t.Fatal("a version not said in time was read")
	}
	// The second answer has the usual time, which a loaded machine needs.
	versionTimeout = was
	write("#!/bin/sh\necho 'NVIM v0.12.0'\n")
	if !e.CanDetach() {
		t.Fatal("the slow answer was kept: the version was not asked again")
	}
}

func editorPTY(t *testing.T, cmd *exec.Cmd) (*os.File, <-chan error, *editorOutput) {
	t.Helper()
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
	output := &editorOutput{}
	go io.Copy(output, f)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return f, exited, output
}

func waitEditorExit(t *testing.T, exited <-chan error) {
	t.Helper()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("editor UI did not detach")
	}
}

func waitEditor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("editor did not answer in time")
}

func TestBackgroundCommandMapsEveryEditingMode(t *testing.T) {
	t.Parallel()
	e := Editor{Found: true, argv: []string{"nvim", "."}}
	cmd, err := e.BackgroundCommand("/tmp", "/tmp/short.sock")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"--listen /tmp/short.sock", "nnoremap <C-z>", "inoremap <C-z>", "vnoremap <C-z>"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %q", want, args)
		}
	}
}

type editorOutput struct {
	sync.Mutex
	buffer bytes.Buffer
}

func (out *editorOutput) Write(p []byte) (int, error) {
	out.Lock()
	defer out.Unlock()
	return out.buffer.Write(p)
}

func (out *editorOutput) String() string {
	out.Lock()
	defer out.Unlock()
	return out.buffer.String()
}
