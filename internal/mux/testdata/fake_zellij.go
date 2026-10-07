package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pane struct {
	ID     int  `json:"id"`
	Plugin bool `json:"is_plugin"`
	Exited bool `json:"exited"`
	TabID  int  `json:"tab_id"`
}

func main() {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	args := os.Args[1:]
	cwd, _ := os.Getwd()
	log, _ := os.OpenFile(filepath.Join(dir, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	data, _ := json.Marshal(struct {
		Args        []string
		Dir, Source string
	}{args, cwd, os.Getenv("ZELLIJ_PANE_ID")})
	log.Write(append(data, '\n'))
	log.Close()
	action := args[3]
	if failure, err := os.ReadFile(filepath.Join(dir, "failure")); err == nil && (!strings.HasPrefix(string(failure), "focus") || action == "focus-pane-id") {
		if string(failure) == "missing" {
			fmt.Fprintf(os.Stderr, "Session '%s' not found.\n", args[1])
		} else if string(failure) == "focused" {
			fmt.Fprintf(os.Stderr, "Pane Terminal(%s) is already focused\n", strings.TrimPrefix(args[4], "terminal_"))
		} else {
			fmt.Fprintln(os.Stderr, string(failure))
		}
		os.Exit(1)
	}
	var panes []pane
	state := filepath.Join(dir, "panes")
	current, _ := os.ReadFile(state)
	json.Unmarshal(current, &panes)
	switch action {
	case "list-panes":
		if _, err := os.Stat(filepath.Join(dir, "gate")); err == nil {
			fmt.Println("[]")
			return
		}
		fmt.Println(string(current))
	case "new-tab", "new-pane":
		id := 10
		for _, p := range panes {
			if p.ID >= id {
				id = p.ID + 1
			}
		}
		tab := 0
		if action == "new-tab" {
			for _, p := range panes {
				tab = max(tab, p.TabID)
			}
			tab++
		}
		panes = append(panes, pane{ID: id, TabID: tab})
		data, _ := json.Marshal(panes)
		tmp := state + ".write"
		os.WriteFile(tmp, data, 0600)
		os.Rename(tmp, state)
		if action == "new-tab" {
			fmt.Println(tab)
		} else {
			fmt.Printf("terminal_%d\n", id)
		}
	case "focus-pane-id":
	default:
		os.Exit(2)
	}
}
