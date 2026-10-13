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
	AgentConfig  map[string]string       `yaml:"agent_config" json:"agent_config,omitempty"`
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
var memoryPattern = regexp.MustCompile(`^[1-9][0-9]*[kKmMgG]?$`)
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
	if err = CheckLayout(p.Root); err != nil {
		return p, err
	}
	p.Dir = filepath.Join(p.Root, "sandboxes", name)
	b, err := readInside(p.Root, filepath.Join(p.Dir, "sandbox.yaml"))
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
	if p.Memory != "" && !memoryPattern.MatchString(p.Memory) {
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
	// The profile's root is held to its shape too: the definition, its
	// Dockerfile, start script and instructions.md, hidden runtime
	// directories - and custom files behind an underscore, _data by
	// convention, where the Dockerfile and sandbox.yaml reference them.
	known := map[string]bool{"sandbox.yaml": true, "instructions.md": true}
	for _, ref := range []string{p.Dockerfile, p.Start} {
		if ref != "" && !strings.Contains(ref, "/") {
			known[ref] = true
		}
	}
	shape, err := os.ReadDir(p.Dir)
	if err != nil {
		return p, err
	}
	for _, e := range shape {
		n := e.Name()
		if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") || known[n] {
			continue
		}
		return p, fmt.Errorf("profile %s: unexpected %s; a profile's root holds sandbox.yaml, its Dockerfile, start script and instructions.md - custom files go under _data and are referenced from there", name, n)
	}
	snapshot := func(dir, prefix string) error {
		return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
			rel, _ := filepath.Rel(dir, path)
			p.Files[prefix+filepath.ToSlash(rel)] = SnapshotFile{b, uint32(info.Mode().Perm())}
			return nil
		})
	}
	if err = snapshot(p.Dir, ""); err != nil {
		return p, err
	}
	// _shared is part of every profile's build context, so a Dockerfile can
	// copy one install script from it; its change outdates every image.
	if _, err := os.Stat(filepath.Join(p.Dir, "_shared")); err == nil {
		return p, fmt.Errorf("profile %s has a _shared directory of its own; _shared lives at the repository root", name)
	}
	if info, err := os.Stat(filepath.Join(p.Root, "_shared")); err == nil && info.IsDir() {
		if err = snapshot(filepath.Join(p.Root, "_shared"), "_shared/"); err != nil {
			return p, err
		}
	}
	for _, path := range []string{p.Dockerfile, p.Start} {
		if path != "" {
			if _, ok := p.Files[filepath.ToSlash(path)]; !ok {
				return p, fmt.Errorf("profile %s: missing %s", name, path)
			}
		}
	}
	for _, rt := range []string{"sbx", "podman"} {
		entries, _ := os.ReadDir(filepath.Join(p.Dir, "."+rt))
		for _, e := range entries {
			if e.IsDir() || e.Name() != "build.args" {
				return p, fmt.Errorf("unknown runtime file .%s/%s; only build.args is supported", rt, e.Name())
			}
		}
		if _, err := os.Stat(filepath.Join(p.Dir, rt)); err == nil {
			return p, fmt.Errorf("profile %s: runtime directories are hidden; rename %s to .%s", name, rt, rt)
		}
	}
	for _, entry := range p.Instructions {
		path, err := p.instructionFile(entry)
		if err != nil {
			return p, err
		}
		if _, err := readInside(p.Root, path); err != nil {
			return p, fmt.Errorf("profile instructions: %w", err)
		}
	}
	for id, file := range p.AgentConfig {
		a, err := FindAgent(id)
		if err != nil {
			return p, err
		}
		if !seen[id] {
			return p, fmt.Errorf("profile %s seeds configuration for %s but does not declare it in agents", name, id)
		}
		if a.Config == "" {
			return p, fmt.Errorf("profile %s: agent %s keeps no JSON configuration unibox can merge; remove it from agent_config", name, id)
		}
		b, err := readInside(p.Root, filepath.Join(p.Dir, file))
		if err != nil {
			return p, fmt.Errorf("profile agent_config: %w", err)
		}
		var seed map[string]any
		if err = json.Unmarshal(b, &seed); err != nil {
			return p, fmt.Errorf("profile %s: agent_config %s must be a JSON object: %v", name, file, err)
		}
	}
	p.Revision = digest(struct {
		Files             map[string]SnapshotFile
		Dockerfile, Image string
		Agents            []string
	}{p.BuildFiles(), p.Dockerfile, p.Image, p.Agents})
	return p, nil
}

