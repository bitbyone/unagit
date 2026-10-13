package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// LocalFolder is read before anything is initialised or sent to a server.
type LocalFolder struct {
	Directory  string
	Repository bool
	Branch     string
	HasCommits bool
	Remotes    []string
}

func InspectLocalFolder(dir string) (LocalFolder, error) {
	var f LocalFolder
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return f, err
	}
	f.Directory, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return f, fmt.Errorf("choose an existing directory: %w", err)
	}
	st, err := os.Stat(f.Directory)
	if err != nil || !st.IsDir() {
		return f, fmt.Errorf("choose a directory")
	}
	g := gitx.New("", nil)
	top, err := g.Run(f.Directory, "rev-parse", "--show-toplevel")
	if err != nil {
		if bare, _ := g.Run(f.Directory, "rev-parse", "--is-bare-repository"); strings.TrimSpace(bare) == "true" {
			return f, fmt.Errorf("a bare repository cannot be published as a folder; choose a working tree")
		}
		if _, err := os.Lstat(filepath.Join(f.Directory, ".git")); !os.IsNotExist(err) {
			return f, fmt.Errorf("cannot read this repository; repair its .git directory first")
		}
		f.Branch = "main"
		return f, nil
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(top))
	if err != nil {
		return f, err
	}
	f.Directory = root
	st, err = os.Lstat(filepath.Join(root, ".git"))
	if err != nil || !st.IsDir() {
		return f, fmt.Errorf("a linked worktree or submodule cannot be published separately; choose its main repository")
	}
	f.Repository = true
	branch, err := g.Run(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return f, fmt.Errorf("HEAD is detached; check out a branch before publishing")
	}
	f.Branch = strings.TrimSpace(branch)
	_, err = g.Run(root, "rev-parse", "--verify", "HEAD")
	f.HasCommits = err == nil
	remotes, err := g.Run(root, "remote")
	if err != nil {
		return f, err
	}
	f.Remotes = strings.Fields(remotes)
	return f, nil
}

func InitLocalFolder(dir, branch string) error {
	g := gitx.New("", nil)
	if _, err := g.Run(dir, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("choose a valid first branch: %w", err)
	}
	f, err := InspectLocalFolder(dir)
	if err != nil {
		return err
	}
	if f.Directory != dir {
		return fmt.Errorf("choose the repository root %s", f.Directory)
	}
	if f.Repository {
		return nil
	}
	_, err = g.Run(dir, "init", "--initial-branch="+branch)
	return err
}

// Publication survives a failed push and a restart. A request whose answer was
// lost is never repeated blindly: the user must identify the repository first.
type Publication struct {
	RepositoryID string
	journal      string
	MoveSource   string
	Directory    string
	Instance     string
	Group        forge.Group
	Request      forge.NewProject
	Branch       string
	AllBranches  bool
	Tags         bool
	Requested    bool
	Project      *forge.Project
	Complete     bool
	MoveTo       string
}

func PublicationPath(configDir, source string) string {
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(configDir, "publications", fmt.Sprintf("%x.json", sum[:]))
}

func ReadPublication(path string) (*Publication, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Publication
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("cannot read publication progress; keep the file and repair it: %w", err)
	}
	p.journal = path
	if p.Project != nil && p.MoveTo != "" {
		if _, err := os.Stat(p.Directory); os.IsNotExist(err) {
			if New(Options{LocalDirectory: p.MoveTo}, nil).CheckLocalProject(*p.Project) == nil {
				p.Directory, p.MoveTo = p.MoveTo, ""
			}
		}
	}
	if p.MoveSource != "" {
		if _, err := os.Stat(p.MoveSource); os.IsNotExist(err) {
			p.MoveSource = ""
		}
	}
	return &p, nil
}

func (p *Publication) Save(path string) error {
	p.journal = path
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".publication-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncPath(filepath.Dir(path))
}

type PublicationProvider interface {
	CreateProject(context.Context, forge.Group, forge.NewProject) (*forge.Project, error)
	SetDefaultBranch(context.Context, forge.Project, string) error
}

