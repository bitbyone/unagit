package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// A grouped worktree is one directory holding a worktree of several
// repositories side by side, so that an editor - or an agent - opened there
// sees all of them at once:
//
//	<root>/.unagit/groups/<name>/<repo>      a linked worktree of <repo>'s main clone
//	<root>/.unagit/groups/<name>/.unagit-group.json
//
// Each member is an ordinary worktree of its own repository and shares that
// repository's object store. The file says which repositories they are, because
// a directory name alone cannot tell two servers or two groups apart.

// GroupFile describes a grouped worktree, in its own directory.
const GroupFile = ".unagit-group.json"

// Group is what GroupFile holds.
type Group struct {
	Name string `json:"name"`
	// Branch is the branch created for every member, or "" when each member
	// checked out a branch of its own.
	Branch  string        `json:"branch,omitempty"`
	Created time.Time     `json:"created"`
	Members []GroupMember `json:"members"`
}

// GroupMember is one repository of a grouped worktree.
type GroupMember struct {
	Instance string `json:"instance"`
	Project  string `json:"project"`
	// Dir is the member's directory name inside the group.
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	// Base is where a new branch started; empty when an existing branch was
	// checked out.
	Base string `json:"base,omitempty"`
}

// GroupsRoot holds every grouped worktree under a clone root. It sits in the
// same hidden directory as the other worktrees, where no group of a server can
// be called the same.
func GroupsRoot(root string) string {
	return filepath.Join(config.Expand(root), ".unagit", "groups")
}

// ReadGroup reads the description of the grouped worktree in dir.
func ReadGroup(dir string) (Group, error) {
	data, err := os.ReadFile(filepath.Join(dir, GroupFile))
	if err != nil {
		return Group{}, err
	}
	var g Group
	if err := json.Unmarshal(data, &g); err != nil {
		return Group{}, fmt.Errorf("%s: %w", filepath.Join(dir, GroupFile), err)
	}
	return g, nil
}

// WriteGroup writes the description of a grouped worktree into dir.
func WriteGroup(dir string, g Group) error {
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, GroupFile), append(data, '\n'), 0o644)
}

// GroupDir is a grouped worktree found on disk.
type GroupDir struct {
	Dir   string
	Group Group
}

