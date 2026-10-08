package editors

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/tobola/unagit/internal/editortest"
)

func TestOnlySupportedNeovimCanDetach(t *testing.T) {
	t.Parallel()
	for _, version := range []struct {
		text string
		want bool
	}{{"NVIM v0.11.9", false}, {"NVIM v0.12.0", true}, {"NVIM v1.0.0", true}, {"VIM 9.1", false}} {
		bin := filepath.Join(t.TempDir(), "nvim")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' '"+version.text+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		e := Editor{ID: Nvim, Terminal: true, Found: true, Where: bin, argv: []string{bin}}
		if e.CanDetach() != version.want {
			t.Errorf("%s: detach = %v", version.text, !version.want)
		}
		e.ID = Custom
		if e.CanDetach() {
			t.Error("a custom editor was treated as Neovim")
		}
	}
}

func TestNeovimDetachesAndKeepsItsBuffers(t *testing.T) {
	t.Parallel()
	bin, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("Neovim is not installed")
	}
	e := Editor{ID: Nvim, Name: "Neovim", Terminal: true, Found: true, Where: bin, argv: []string{bin, "--clean", "-n"}}
	if !e.CanDetach() {
		t.Skip("Neovim 0.12 or newer is required")
	}
	dir := editortest.ShortDir(t)
	socket := filepath.Join(dir, "n.sock")
	t.Cleanup(func() { RemoteExpr(bin, socket, "execute('qa!')") })
	start, err := e.BackgroundCommand(dir, socket)
	if err != nil {
		t.Fatal(err)
	}
	terminal, exited, _ := editorPTY(t, start)
	waitEditor(t, func() bool { _, err := RemoteExpr(bin, socket, "1"); return err == nil })
	if _, err := RemoteExpr(bin, socket, `execute('file draft.txt') . execute('call setline(1, "still here")')`); err != nil {
		t.Fatal(err)
	}
	if _, err := terminal.Write([]byte{26}); err != nil {
		t.Fatal(err)
	}
	waitEditorExit(t, exited)
	terminal.Close()
	if !SocketAlive(socket) {
		t.Fatal("detaching closed the server")
	}
	if dirty, err := Modified(bin, socket); err != nil || !dirty {
		t.Fatalf("unsaved buffer lost: modified=%v, %v", dirty, err)
	}
	if closed, err := Close(bin, socket); err != nil || closed {
		t.Fatalf("close discarded an unsaved buffer: closed=%v, %v", closed, err)
	}

	remote, attached, output := editorPTY(t, AttachCommand(bin, socket, dir))
	waitEditor(t, func() bool { value, _ := RemoteExpr(bin, socket, "len(nvim_list_uis())"); return value == "1" })
	value, err := RemoteExpr(bin, socket, "expand('%') . ':' . getline(1)")
	if err != nil || value != "draft.txt:still here" {
		t.Fatalf("buffer after attaching = %q, %v", value, err)
	}
	file := filepath.Join(dir, "a file's | name.txt")
	if err := os.WriteFile(file, []byte("chosen\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := OpenFile(bin, socket, file); err != nil {
		t.Fatal(err)
	}
	value, err = RemoteExpr(bin, socket, "expand('%:p') . ':' . getline(1)")
	if err != nil || value != file+":chosen" {
		t.Fatalf("chosen buffer = %q, %v", value, err)
	}
	if dirty, err := Modified(bin, socket); err != nil || !dirty {
		t.Fatalf("chosen file discarded previous edits: %v, %v", dirty, err)
	}
	if _, err := RemoteExpr(bin, socket, "execute('tabclose')"); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go ConfirmCloseOnAttach(bin, socket, "0", finished)
	waitEditor(t, func() bool { return strings.Contains(output.String(), "Save changes") })
	if _, err := remote.Write([]byte("c")); err != nil {
		t.Fatal(err)
	}
	waitEditor(t, func() bool { dirty, err := Modified(bin, socket); return err == nil && dirty })
	close(finished)
	if _, err := RemoteExpr(bin, socket, "execute('startinsert')"); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Write([]byte{26}); err != nil {
		t.Fatal(err)
	}
	waitEditorExit(t, attached)
	if !SocketAlive(socket) {
		t.Fatal("detaching from insert mode closed the server")
	}
	if _, err := RemoteExpr(bin, socket, "execute('set nomodified')"); err != nil {
		t.Fatal(err)
	}
	if closed, err := Close(bin, socket); err != nil || !closed {
		t.Fatalf("clean server did not close: %v, %v", closed, err)
	}
	waitEditor(t, func() bool { _, err := os.Stat(socket); return os.IsNotExist(err) })
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

// TestATerminalJobIsNotAnUnsavedChange: a terminal's buffer is modified
// while its job runs, but closing an editor with one is not a question of
// saving: it closes without being attached to.
func TestATerminalJobIsNotAnUnsavedChange(t *testing.T) {
	t.Parallel()
	bin, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("Neovim is not installed")
	}
	socket := filepath.Join(editortest.ShortDir(t), "t.sock")
	cmd := exec.Command(bin, "--headless", "--clean", "--listen", socket, "-c", "terminal sleep 100")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	waitEditor(t, func() bool { _, err := RemoteExpr(bin, socket, "1"); return err == nil })
	if dirty, err := Modified(bin, socket); err != nil || dirty {
		t.Fatalf("a running terminal counted as unsaved: %v, %v", dirty, err)
	}
	if closed, err := Close(bin, socket); !closed {
		t.Fatalf("not closed: %v", err)
	}
}
