package unibox

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tobola/unagit/internal/unibox/api"
)

type RepoInput struct{ Source, Branch, Name string }
type Request struct {
	Sandbox, NewSandbox, Runtime, Profile, Task, Agent, Branch, Author string
	Repositories                                                       []RepoInput
	Instructions                                                       []string
	Push, IgnoreLocal, Yes                                             bool
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	c := hostCommand(ctx, "git", append([]string{"-C", dir}, args...)...)
	c.Env = append(c.Env, "GIT_TERMINAL_PROMPT=0")
	b, err := commandOutput(c)
	return strings.TrimSpace(string(b)), err
}

func CloneURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		at := strings.LastIndex(raw, "@")
		colon := strings.Index(raw, ":")
		if colon < 0 {
			return "", fmt.Errorf("origin is not a forge URL; use --repo https://host/group/repository.git")
		}
		host := raw[:colon]
		if at >= 0 && at < colon {
			host = raw[at+1 : colon]
		}
		raw = "https://" + host + "/" + raw[colon+1:]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("invalid clone URL; use an https forge URL")
	}
	if u.Scheme == "ssh" {
		if u.Port() != "" {
			return "", fmt.Errorf("cannot infer HTTPS from an SSH URL with a port; supply the HTTPS clone URL")
		}
		u.Scheme = "https"
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("clone URL must use https; supply the forge's HTTPS clone URL")
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	if u.Path == "" || u.Path == "/" || strings.ContainsAny(u.Path, "\n\r") {
		return "", fmt.Errorf("clone URL needs a repository path")
	}
	return u.String(), nil
}

var authorPattern = regexp.MustCompile(`^([^<>\r\n]+) <([^<>\s]+@[^<>\s]+)>$`)

func AuthorParts(author string) (string, string, error) {
	m := authorPattern.FindStringSubmatch(author)
	if m == nil {
		return "", "", fmt.Errorf("missing or invalid commit author; configure git user.name and user.email or pass --author 'Name <email>'")
	}
	return m[1], m[2], nil
}

