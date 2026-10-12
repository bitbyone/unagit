//go:build integration

package editors

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/editortest"
	"github.com/tobola/unagit/internal/testenv"
)

func TestNeovimDetachesAndKeepsItsBuffers(t *testing.T) {
	t.Parallel()
	bin := testenv.Require(t, "nvim")
	e := Editor{ID: Nvim, Name: "Neovim", Terminal: true, Found: true, Where: bin, argv: []string{bin, "--clean", "-n"}}
	if !e.CanDetach() {
		t.Fatal("the image's Neovim cannot detach; it needs 0.12 or newer")
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

// TestATerminalJobIsNotAnUnsavedChange: a terminal's buffer is modified
// while its job runs, but closing an editor with one is not a question of
// saving: it closes without being attached to.
func TestATerminalJobIsNotAnUnsavedChange(t *testing.T) {
	t.Parallel()
	bin := testenv.Require(t, "nvim")
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
