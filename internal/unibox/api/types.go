// Package api is the versioned command-line contract shared with unibox clients.
package api

import "time"

const Version = 1

type Repository struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	HostPath string `json:"host_path,omitempty"`
	Author   string `json:"author"`
}

type Task struct {
	ID           string       `json:"id"`
	Sandbox      string       `json:"sandbox"`
	Directory    string       `json:"directory"`
	Repositories []Repository `json:"repositories"`
	Agent        string       `json:"agent,omitempty"`
	State        string       `json:"state"`
}

type Run struct {
	ID      string    `json:"id"`
	Sandbox string    `json:"sandbox"`
	Task    string    `json:"task,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	PID     int       `json:"pid"`
	Pane    string    `json:"pane,omitempty"`
	Socket  string    `json:"socket,omitempty"`
	Since   time.Time `json:"since"`
}

type Sandbox struct {
	Name                string   `json:"name"`
	Runtime             string   `json:"runtime"`
	RuntimeName         string   `json:"runtime_name"`
	RuntimeID           string   `json:"runtime_id,omitempty"`
	Profile             string   `json:"profile"`
	Instructions        []string `json:"instructions"`
	Revision            string   `json:"revision,omitempty"`
	InstructionRevision string   `json:"instruction_revision,omitempty"`
	State               string   `json:"state"`
	Memory              string   `json:"memory,omitempty"`
	Tasks               []Task   `json:"tasks"`
	Runs                []Run    `json:"runs"`
}

type Profile struct {
	Name       string            `json:"name"`
	Agents     []string          `json:"agents"`
	Revision   string            `json:"revision,omitempty"`
	Images     map[string]string `json:"images"`
	Approved   bool              `json:"approved"`
	Error      string            `json:"error,omitempty"`
	LastCommit string            `json:"last_commit,omitempty"`
}

type Need struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Repository string `json:"repository,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Warning    bool   `json:"warning,omitempty"`
}

type Check struct {
	APIVersion   int          `json:"api_version"`
	Ready        bool         `json:"ready"`
	Needs        []Need       `json:"needs"`
	Repositories []Repository `json:"repositories"`
}

type Prepared struct {
	APIVersion  int    `json:"api_version"`
	Sandbox     string `json:"sandbox"`
	Runtime     string `json:"runtime"`
	RuntimeName string `json:"runtime_name"`
	Task        string `json:"task"`
	Directory   string `json:"directory"`
	State       string `json:"state"`
}

type Error struct {
	APIVersion int    `json:"api_version"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}
