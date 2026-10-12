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
