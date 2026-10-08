package editors

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var detachVersions sync.Map

// CanDetach is cached by launcher, so checking an opening action does not
// start another process every time. Custom commands keep their own meaning.
func (e Editor) CanDetach() bool {
	if e.ID != Nvim || !e.Found || !e.Terminal {
		return false
	}
	probe := sync.OnceValue(func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, e.Where, "--version").Output()
		if err != nil {
			return false
		}
		var major, minor int
		if _, err := fmt.Sscanf(string(out), "NVIM v%d.%d", &major, &minor); err != nil {
			return false
		}
		return major > 0 || minor >= 12
	})
	value, _ := detachVersions.LoadOrStore(e.Where, probe)
	return value.(func() bool)()
}

// BackgroundCommand changes only this invocation, never the user's config.
func (e Editor) BackgroundCommand(dir, socket string) (*exec.Cmd, error) {
	return e.BackgroundCommandAt(dir, socket, "")
}

func (e Editor) BackgroundCommandAt(dir, socket, file string) (*exec.Cmd, error) {
	cmd, err := e.ServerCommandAt(dir, socket, file)
	if err != nil {
		return nil, err
	}
	for _, mode := range []string{"nnoremap", "inoremap", "vnoremap"} {
		cmd.Args = append(cmd.Args, "-c", mode+" <C-z> <cmd>detach<cr>")
	}
	return cmd, nil
}

// ServerCommandAt starts Neovim listening on socket and nothing more: an
// editor in a pane of its own is not put aside, but unagit still has to
// reach it to open a file in it, ask about unsaved changes, or close it.
func (e Editor) ServerCommandAt(dir, socket, file string) (*exec.Cmd, error) {
	cmd, err := e.CommandAt(dir, file)
	if err != nil {
		return nil, err
	}
	cmd.Args = append(cmd.Args, "--listen", socket)
	return cmd, nil
}

// SocketAlive checks the listener without asking the editor to run code.
// A busy editor may not handle RPC yet; that must not cost it its record.
func SocketAlive(socket string) bool {
	if socket == "" {
		return false
	}
	c, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// RemoteExpr bounds reads from an editor that might be busy or waiting on
// a prompt. Queries are few enough that Neovim's own RPC client suffices.
func RemoteExpr(launcher, socket, expr string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, launcher, "--server", socket, "--remote-expr", expr).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("editor is not answering; attach to it and try again")
		}
		return "", fmt.Errorf("editor request failed: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// unsaved counts the buffers with edits not written. A terminal's buffer
// counts as modified while its job runs, but holds nothing to save: closing
// ends the job, and asking about it would attach to Neovim for nothing.
const unsaved = "len(filter(getbufinfo({'bufmodified': 1}), {_, b -> getbufvar(b.bufnr, '&buftype') !=# 'terminal'}))"

func Modified(launcher, socket string) (bool, error) {
	out, err := RemoteExpr(launcher, socket, unsaved)
	if err != nil {
		return false, err
	}
	n, err := strconv.Atoi(out)
	return n > 0, err
}

func AttachCommand(launcher, socket, dir string) *exec.Cmd {
	cmd := exec.Command(launcher, "--server", socket, "--remote-ui")
	cmd.Dir = dir
	return cmd
}

// Close asks through RPC only after the caller has checked for edits.
// Rechecking inside Neovim closes the race with another attached UI. With
// no edits it quits without asking: a question - a terminal's job still
// running - would wait for an answer where nobody can see it, and the
// caller would have to attach to it after all. A terminal's job ends
// with it.
func Close(launcher, socket string) (bool, error) {
	out, err := RemoteExpr(launcher, socket, unsaved+" ? 1 : execute('qa!')")
	if out == "1" {
		return false, nil
	}
	// A Neovim that quits takes a moment to let go of its socket - often
	// after it has answered, or instead of answering. Asking at once would
	// take it for one that refused, and attach to it as it goes.
	if gone(socket, closeWait) {
		return true, nil
	}
	return false, err
}

// closeWait is how long Close waits for an editor that was asked to quit.
const closeWait = 2 * time.Second

// gone waits up to wait for nothing to listen on socket any more.
func gone(socket string, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for SocketAlive(socket) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

// ConfirmCloseOnAttach waits for the new UI, so a save question is drawn
// where its buffers can be seen. Scheduling lets the RPC client return even
// while that question waits for the user.
func ConfirmCloseOnAttach(launcher, socket, before string, finished <-chan struct{}) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-finished:
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			count, err := RemoteExpr(launcher, socket, "len(nvim_list_uis())")
			if err == nil && count != before && count != "0" {
				RemoteExpr(launcher, socket, `luaeval('vim.schedule(function() vim.cmd("confirm qa") end)')`)
				return
			}
		}
	}
}

// Checktime has Neovim read again the files that changed on disk while
// nobody was looking - a review worktree reset for a force push or narrowed
// to one commit, a branch pulled or rebased. Its buffers would otherwise
// show the old files, and the gutter a diff that is no longer there. A
// buffer with unsaved changes is asked about by Neovim itself.
func Checktime(launcher, socket string) error {
	_, err := RemoteExpr(launcher, socket, "execute('checktime')")
	return err
}

// UIs counts the interfaces attached to a Neovim.
func UIs(launcher, socket string) (int, error) {
	out, err := RemoteExpr(launcher, socket, "len(nvim_list_uis())")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// DetachUI makes a Neovim let go of its interface, as Ctrl-Z in it would:
// the server runs on with its buffers and unsaved changes, and the process
// of that interface - a Zellij pane's - ends. Checked by hand with Neovim
// 0.12.5: :detach asked for over RPC detaches the interface last in use,
// which is the one only when there is one, so callers count first.
// chanclose on the interface's channel is not the way: when it is the
// interface Neovim was started with, the server ends with it.
func DetachUI(launcher, socket string) error {
	if _, err := RemoteExpr(launcher, socket, "execute('detach')"); err != nil {
		return err
	}
	deadline := time.Now().Add(closeWait)
	for {
		if n, err := UIs(launcher, socket); err == nil && n == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the editor kept its other window; close it there and try again")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// OpenFile keeps unsaved buffers in their own tab instead of replacing them
// when the file browser hands a file to an already running editor.
func OpenFile(launcher, socket, file string) error {
	encoded, err := json.Marshal(file)
	if err != nil {
		return err
	}
	literal := strings.ReplaceAll(string(encoded), "'", "''")
	_, err = RemoteExpr(launcher, socket, "execute('tabedit ' . fnameescape(json_decode('"+literal+"')))")
	return err
}
