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
	cmd, err := e.CommandAt(dir, file)
	if err != nil {
		return nil, err
	}
	cmd.Args = append(cmd.Args, "--listen", socket)
	for _, mode := range []string{"nnoremap", "inoremap", "vnoremap"} {
		cmd.Args = append(cmd.Args, "-c", mode+" <C-z> <cmd>detach<cr>")
	}
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

func Modified(launcher, socket string) (bool, error) {
	out, err := RemoteExpr(launcher, socket, "len(getbufinfo({'bufmodified': 1}))")
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
// Rechecking inside Neovim closes the race with another attached UI.
func Close(launcher, socket string) (bool, error) {
	out, err := RemoteExpr(launcher, socket, "len(getbufinfo({'bufmodified': 1})) ? 1 : execute('confirm qa')")
	if !SocketAlive(socket) {
		return true, nil
	}
	if out == "1" {
		return false, nil
	}
	return false, err
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
