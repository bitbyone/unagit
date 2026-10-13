package unibox

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:scaffold
var scaffoldFS embed.FS

// InitReport says what Init wrote and what it found already there and kept.
type InitReport struct {
	Root          string
	Written, Kept []string
}

// Init lays the environments repository's mandatory structure into root and
// adds one profile: the shared runner and its installation scripts, the
// unibox instructions text, and the profile's own files with the chosen
// agents installed. It writes only what is missing - a file already there is
// kept word for word - so it is safe on a repository in use.
func Init(root, name string, agents []string, cpus int, memory string) (InitReport, error) {
	report := InitReport{Root: root}
	if err := ValidName(name); err != nil {
		return report, err
	}
	if len(agents) == 0 {
		return report, fmt.Errorf("choose at least one agent for the profile")
	}
	for _, id := range agents {
		if _, err := FindAgent(id); err != nil {
			return report, err
		}
	}
	if memory != "" && !memoryPattern.MatchString(memory) {
		return report, fmt.Errorf("invalid memory %q; use e.g. 8g", memory)
	}
	place := func(rel string, data []byte, mode os.FileMode) error {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(path); err == nil {
			report.Kept = append(report.Kept, rel)
			return nil
		}
		if err := AtomicWrite(path, data, mode); err != nil {
			return err
		}
		report.Written = append(report.Written, rel)
		return nil
	}
	err := fs.WalkDir(scaffoldFS, "scaffold", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(path, "scaffold/")
		dest := rel
		if rel == "install" || strings.HasPrefix(rel, "installations/") {
			dest = "_shared/" + rel
		}
		data, err := scaffoldFS.ReadFile(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if rel == "install" || strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		return place(dest, data, mode)
	})
	if err != nil {
		return report, err
	}
	dir := "sandboxes/" + name
	lines := []string{"base", "tools/node"}
	declared := []string{}
	for _, id := range agents {
		lines = append(lines, "agents/"+id)
		declared = append(declared, id)
	}
	yaml := fmt.Sprintf(`# %s: describe the environment here.
dockerfile: Dockerfile
cpus: %d
memory: %s
agents: [%s]
instructions: [unibox]
`, name, cpus, memory, strings.Join(declared, ", "))
	dockerfile := fmt.Sprintf(`# The image is composed by _shared/install: list the installation scripts,
# one per line. The sbx build passes BASE=docker/sandbox-templates:shell
# (.sbx/build.args); the default keeps this buildable by hand.
ARG BASE=ubuntu:24.04
FROM ${BASE}

ARG UNIBOX_RUNTIME
USER root

RUN --mount=type=bind,source=_shared,target=/unibox /unibox/install <<EOF
%s
EOF

# The shell template ships an agent user; a plain ubuntu base needs one made.
RUN id agent >/dev/null 2>&1 || useradd -m -s /bin/bash agent
USER agent
WORKDIR /home/agent
`, strings.Join(lines, "\n"))
	instructions := fmt.Sprintf("# %s\n\nWhat the agent should know when it works in this profile.\n", name)
	for _, f := range []struct {
		rel  string
		data string
		mode os.FileMode
	}{
		{dir + "/sandbox.yaml", yaml, 0o644},
		{dir + "/Dockerfile", dockerfile, 0o644},
		{dir + "/instructions.md", instructions, 0o644},
		{dir + "/.sbx/build.args", "BASE=docker/sandbox-templates:shell\n", 0o644},
	} {
		if err := place(f.rel, []byte(f.data), f.mode); err != nil {
			return report, err
		}
	}
	if _, err := LoadProfile(root, name); err != nil {
		return report, fmt.Errorf("the repository did not come out loadable: %w", err)
	}
	return report, nil
}