// PublishLocal only appends history to the remote. Every durable step can be
// repeated; an existing origin is accepted only when this journal names it.
func (m *Manager) PublishLocal(ctx context.Context, client PublicationProvider, p *Publication, journal string, adopted func(forge.Project) error) error {
	unlock, err := lockPublication(journal)
	if err != nil {
		return err
	}
	defer unlock()
	saved, err := ReadPublication(journal)
	if err != nil {
		return err
	}
	if saved != nil && saved.Requested {
		if saved.Instance != p.Instance || saved.Group.FullPath != p.Group.FullPath || saved.Request.Name != p.Request.Name || saved.Branch != p.Branch {
			return fmt.Errorf("this folder has an unfinished publication; reopen it to continue")
		}
		*p = *saved
	}
	f, err := InspectLocalFolder(p.Directory)
	if err != nil {
		return err
	}
	if !f.Repository || f.Directory != p.Directory {
		return fmt.Errorf("initialise the chosen folder before publishing")
	}
	if f.Branch != p.Branch {
		return fmt.Errorf("the checked-out branch changed; return to %s before continuing", p.Branch)
	}
	if len(f.Remotes) != 0 && (p.Project == nil || len(f.Remotes) != 1 || f.Remotes[0] != "origin") {
		return fmt.Errorf("this folder already has a remote; choose a repository without remotes")
	}
	identity, _ := m.git.Run(p.Directory, "config", "--local", "--get", "unagit.publication")
	identity = strings.TrimSpace(identity)
	if p.RepositoryID != "" && identity != p.RepositoryID {
		return fmt.Errorf("the source repository was replaced; restore the original folder before continuing")
	}
	if p.RepositoryID == "" {
		if identity == "" {
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return err
			}
			identity = fmt.Sprintf("%x", id[:])
			if _, err := m.git.Run(p.Directory, "config", "unagit.publication", identity); err != nil {
				return err
			}
		}
		p.RepositoryID = identity
	}
	if p.Project == nil {
		if p.Requested {
			return fmt.Errorf("the server may already have created this repository; reopen New Repository from Folder to recover it before retrying")
		}
		p.Requested = true
		if err = p.Save(journal); err != nil {
			return err
		}
		req := p.Request
		req.Readme, req.License, req.Gitignore = false, "", ""
		pr, createErr := client.CreateProject(ctx, p.Group, req)
		if pr == nil {
			return fmt.Errorf("creation was not confirmed; reopen New Repository from Folder to recover or retry: %w", createErr)
		}
		pr.Instance = p.Instance
		p.Project = pr
		if err = p.Save(journal); err != nil {
			return fmt.Errorf("repository created, but progress could not be saved; keep this folder: %w", err)
		}
		if createErr != nil {
			m.log("%s", createErr)
		}
	}
	pr := *p.Project
	remote := m.RemoteURL(pr)
	if len(f.Remotes) == 0 {
		if _, err = m.git.Run(p.Directory, "remote", "add", "origin", remote); err != nil {
			return err
		}
	} else if have, e := m.git.RemoteURL(p.Directory, "origin"); e != nil || have != remote {
		return fmt.Errorf("origin changed since publication; restore it before continuing")
	}
	if _, err = m.git.Run(p.Directory, "config", "unagit.localProject", pr.Instance+"/"+pr.PathWithNamespace); err != nil {
		return err
	}
	if err = adopted(pr); err != nil {
		return err
	}
	if f.HasCommits {
		branches := []string{p.Branch}
		if p.AllBranches {
			branches = m.git.LocalBranches(p.Directory)
		}
		for _, branch := range branches {
			if _, err = m.git.Run(p.Directory, "push", "--set-upstream", "origin", "refs/heads/"+branch+":refs/heads/"+branch); err != nil {
				return err
			}
		}
		if p.Tags {
			if _, err = m.git.Run(p.Directory, "push", "origin", "--tags"); err != nil {
				return err
			}
		}
		if err = client.SetDefaultBranch(ctx, pr, p.Branch); err != nil {
			return err
		}

		p.Project.DefaultBranch = p.Branch
		if err = adopted(*p.Project); err != nil {
			return err
		}
	}
	p.Complete = true
	return p.Save(journal)
}

// CheckLocalProject prevents a replaced folder being mistaken for the checkout
// remembered on this machine. The marker contains no remote credentials.
func (m *Manager) CheckLocalProject(p forge.Project) error {
	dir := m.ProjectDir(p.PathWithNamespace)
	g := gitx.New("", nil)
	mark, err := g.Run(dir, "config", "--local", "--get", "unagit.localProject")
	if err != nil || strings.TrimSpace(mark) != p.Instance+"/"+p.PathWithNamespace {
		return fmt.Errorf("the remembered folder %s is missing or belongs to another repository; restore it before opening", dir)
	}
	remote, err := g.RemoteURL(dir, "origin")
	if err != nil || (remote != p.HTTPURLToRepo && remote != p.SSHURLToRepo && remote != m.RemoteURL(p)) {
		return fmt.Errorf("origin in %s no longer belongs to this repository; restore it before opening", dir)
	}
	return nil
}

func (p *Publication) JournalPath(configDir string) string {
	if p.journal != "" {
		return p.journal
	}
	return PublicationPath(configDir, p.Directory)
}

func FindPublication(configDir, dir string) (*Publication, error) {
	p, err := ReadPublication(PublicationPath(configDir, dir))
	if p != nil || err != nil {
		return p, err
	}
	paths, err := filepath.Glob(filepath.Join(configDir, "publications", "*.json"))
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		p, err := ReadPublication(path)
		if err != nil {
			return nil, err
		}
		if p != nil {
			actual, _ := filepath.EvalSymlinks(p.Directory)
			if p.Directory == dir || p.MoveTo == dir || actual == dir {
				return p, nil
			}
		}
	}
	return nil, nil
}

func lockPublication(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("this publication is already running; wait for it to finish")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
