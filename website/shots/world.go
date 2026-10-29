//go:build shots

package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// The servers' side of the world, as mock lays it out and serve answers
// from: who the token is, and each server's groups, projects, pipelines
// and merge requests. It is written to server.json beside the world; what
// is pushed to a project's origin while serve runs is read from the origin.

type serverWorld struct {
	Me    forge.User       `json:"me"`
	Users []forge.User     `json:"users"`
	Hosts map[string]*host `json:"hosts"`
}

type host struct {
	Groups   []forge.Group `json:"groups"`
	Projects []*wproject   `json:"projects"`
	MRs      []*wmr        `json:"mrs"`
}

// wproject is a project and what the server knows of it. Origin is the bare
// repository its clones push to, "" for one never cloned: its commits and
// branches are made up then.
type wproject struct {
	forge.Project
	Origin    string             `json:"origin,omitempty"`
	Languages map[string]float64 `json:"languages,omitempty"`
	Pipelines []*wpipeline       `json:"pipelines,omitempty"`
}

// wpipeline is a pipeline of a ref. SHA "" is the ref's head on origin, as
// serve finds it on start. Status, when set, is what it says whatever its
// jobs do.
type wpipeline struct {
	ID      int        `json:"id"`
	Ref     string     `json:"ref"`
	SHA     string     `json:"sha,omitempty"`
	Source  string     `json:"source,omitempty"`
	Created time.Time  `json:"created"`
	User    forge.User `json:"user"`
	Status  string     `json:"status,omitempty"`
	Jobs    []*wjob    `json:"jobs"`
}

// wjob is a job. Status, when set, is where it stays; otherwise it runs
// from Start for Seconds, and ends Final - success unless said.
type wjob struct {
	ID      int64      `json:"id"`
	Name    string     `json:"name"`
	Stage   string     `json:"stage"`
	Status  string     `json:"status,omitempty"`
	Final   string     `json:"final,omitempty"`
	Start   time.Time  `json:"start"`
	Seconds float64    `json:"seconds"`
	Log     string     `json:"log,omitempty"`
	User    forge.User `json:"user"`
}

// wmr is a merge request: the list's fields, and what its detail, threads
// and commits say. SHA "" is its source branch's head on origin.
type wmr struct {
	forge.MergeRequest
	Description string         `json:"description"`
	Created     time.Time      `json:"created"`
	Discussions []*wdiscussion `json:"discussions,omitempty"`
	Commits     []string       `json:"commits,omitempty"`
	Changes     int            `json:"changes"`
}

type wdiscussion struct {
	ID    string   `json:"id"`
	Notes []*wnote `json:"notes"`
}

type wnote struct {
	ID         int        `json:"id"`
	Body       string     `json:"body"`
	Created    time.Time  `json:"created"`
	Author     forge.User `json:"author"`
	Resolvable bool       `json:"resolvable"`
	Resolved   bool       `json:"resolved"`
}

func (w *serverWorld) save(path string) {
	b, err := json.MarshalIndent(w, "", " ")
	check(err)
	check(os.WriteFile(path, b, 0o644))
}

func loadWorld(path string) *serverWorld {
	b, err := os.ReadFile(path)
	check(err)
	var w serverWorld
	check(json.Unmarshal(b, &w))
	return &w
}

// status is where the job stands at now.
func (j *wjob) status(now time.Time) string {
	switch {
	case j.Status != "":
		return j.Status
	case now.Before(j.Start):
		return "pending"
	case now.Before(j.Start.Add(time.Duration(j.Seconds * float64(time.Second)))):
		return "running"
	case j.Final != "":
		return j.Final
	}
	return "success"
}

// latest is the newest attempt of each job, in the order they were made.
func (p *wpipeline) latest() []*wjob {
	last := map[string]int{}
	for i, j := range p.Jobs {
		last[j.Name] = i
	}
	var out []*wjob
	for i, j := range p.Jobs {
		if last[j.Name] == i {
			out = append(out, j)
		}
	}
	return out
}

// status is the pipeline's: running while a job is, failed for a failed
// job, success when every job passed or waits for a hand.
func (p *wpipeline) status(now time.Time) string {
	if p.Status != "" {
		return p.Status
	}
	running, failed := false, false
	for _, j := range p.latest() {
		switch j.status(now) {
		case "running", "pending":
			running = true
		case "failed":
			failed = true
		}
	}
	switch {
	case running:
		return "running"
	case failed:
		return "failed"
	}
	return "success"
}

// started is when its first job began.
func (p *wpipeline) started() time.Time {
	at := p.Created
	for _, j := range p.Jobs {
		if !j.Start.IsZero() && (at.IsZero() || j.Start.Before(at)) {
			at = j.Start
		}
	}
	return at
}

// updated is when it last moved.
func (p *wpipeline) updated(now time.Time) time.Time {
	at := p.Created
	for _, j := range p.Jobs {
		end := j.Start.Add(time.Duration(j.Seconds * float64(time.Second)))
		if end.After(now) {
			end = now
		}
		if end.After(at) {
			at = end
		}
	}
	return at
}
