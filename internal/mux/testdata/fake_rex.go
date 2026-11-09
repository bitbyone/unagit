package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A rex stand-in: it writes down each call with the server it was pointed
// at, keeps sessions and blocks in a file beside itself, and answers the
// way rex does with --json and -q.

type block struct {
	ID      string `json:"id"`
	Session string `json:"session"`
}

type state struct {
	Sessions []map[string]string `json:"sessions"`
	Blocks   []block             `json:"blocks"`
}

func main() {
	if os.Getenv("UNAGIT_FAKE_REX_WARM") == "1" {
		return
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	args := os.Args[1:]
	log, _ := os.OpenFile(filepath.Join(dir, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	data, _ := json.Marshal(struct {
		Args   []string
		Server string
	}{args, os.Getenv("REX_SERVER")})
	log.Write(append(data, '\n'))
	log.Close()

	command := strings.Join(args[:min(2, len(args))], " ")
	if failure, err := os.ReadFile(filepath.Join(dir, "failure")); err == nil {
		code, on, _ := strings.Cut(strings.TrimSpace(string(failure)), " ")
		if on == "" || strings.HasPrefix(command, on) {
			fmt.Fprintln(os.Stderr, "Error: the stand-in was told to fail")
			n, _ := strconv.Atoi(code)
			os.Exit(n)
		}
	}
	var st state
	path := filepath.Join(dir, "state")
	current, _ := os.ReadFile(path)
	json.Unmarshal(current, &st)
	save := func() {
		data, _ := json.Marshal(st)
		os.WriteFile(path+".write", data, 0o600)
		os.Rename(path+".write", path)
	}
	add := func(session string) string {
		id := fmt.Sprintf("block:%d", len(st.Blocks)+1)
		st.Blocks = append(st.Blocks, block{id, session})
		return id
	}
	reply := func(v any) {
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
	}
	flag := func(name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	switch {
	case command == "ls --json":
		reply(map[string]any{"sessions": st.Sessions})
	case args[0] == "new":
		id := fmt.Sprintf("session:%d", len(st.Sessions)+1)
		st.Sessions = append(st.Sessions, map[string]string{"session_id": id, "label": args[1]})
		add(id)
		save()
		reply(map[string]any{"session_id": id})
	case args[0] == "run":
		b := add(flag("-s"))
		save()
		reply(map[string]any{"block_ids": []string{b}, "window_id": "window:" + b})
	case args[0] == "split":
		b := add("")
		save()
		reply(map[string]any{"anchor_block_id": flag("-b"), "block_ids": []string{b}, "split_ids": []string{"split:1"}})
	case command == "block ls":
		session := flag("-s")
		for _, b := range st.Blocks {
			if session == "" || b.Session == session {
				fmt.Println(b.ID)
			}
		}
	case command == "block focus", command == "window rename":
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n", strings.Join(args, " "))
		os.Exit(2)
	}
}