func ResolveRepositories(ctx context.Context, req Request) ([]api.Repository, []api.Need, error) {
	repos := []api.Repository{}
	needs := []api.Need{}
	names := map[string]bool{}
	for _, in := range req.Repositories {
		r := api.Repository{Name: in.Name, Branch: in.Branch, Author: req.Author}
		if r.Branch == "" {
			r.Branch = req.Branch
		}
		_, statErr := os.Stat(in.Source)
		local := statErr == nil
		if local {
			path, err := ExpandPath(in.Source)
			if err != nil {
				return nil, nil, err
			}
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return nil, nil, err
			}
			r.HostPath, err = git(ctx, path, "rev-parse", "--show-toplevel")
			if err != nil {
				return nil, nil, err
			}
			raw, err := git(ctx, r.HostPath, "config", "--get", "remote.origin.url")
			if err != nil {
				return nil, nil, fmt.Errorf("repository %s has no origin; set it or use --repo with a URL", r.HostPath)
			}
			r.URL, err = CloneURL(raw)
			if err != nil {
				return nil, nil, err
			}
			current, _ := git(ctx, r.HostPath, "symbolic-ref", "--quiet", "--short", "HEAD")
			if r.Branch == "" {
				r.Branch = current
			}
			if r.Branch == "" {
				return nil, nil, fmt.Errorf("repository %s has detached HEAD; pass --branch", r.HostPath)
			}
			if r.Author == "" {
				name, _ := git(ctx, r.HostPath, "config", "user.name")
				email, _ := git(ctx, r.HostPath, "config", "user.email")
				r.Author = name + " <" + email + ">"
			}
			if current == r.Branch {
				dirty, err := git(ctx, r.HostPath, "status", "--porcelain")
				if err != nil {
					return nil, nil, err
				}
				if dirty != "" {
					needs = append(needs, api.Need{Code: "uncommitted", Message: "local edits will not reach the sandbox; commit and push them or use --ignore-local", Repository: r.Name, Warning: req.IgnoreLocal})
				}
			}
		} else {
			var err error
			r.URL, err = CloneURL(in.Source)
			if err != nil {
				return nil, nil, err
			}
			if r.Branch == "" {
				return nil, nil, fmt.Errorf("a URL needs --branch; select a work branch")
			}
			if r.Author == "" {
				name, _ := git(ctx, ".", "config", "user.name")
				email, _ := git(ctx, ".", "config", "user.email")
				r.Author = name + " <" + email + ">"
			}
		}
		if r.Name == "" {
			u, _ := url.Parse(r.URL)
			r.Name = strings.TrimSuffix(filepath.Base(u.Path), ".git")
		}
		if err := ValidName(r.Name); err != nil {
			return nil, nil, fmt.Errorf("repository directory: %w; use --repo name=source", err)
		}
		if names[r.Name] {
			return nil, nil, fmt.Errorf("duplicate repository directory %s; use --repo name=source", r.Name)
		}
		names[r.Name] = true
		if _, err := git(ctx, ".", "check-ref-format", "--branch", r.Branch); err != nil {
			return nil, nil, fmt.Errorf("invalid branch %q; use a git branch name", r.Branch)
		}
		if _, _, err := AuthorParts(r.Author); err != nil {
			return nil, nil, err
		}
		if local {
			out, err := git(ctx, r.HostPath, "ls-remote", "--symref", r.URL, "HEAD", "refs/heads/"+r.Branch)
			if err != nil {
				return nil, nil, fmt.Errorf("cannot check remote branch for %s; check forge access: %w", r.Name, err)
			}
			remoteTip := ""
			defaultBranch := ""
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
					defaultBranch = strings.TrimPrefix(fields[1], "refs/heads/")
				}
				if len(fields) == 2 && fields[1] == "refs/heads/"+r.Branch {
					remoteTip = fields[0]
				}
			}
			if defaultBranch == "" {
				needs = append(needs, api.Need{Code: "default_branch_unknown", Message: "cannot determine the forge's default branch; check its HEAD configuration", Repository: r.Name})
			} else if defaultBranch == r.Branch {
				needs = append(needs, api.Need{Code: "default_branch", Message: "make a work branch first", Repository: r.Name})
			}
			localTip, localErr := git(ctx, r.HostPath, "rev-parse", "--verify", "refs/heads/"+r.Branch)
			if remoteTip == "" {
				if localErr != nil {
					needs = append(needs, api.Need{Code: "branch_missing", Message: "branch exists neither locally nor on the forge; pass --push to create it from the current HEAD", Repository: r.Name})
				} else {
					needs = append(needs, api.Need{Code: "branch_not_pushed", Message: "push the selected branch first (or pass --push)", Repository: r.Name})
				}
			} else if localErr == nil && localTip != remoteTip {
				if _, err = git(ctx, r.HostPath, "merge-base", "--is-ancestor", localTip, remoteTip); err != nil {
					needs = append(needs, api.Need{Code: "local_commits", Message: "selected local branch differs from the forge; push it or use --ignore-local to use the forge's version", Repository: r.Name, Warning: req.IgnoreLocal})
				}
			}
		}
		repos = append(repos, r)
	}
	return repos, needs, nil
}

func PushRepositories(ctx context.Context, repos []api.Repository) error {
	for _, r := range repos {
		if r.HostPath == "" {
			return fmt.Errorf("--push requires local repositories")
		}
		if _, err := git(ctx, r.HostPath, "rev-parse", "--verify", "refs/heads/"+r.Branch); err != nil {
			// A branch nobody has yet starts on the forge from what the host
			// looks at; the host checkout stays as it is.
			if _, err := git(ctx, r.HostPath, "push", "origin", "HEAD:refs/heads/"+r.Branch); err != nil {
				return err
			}
			continue
		}
		if _, err := git(ctx, r.HostPath, "push", "--set-upstream", "origin", "refs/heads/"+r.Branch+":refs/heads/"+r.Branch); err != nil {
			return err
		}
	}
	return nil
}
