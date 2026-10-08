package ui

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
)

// MRLink is a merge request named by its address on the forge, the way it is
// pasted from a chat or an email.
type MRLink struct {
	Instance string // the configured server it is on
	Project  string // group/project, or owner/repo
	IID      int
}

func (l MRLink) String() string { return fmt.Sprintf("%s!%d", l.Project, l.IID) }

// ParseMRLink reads a merge request address and finds the configured server it
// belongs to. Whatever follows the number - a tab, a comment anchor, a query -
// is ignored, since that is what people copy.
//
//	https://gitlab.example.com/group/sub/project/-/merge_requests/12/diffs#note_3
//	https://github.com/owner/repo/pull/12/files
func ParseMRLink(cfg *config.Config, raw string) (MRLink, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return MRLink{}, fmt.Errorf("%q is not a merge request address - paste the whole https:// link", raw)
	}
	var candidates []config.Instance
	var rest string
	for _, inst := range cfg.Instances {
		base, err := url.Parse(inst.URL)
		if err != nil || !strings.EqualFold(base.Host, u.Host) {
			continue
		}
		// A server can live below a path of its own: https://host/gitlab.
		prefix := strings.TrimSuffix(base.Path, "/")
		if !strings.HasPrefix(u.Path, prefix+"/") {
			continue
		}
		candidates = append(candidates, inst)
		rest = strings.Trim(strings.TrimPrefix(u.Path, prefix), "/")
	}
	if len(candidates) == 0 {
		return MRLink{}, fmt.Errorf("no server in Settings is at %s - add it there first", u.Host)
	}
	project, iid, ok := splitMRPath(rest, candidates[0].IsGitHub())
	if !ok {
		return MRLink{}, fmt.Errorf("%q does not point at a merge request or pull request", raw)
	}
	// Two accounts on the same server: the one that has the group selected
	// is the one whose token can see it.
	chosen := candidates[0]
	for _, inst := range candidates {
		for _, g := range inst.Groups {
			if strings.HasPrefix(strings.ToLower(project)+"/", strings.ToLower(g.FullPath)+"/") {
				chosen = inst
			}
		}
	}
	return MRLink{Instance: chosen.ID, Project: project, IID: iid}, nil
}

// splitMRPath takes "group/project/-/merge_requests/12/..." or
// "owner/repo/pull/12/..." apart.
func splitMRPath(path string, github bool) (string, int, bool) {
	parts := strings.Split(path, "/")
	marker := func(i int) bool {
		if github {
			return parts[i] == "pull" || parts[i] == "pulls"
		}
		return parts[i] == "merge_requests"
	}
	for i := 1; i+1 < len(parts); i++ {
		if !marker(i) {
			continue
		}
		iid, err := strconv.Atoi(parts[i+1])
		if err != nil || iid <= 0 {
			return "", 0, false
		}
		project := parts[:i]
		if len(project) > 0 && project[len(project)-1] == "-" {
			project = project[:len(project)-1]
		}
		if len(project) < 2 {
			return "", 0, false
		}
		return strings.Join(project, "/"), iid, true
	}
	return "", 0, false
}

// Goal is what unagit was started to do, instead of waiting for a key.
type Goal struct {
	Link   MRLink
	Review bool // the review worktree; otherwise the branch one
}

// WithGoal makes the app open a merge request as soon as the vault is open.
func (a *App) WithGoal(g Goal) *App {
	a.goal = &g
	return a
}

// pursueGoal opens the merge request the app was started for. One already in
// the list is taken from there; any other is asked of the forge, so a link to
// a repository outside the selected groups works too.
func (a *App) pursueGoal() {
	g := a.goal
	a.goal = nil
	if g == nil {
		return
	}
	a.switchTab(pageMRs)
	open := func(mr forge.MergeRequest) {
		a.withEditor(false, func(ed *editors.Editor) {
			if g.Review {
				a.openMRReview(mr, ed)
			} else {
				a.openMR(mr, ed)
			}
		})
	}
	for _, mr := range a.mrs {
		if mr.Instance == g.Link.Instance && mr.IID == g.Link.IID &&
			strings.EqualFold(a.projectPathOfMR(mr), g.Link.Project) {
			a.mrsPane.selectWhere(func(i int) bool { return keyOfMR(a.mrs[i]) == keyOfMR(mr) })
			open(mr)
			return
		}
	}
	client := a.client(g.Link.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(g.Link.Instance))
		return
	}
	stub := forge.MergeRequest{Instance: g.Link.Instance, ProjectPath: g.Link.Project, IID: g.Link.IID}
	if pr, ok := a.projByKey[projectKey{g.Link.Instance, g.Link.Project}]; ok {
		stub.ProjectID = pr.ID
	}
	// Not a task of its own: the task that opens the worktree shares its
	// page, and the finishing lookup would close it.
	job := a.startJob("looking up " + g.Link.String())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		det, err := client.MergeRequestDetail(ctx, stub)
		a.tv.QueueUpdateDraw(func() {
			a.endJob(job)
			if err != nil {
				a.errorf("%s: %v", g.Link, err)
				return
			}
			mr := det.MergeRequest
			mr.Instance, mr.ProjectPath = stub.Instance, stub.ProjectPath
			open(mr)
		})
	}()
}
