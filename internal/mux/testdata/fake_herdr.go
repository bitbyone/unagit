package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A herdr stand-in: it writes down each call with the environment unagit
// gave it, and answers the way herdr 0.9 does.

type pane struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

func main() {
	if os.Getenv("UNAGIT_FAKE_HERDR_WARM") == "1" {
		return
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	args := os.Args[1:]
	log, _ := os.OpenFile(filepath.Join(dir, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	data, _ := json.Marshal(struct {
		Args           []string
		Socket, Launch string
	}{args, os.Getenv("HERDR_SOCKET_PATH"), envOf(args)})
	log.Write(append(data, '\n'))
	log.Close()

	command := strings.Join(args[:min(2, len(args))], " ")
	if failure, err := os.ReadFile(filepath.Join(dir, "failure")); err == nil {
		code, on, _ := strings.Cut(strings.TrimSpace(string(failure)), " ")
		if on == "" || on == command {
			fail(code)
		}
	}
	var panes []pane
	state := filepath.Join(dir, "panes")
	current, _ := os.ReadFile(state)
	json.Unmarshal(current, &panes)
	add := func(workspace, tab string) pane {
		p := pane{PaneID: fmt.Sprintf("%s:p%d", workspace, len(panes)+1), TabID: tab, WorkspaceID: workspace}
		panes = append(panes, p)
		data, _ := json.Marshal(panes)
		os.WriteFile(state+".write", data, 0o600)
		os.Rename(state+".write", state)
		return p
	}
	switch command {
	case "pane list":
		reply(map[string]any{"type": "pane_list", "panes": panes})
	case "workspace list":
		var workspaces []map[string]string
		if label, err := os.ReadFile(filepath.Join(dir, "workspace")); err == nil {
			workspaces = append(workspaces, map[string]string{"workspace_id": "wN", "label": string(label)})
		}
		workspaces = append(workspaces, map[string]string{"workspace_id": "wA", "label": "work"})
		reply(map[string]any{"type": "workspace_list", "workspaces": workspaces})
	case "tab list":
		reply(map[string]any{"type": "tab_list", "tabs": []map[string]string{{"tab_id": "wA:t1", "workspace_id": "wA", "label": "gateway !7"}}})
	case "agent list":
		// The agents are what the test wrote, in herdr's own words.
		var agents []map[string]any
		data, _ := os.ReadFile(filepath.Join(dir, "agents"))
		json.Unmarshal(data, &agents)
		if agents == nil {
			agents = []map[string]any{}
		}
		reply(map[string]any{"type": "agent_list", "agents": agents})
	case "pane close":
		reply(map[string]any{"type": "ok"})
	case "workspace create":
		p := add("wN", "wN:t1")
		os.WriteFile(filepath.Join(dir, "workspace"), []byte(args[5]), 0o600)
		reply(map[string]any{"type": "workspace_created", "root_pane": p})
	case "tab create":
		workspace := args[3]
		p := add(workspace, workspace+":t9")
		reply(map[string]any{"type": "tab_created", "root_pane": p})
	case "pane split":
		p := add("wU", "wU:t1")
		reply(map[string]any{"type": "pane_info", "pane": p})
	case "pane run":
		// herdr says nothing when it has typed the line.
	case "pane focus", "tab focus", "tab rename", "agent focus":
		reply(map[string]any{"type": "ok"})
	case "agent start":
		reply(map[string]any{"type": "agent_started"})
	default:
		fail("unknown_command")
	}
}

func envOf(args []string) string {
	for i, arg := range args {
		if arg == "--env" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func reply(result any) {
	data, _ := json.Marshal(map[string]any{"id": "cli", "result": result})
	fmt.Println(string(data))
}

func fail(code string) {
	data, _ := json.Marshal(map[string]any{"id": "cli", "error": map[string]string{"code": code, "message": "fake " + code}})
	fmt.Println(string(data))
	os.Exit(1)
}