// BuildFiles is the image's inputs: the profile's files without what only
// unibox reads - the definition, the automatic instructions.md, the agent
// configuration seeds. Those change without the image changing.
func (p Profile) BuildFiles() map[string]SnapshotFile {
	skip := map[string]bool{"sandbox.yaml": true, "instructions.md": true}
	for _, file := range p.AgentConfig {
		skip[filepath.ToSlash(file)] = true
	}
	out := make(map[string]SnapshotFile, len(p.Files))
	for name, file := range p.Files {
		if !skip[name] {
			out[name] = file
		}
	}
	return out
}

func (p Profile) ApprovalKey() string { return digest([]string{p.Root, p.Name}) }
func (p Profile) AccessHash() string  { return digest(p.HostAccess) }
func (p Profile) BuildArgs(runtime string) ([]string, error) {
	args := []string{"AGENTBOX_RUNTIME=" + runtime, "UNIBOX_RUNTIME=" + runtime}
	f, ok := p.Files["."+runtime+"/build.args"]
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

// instructionFile resolves one instructions entry of a profile: a bare name
// is a text of the repository's instructions directory; anything with a path
// or an extension is a markdown file named relative to sandbox.yaml, still
// confined to the repository when read.
func (p Profile) instructionFile(entry string) (string, error) {
	if ValidName(entry) == nil {
		return filepath.Join(p.Root, "instructions", entry+".md"), nil
	}
	if filepath.IsAbs(entry) || !strings.HasSuffix(entry, ".md") {
		return "", fmt.Errorf("instruction %q is neither a name of instructions/ nor a relative .md path", entry)
	}
	return filepath.Join(p.Dir, entry), nil
}

// InstructionText lays the texts together: the profile's entries, its own
// instructions.md when it has one, then the sandbox's attached names - each
// file once. Nothing is fixed in the binary; the environments repository
// owns every word the agent gets.
func (p Profile) InstructionText(attached []string) (string, error) {
	entries := append([]string{}, p.Instructions...)
	if _, ok := p.Files["instructions.md"]; ok {
		entries = append(entries, "instructions.md")
	}
	paths := []string{}
	for _, entry := range entries {
		path, err := p.instructionFile(entry)
		if err != nil {
			return "", err
		}
		paths = append(paths, path)
	}
	for _, name := range attached {
		if err := ValidName(name); err != nil {
			return "", fmt.Errorf("attached instructions are names of the instructions directory: %w", err)
		}
		paths = append(paths, filepath.Join(p.Root, "instructions", name+".md"))
	}
	parts := []string{}
	used := map[string]bool{}
	for _, path := range paths {
		path = filepath.Clean(path)
		if used[path] {
			continue
		}
		used[path] = true
		b, err := readInside(p.Root, path)
		if err != nil {
			return "", err
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

// CheckLayout holds the environments repository to its shape: profiles under
// sandboxes/, attachable markdown under instructions/, anything of the
// user's own behind an underscore - so a misplaced file is named instead of
// quietly ignored.
func CheckLayout(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "."), strings.HasPrefix(name, "_"):
		case name == "README.md" && !e.IsDir():
		case (name == "sandboxes" || name == "instructions") && e.IsDir():
		default:
			return fmt.Errorf("unexpected %s in the environments repository; profiles go under sandboxes/, attachable texts under instructions/, anything else under an underscore directory such as _shared", name)
		}
	}
	profiles, err := os.ReadDir(filepath.Join(root, "sandboxes"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range profiles {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !e.IsDir() {
			return fmt.Errorf("sandboxes/%s is not a profile directory; every entry of sandboxes/ is a directory with a sandbox.yaml", e.Name())
		}
		if _, err := os.Stat(filepath.Join(root, "sandboxes", e.Name(), "sandbox.yaml")); err != nil {
			return fmt.Errorf("sandboxes/%s has no sandbox.yaml; every profile directory needs one", e.Name())
		}
	}
	texts, err := os.ReadDir(filepath.Join(root, "instructions"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range texts {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			return fmt.Errorf("instructions/%s is not a markdown file; the instructions directory holds attachable .md texts alone", e.Name())
		}
		if e.Name() == "instructions.md" {
			return fmt.Errorf("instructions/instructions.md collides with the profiles' automatic instructions.md; give the text a name of its own")
		}
	}
	return nil
}

func ProfileNames(root string) ([]string, error) {
	if root == "" {
		return []string{}, nil
	}
	root, err := ExpandPath(root)
	if err != nil {
		return nil, err
	}
	if err = CheckLayout(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "sandboxes"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(root, "sandboxes", e.Name(), "sandbox.yaml")); err == nil {
				names = append(names, e.Name())
			}
		}
	}
	slices.Sort(names)
	return names, nil
}
