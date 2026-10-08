// This editor fixture keeps its listener in a separate process, just as
// Neovim does after its UI is detached. No real configuration is read.
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("NVIM v0.12.5")
		return
	}
	if len(args) > 0 && args[0] == "--fake-server" {
		serve(args[1])
		return
	}
	socket, expr, attach := "", "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen", "--server":
			i++
			socket = args[i]
		case "--remote-expr":
			i++
			expr = args[i]
		case "--remote-ui":
			attach = true
		}
	}
	if expr != "" {
		c, err := net.Dial("unix", socket)
		if err != nil {
			os.Exit(1)
		}
		defer c.Close()
		fmt.Fprintln(c, expr)
		out, _ := bufio.NewReader(c).ReadString('\n')
		fmt.Print(out)
		return
	}
	if attach {
		log("attach", socket)
		cwd, _ := os.Getwd()
		log("directory", cwd)
		return
	}
	log("start", socket)
	if os.Getenv("UNAGIT_FAKE_NVIM_CLOSE") == "1" {
		return
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "--fake-server", socket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() != nil {
		os.Exit(1)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		c, err := net.Dial("unix", socket)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(1)
}

func log(what, socket string) {
	f, err := os.OpenFile(os.Getenv("UNAGIT_FAKE_NVIM_LOG"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err == nil {
		defer f.Close()
		fmt.Fprintln(f, what+"|"+socket)
	}
}

func serve(socket string) {
	l, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(1)
	}
	defer l.Close()
	// A failed test can remove the socket before cleanup reaches the RPC
	// client. Its temporary log still tells this process when to leave.
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-finished:
				return
			case <-ticker.C:
				if _, err := os.Stat(os.Getenv("UNAGIT_FAKE_NVIM_LOG")); os.IsNotExist(err) {
					l.Close()
					return
				}
			}
		}
	}()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		expr, _ := bufio.NewReader(c).ReadString('\n')
		if expr == "" {
			c.Close()
			continue
		}
		_, err = os.Stat(socket + ".dirty")
		dirty := err == nil
		switch {
		case strings.Contains(expr, "qa!"):
			c.Close()
			return
		case strings.Contains(expr, "confirm qa") && !dirty:
			c.Close()
			return
		case strings.Contains(expr, "bufmodified"):
			if dirty {
				fmt.Fprintln(c, "1")
			} else {
				fmt.Fprintln(c, "0")
			}
		case strings.Contains(expr, "tabedit"):
			log("file", strings.TrimSpace(expr))
			fmt.Fprintln(c, "0")
		case strings.Contains(expr, "checktime"):
			log("checktime", socket)
			fmt.Fprintln(c, "0")
		case strings.Contains(expr, "nvim_list_uis"):
			fmt.Fprintln(c, "0")
		default:
			fmt.Fprintln(c, "0")
		}
		c.Close()
	}
}
