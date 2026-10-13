package unibox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Problem is one finding of Validate: what it concerns and what to do.
type Problem struct {
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// Validate checks the environments repository as a whole: the layout, every
// profile with its instructions, and every attachable text - so a mistake
// surfaces now, not at the next start. One repository today; reporting by
// subject keeps room for several sources later, a team's beside the user's.
func Validate(root string) ([]Problem, error) {
	if root == "" {
		return nil, fmt.Errorf("no environments repository configured; run unibox config set environments <path> or unibox init")
	}
	abs, err := ExpandPath(root)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	if err := CheckLayout(abs); err != nil {
		return []Problem{{Subject: "layout", Message: err.Error()}}, nil
	}
	problems := []Problem{}
	names, err := ProfileNames(abs)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		p, err := LoadProfile(abs, name)
		if err == nil {
			_, err = p.InstructionText(nil)
		}
		if err != nil {
			problems = append(problems, Problem{Subject: "profile " + name, Message: err.Error()})
		}
	}
	texts, err := os.ReadDir(filepath.Join(abs, "instructions"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range texts {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := readInside(abs, filepath.Join(abs, "instructions", e.Name())); err != nil {
			problems = append(problems, Problem{Subject: "instructions/" + e.Name(), Message: err.Error()})
		}
	}
	return problems, nil
}
