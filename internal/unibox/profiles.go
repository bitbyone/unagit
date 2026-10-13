package unibox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type Cache struct {
	Host string `yaml:"host" json:"host"`
	Env  string `yaml:"env" json:"env"`
}
type GitOptions struct {
	Author   string `yaml:"author" json:"author"`
	CoAuthor *bool  `yaml:"co_author" json:"co_author,omitempty"`
}
type HostAccess struct {
	Env         []string `yaml:"env" json:"env"`
	Caches      []Cache  `yaml:"caches" json:"caches"`
	DenyNetwork []string `yaml:"deny_network" json:"deny_network"`
}
type Profile struct {
	Dockerfile   string `yaml:"dockerfile" json:"dockerfile"`
	Image        string `yaml:"image" json:"image"`
	CPUs         int    `yaml:"cpus" json:"cpus"`
	Memory       string `yaml:"memory" json:"memory"`
	HostAccess   `yaml:",inline"`
	Agents       []string                `yaml:"agents" json:"agents"`
	Start        string                  `yaml:"start" json:"start"`
	Bootstrap    string                  `yaml:"bootstrap" json:"bootstrap"`
	Instructions []string                `yaml:"instructions" json:"instructions"`
	Git          GitOptions              `yaml:"git" json:"git"`
	Name         string                  `yaml:"-" json:"-"`
	Dir          string                  `yaml:"-" json:"-"`
	Root         string                  `yaml:"-" json:"-"`
	Revision     string                  `yaml:"-" json:"-"`
	Files        map[string]SnapshotFile `yaml:"-" json:"-"`
}
type SnapshotFile struct {
	Data []byte `json:"data"`
	Mode uint32 `json:"mode"`
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var envName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
var sensitiveEnv = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSPHRASE|CREDENTIAL|API_KEY|PRIVATE_KEY|SSH_AUTH|GIT_CONFIG|DOCKER_HOST|DOCKER_CONTEXT|LD_|DYLD_)`)

func safeRelative(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.Clean(path) != ".." && !strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator))
}
func inside(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && (r == "." || safeRelative(r))
}

func readInside(root, path string) ([]byte, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if !inside(root, real) {
		return nil, fmt.Errorf("%s leaves the environments repository; move it inside", path)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, fmt.Errorf("%s must be a regular file smaller than 16 MiB", path)
	}
	return os.ReadFile(real)
}

func LoadProfile(root, name string) (Profile, error) {
	p := Profile{Name: name}
	if err := ValidName(name); err != nil {
		return p, err
	}
	if root == "" {
		return p, fmt.Errorf("no environments repository configured; run unibox config set environments <path>")
	}
	abs, err := ExpandPath(root)
	if err != nil {
		return p, err
	}
	p.Root, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return p, err
	}
	p.Dir = filepath.Join(p.Root, name)
	b, err := readInside(p.Root, filepath.Join(p.Dir, "profile.yaml"))
	if err != nil {
		return p, err
	}
	if err = DecodeYAML(b, &p); err != nil {
		return p, fmt.Errorf("profile %s: %w", name, err)
	}
	if (p.Dockerfile == "") == (p.Image == "") {
		return p, fmt.Errorf("profile %s must specify exactly one of dockerfile or image", name)
	}
	if p.CPUs < 0 {
		return p, fmt.Errorf("profile %s: cpus must be positive", name)
	}
	if p.Memory != "" && !regexp.MustCompile(`^[1-9][0-9]*[kKmMgG]?$`).MatchString(p.Memory) {
		return p, fmt.Errorf("profile %s: invalid memory; use e.g. 8g", name)
	}
	if len(p.Agents) == 0 {
		return p, fmt.Errorf("profile %s has no agents; declare the agents installed in its image", name)
	}
	seen := map[string]bool{}
	for _, id := range p.Agents {
		if _, err := FindAgent(id); err != nil {
			return p, err
		}
		if seen[id] {
			return p, fmt.Errorf("duplicate agent %s", id)
		}
		seen[id] = true
	}
	for _, path := range []string{p.Dockerfile, p.Start, p.Bootstrap} {
		if path != "" && !safeRelative(path) {
			return p, fmt.Errorf("profile %s: script and dockerfile paths must stay inside their directory", name)
		}
	}
	if p.Git.Author != "" && p.Git.Author != "user" {
		return p, fmt.Errorf("profile %s: git.author must be user", name)
	}
	for _, key := range p.Env {
		if !envName.MatchString(key) || sensitiveEnv.MatchString(key) || key == "HOME" || key == "PATH" {
			return p, fmt.Errorf("profile %s: environment name %q is reserved or sensitive; configure credentials through the runtime", name, key)
		}
	}
	for i, c := range p.Caches {
		if !envName.MatchString(c.Env) || sensitiveEnv.MatchString(c.Env) || c.Env == "HOME" || c.Env == "PATH" {
			return p, fmt.Errorf("profile %s: invalid cache environment name %q", name, c.Env)
		}
		h, err := ExpandPath(c.Host)
		if err != nil {
			return p, err
		}
		h, err = filepath.EvalSymlinks(h)
		if err != nil {
			return p, fmt.Errorf("cache %s is unavailable; create it or remove it from the profile", c.Host)
		}
		info, err := os.Stat(h)
		if err != nil || !info.IsDir() {
			return p, fmt.Errorf("cache %s must be a directory", h)
		}
		if strings.ContainsAny(h, ":\n\r") {
			return p, fmt.Errorf("cache path contains an unsupported character")
		}
		p.Caches[i].Host = h
	}
	p.Files = map[string]SnapshotFile{}
	err = filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("profile contains symlink %s; use a regular file in its build context", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("profile contains non-regular file %s", path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(p.Dir, path)
		p.Files[filepath.ToSlash(rel)] = SnapshotFile{b, uint32(info.Mode().Perm())}
		return nil
	})
	if err != nil {
		return p, err
	}
	for _, path := range []string{p.Dockerfile, p.Start} {
		if path != "" {
			if _, ok := p.Files[filepath.ToSlash(path)]; !ok {
				return p, fmt.Errorf("profile %s: missing %s", name, path)
			}
		}
	}
	for _, rt := range []string{"sbx", "podman"} {
		entries, _ := os.ReadDir(filepath.Join(p.Dir, rt))
		for _, e := range entries {
			if e.IsDir() || e.Name() != "build.args" {
				return p, fmt.Errorf("unknown runtime file %s/%s; only build.args is supported", rt, e.Name())
			}
		}
	}
	for _, path := range p.Instructions {
		if _, err := readInside(p.Root, filepath.Join(p.Dir, path)); err != nil {
			return p, fmt.Errorf("profile instructions: %w", err)
		}
	}
	buildFiles := make(map[string]SnapshotFile, len(p.Files))
	for name, file := range p.Files {
		if name != "profile.yaml" {
			buildFiles[name] = file
		}
	}
	p.Revision = digest(struct {
		Files             map[string]SnapshotFile
		Dockerfile, Image string
		Agents            []string
	}{buildFiles, p.Dockerfile, p.Image, p.Agents})
	return p, nil
}

func (p Profile) ApprovalKey() string { return digest([]string{p.Root, p.Name}) }
func (p Profile) AccessHash() string  { return digest(p.HostAccess) }
func (p Profile) BuildArgs(runtime string) ([]string, error) {
	args := []string{"AGENTBOX_RUNTIME=" + runtime, "UNIBOX_RUNTIME=" + runtime}
	f, ok := p.Files[runtime+"/build.args"]
	if !ok {
		return args, nil
	}
	for _, line := range strings.Split(string(f.Data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, _, ok := strings.Cut(line, "=")
		if !ok || !envName.MatchString(k) || sensitiveEnv.MatchString(k) || k == "AGENTBOX_RUNTIME" || k == "UNIBOX_RUNTIME" {
			return nil, fmt.Errorf("invalid build argument %q; use NAME=value without credentials", k)
		}
		args = append(args, line)
	}
	return args, nil
}

const BaseInstructions = `# Unibox sandbox

You work in private clones inside a sandbox. Other tasks may share this sandbox;
leave their directories and processes alone. Host workspaces are not mounted.
For each repository, work on its checked-out branch. Commit and push progress
to that branch regularly. Never force-push, delete remote branches, or push a
different branch. Keep instructions and sandbox configuration out of commits.
`

func (p Profile) InstructionText(attached []string) (string, error) {
	parts := []string{BaseInstructions}
	for _, file := range p.Instructions {
		b, err := readInside(p.Root, filepath.Join(p.Dir, file))
		if err != nil {
			return "", err
		}
		parts = append(parts, string(b))
	}
	for _, name := range attached {
		if err := ValidName(name); err != nil {
			return "", err
		}
		b, err := readInside(p.Root, filepath.Join(p.Root, "instructions", name+".md"))
		if err != nil {
			return "", err
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

func ProfileNames(root string) ([]string, error) {
	if root == "" {
		return []string{}, nil
	}
	root, err := ExpandPath(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(root, e.Name(), "profile.yaml")); err == nil {
				names = append(names, e.Name())
			}
		}
	}
	slices.Sort(names)
	return names, nil
}