// ListGroups finds every grouped worktree under root, by name. A directory
// without a readable description is not one.
func ListGroups(root string) []GroupDir {
	base := GroupsRoot(root)
	entries, _ := os.ReadDir(base)
	var out []GroupDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if g, err := ReadGroup(dir); err == nil {
			out = append(out, GroupDir{Dir: dir, Group: g})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// MemberDirNames gives every repository a directory name inside a group: its
// own name, or its whole path flattened when two would share one.
func MemberDirNames(projects []string) []string {
	count := map[string]int{}
	for _, p := range projects {
		count[strings.ToLower(filepath.Base(p))]++
	}
	out := make([]string, len(projects))
	for i, p := range projects {
		name := filepath.Base(p)
		if count[strings.ToLower(name)] > 1 {
			name = strings.ReplaceAll(p, "/", "-")
		}
		out[i] = Sanitize(name)
	}
	return out
}

// NewMemberDirName names the folder of a repository joining a group, apart
// from the folders already there: its own name, its whole path flattened when
// that is taken, and a number after it when even that is.
func NewMemberDirName(taken []string, projectPath string) string {
	used := map[string]bool{GroupFile: true, ".incomm": true}
	for _, t := range taken {
		used[strings.ToLower(t)] = true
	}
	for _, name := range []string{Sanitize(filepath.Base(projectPath)), Sanitize(strings.ReplaceAll(projectPath, "/", "-"))} {
		if !used[strings.ToLower(name)] {
			return name
		}
	}
	base := Sanitize(strings.ReplaceAll(projectPath, "/", "-"))
	for n := 2; ; n++ {
		if name := fmt.Sprintf("%s-%d", base, n); !used[strings.ToLower(name)] {
			return name
		}
	}
}

// GroupExtras lists what a grouped worktree holds besides its members and its
// description - notes an agent or a person left there - so that deleting it
// can say so first.
func GroupExtras(dir string, g Group) []string {
	members := map[string]bool{GroupFile: true}
	for _, m := range g.Members {
		members[m.Dir] = true
	}
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if members[e.Name()] {
			continue
		}
		// An Incomm store with no comments in it yet is the group's own.
		if e.Name() == ".incomm" {
			if notes, _ := filepath.Glob(filepath.Join(dir, ".incomm", "notes*.json")); len(notes) == 0 {
				continue
			}
			out = append(out, ".incomm (Incomm comments)")
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// PrepareGroupMember makes sure the repository is cloned and its view of
// origin is fresh, before anything is checked for or created.
func (m *Manager) PrepareGroupMember(p forge.Project) error {
	dir, err := m.ensureMain(p)
	if err != nil {
		return err
	}
	if err := m.git.Fetch(dir); err != nil {
		m.log("! fetch of %s failed, using the refs already on disk", p.PathWithNamespace)
	}
	return nil
}

// CheckGroupMember says why a member could not be created as asked, before
// any member is: a group half made is worse than none. branch is the branch
// to create when isNew, from base; otherwise it is the branch to check out.
func (m *Manager) CheckGroupMember(p forge.Project, branch, base string, isNew bool) error {
	dir := m.ProjectDir(p.PathWithNamespace)
	if isNew {
		switch {
		case m.git.LocalBranchExists(dir, branch):
			return fmt.Errorf("%s already has a branch %s - give the group another name", p.PathWithNamespace, branch)
		case m.git.RemoteBranchExists(dir, branch):
			return fmt.Errorf("%s already has %s on origin - give the group another name", p.PathWithNamespace, branch)
		case m.startOfGroupBranch(dir, base) == "":
			return fmt.Errorf("%s has no branch %s to start from", p.PathWithNamespace, base)
		}
		return nil
	}
	if other := m.git.CheckedOutIn(dir, branch); other != "" {
		return fmt.Errorf("%s: %s is already checked out in %s, and git checks a branch out only once - "+
			"give the group a new branch, or switch that checkout to another", p.PathWithNamespace, branch, other)
	}
	if !m.git.LocalBranchExists(dir, branch) && !m.git.RemoteBranchExists(dir, branch) {
		return fmt.Errorf("%s has no branch %s, locally or on origin", p.PathWithNamespace, branch)
	}
	return nil
}

// startOfGroupBranch is where a new branch based on base starts: origin's copy
// when there is one, since the local branch may lag behind it.
func (m *Manager) startOfGroupBranch(dir, base string) string {
	return m.git.BaseRef(dir, base)
}

// AddGroupMember checks the repository out into wtDir, on a new branch from
// base or on an existing one, the way CheckGroupMember has approved.
func (m *Manager) AddGroupMember(p forge.Project, wtDir, branch, base string, isNew bool) error {
	dir := m.ProjectDir(p.PathWithNamespace)
	m.git.WorktreePrune(dir)
	m.log("Checking out %s in %s", branch, p.PathWithNamespace)
	switch {
	case isNew:
		// Made from origin's copy it would track the base and push back to it;
		// it gets an upstream of its own when it is first pushed.
		if _, err := m.git.Run(dir, "branch", "--no-track", branch, m.startOfGroupBranch(dir, base)); err != nil {
			return err
		}
		_ = m.git.SetBranchBase(dir, branch, base)
		return m.git.WorktreeAdd(dir, wtDir, branch)
	case m.git.LocalBranchExists(dir, branch):
		return m.git.WorktreeAdd(dir, wtDir, branch)
	default:
		if err := m.addWorktreeFrom(dir, wtDir, branch, "origin/"+branch); err != nil {
			return err
		}
		_ = m.git.SetUpstream(wtDir, branch, branch)
		return nil
	}
}

// RemoveGroupMember detaches one member from its repository and deletes it.
// A new branch made for the group stays: it may hold work not pushed yet.
func (m *Manager) RemoveGroupMember(projectPath, dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	mainDir := m.ProjectDir(projectPath)
	m.log("Removing %s", dir)
	if Exists(mainDir) {
		if err := m.git.WorktreeRemove(mainDir, dir, true); err != nil {
			m.log("! worktree remove failed, deleting the directory directly")
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if Exists(mainDir) {
		m.git.WorktreePrune(mainDir)
	}
	return nil
}
