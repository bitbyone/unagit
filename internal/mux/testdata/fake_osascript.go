package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// An osascript stand-in for Ghostty: it writes down the arguments each
// script was given and answers what Ghostty would.

func main() {
	if os.Getenv("UNAGIT_FAKE_OSASCRIPT_WARM") == "1" {
		return
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	args := os.Args[1:]
	if len(args) < 2 || args[0] != "-e" {
		fmt.Fprintln(os.Stderr, "usage: osascript -e script args")
		os.Exit(1)
	}
	script, argv := args[1], args[2:]
	kind := "other"
	switch {
	case strings.Contains(script, "new surface configuration"):
		kind = "open"
	case strings.Contains(script, "repeat with t in terminals"):
		kind = "list"
	case strings.Contains(script, "whose name is"):
		kind = "named"
	case strings.Contains(script, "to focus"):
		kind = "focus"
	case strings.Contains(script, "to close"):
		kind = "close"
	}
	log, _ := os.OpenFile(filepath.Join(dir, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	data, _ := json.Marshal(struct {
		Kind string
		Argv []string
	}{kind, argv})
	log.Write(append(data, '\n'))
	log.Close()
	if failure, err := os.ReadFile(filepath.Join(dir, "failure")); err == nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(string(failure)))
		os.Exit(1)
	}
	switch kind {
	case "open":
		fmt.Println("NEW-TERMINAL")
	case "list":
		live, _ := os.ReadFile(filepath.Join(dir, "terminals"))
		fmt.Print(string(live))
	case "named":
		fmt.Println("SELF-TERMINAL")
	}
}
