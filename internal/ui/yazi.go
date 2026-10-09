package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

func (a *App) yaziOn() bool {
	if a.cfg.Integrations.Yazi != nil && !*a.cfg.Integrations.Yazi {
		return false
	}
	_, err := a.executable("yazi")
	return err == nil
}

func (a *App) yaziBinary() (string, bool) {
	if a.cfg.Integrations.Yazi != nil && !*a.cfg.Integrations.Yazi {
		a.flash("yazi is disabled; enable it in Settings › Integrations")
		return "", false
	}
	bin, err := a.executable("yazi")
	if err != nil {
		a.flash("yazi is not installed; install it and add it to PATH")
		return "", false
	}
	return bin, true
}

func (a *App) browseFilesAction(run func()) uiAction {
	return uiAction{name: "Browse Files", about: "Browse in Yazi; choose a file to open it in your default editor.", rank: 17, when: a.yaziOn, run: run}
}

func (a *App) browseProject(pr forge.Project) {
	if _, ok := a.yaziBinary(); !ok {
		return
	}
	what := session.Record{Instance: pr.Instance, Server: a.instanceLabel(pr.Instance), Project: pr.PathWithNamespace, Mode: session.ModeRepository}
	if dir := a.projectDir(pr.Instance, pr.PathWithNamespace); workspace.Exists(dir) {
		a.browseNow(dir, what)
		return
	}
	a.runTaskThen("Cloning "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		return a.newManager(pr.Instance, pr.PathWithNamespace, log).CloneProject(pr)
	}, func(dir string) { a.browseNow(dir, what) })
}

func (a *App) browseMR(mr forge.MergeRequest) {
	if _, ok := a.yaziBinary(); !ok {
		return
	}
	project := a.mrProject(mr)
	path := project.PathWithNamespace
	review := a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	branch := a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	switch {
	case workspace.Exists(review):
		a.browseNow(review, a.sessionOf(mr, path, session.ModeReview))
	case workspace.Exists(branch):
		a.browseNow(branch, a.sessionOf(mr, path, session.ModeBranch))
	default:
		client, integrate := a.client(mr.Instance), a.cfg.Integrations.Incomm
		a.runTaskThen(fmt.Sprintf("Preparing %s !%d for Yazi", path, mr.IID), func(log func(string)) (string, error) {
			return a.prepareReview(mr, project, client, "", integrate, log)
		}, func(dir string) { a.browseNow(dir, a.sessionOf(mr, path, session.ModeReview)) })
	}
}

func (a *App) browseWorktree(r worktreeRow) {
	mode := session.ModeBranch
	if r.grouped() {
		mode = session.ModeGroup
	}
	a.browseNow(r.Dir, session.Record{Instance: r.Instance, Server: a.instanceLabel(r.Instance), Project: r.Path, Title: r.Branch, Mode: mode})
}

func (a *App) browseNow(dir string, what session.Record) {
	bin, ok := a.yaziBinary()
	if !ok {
		return
	}
	go func() {
		// Yazi and editors share one terminal, including while a launcher returns.
		a.editorMu.Lock()
		file, err := a.runYazi(bin, dir, what)
		a.editorMu.Unlock()
		if err != nil {
			a.tv.QueueUpdateDraw(func() { a.errorf("yazi: %v", err) })
			return
		}
		if file != "" {
			a.openEditorAt(dir, what, nil, file)
			return
		}
		a.tv.QueueUpdateDraw(func() { a.done("browsed " + dir) })
	}()
}

func (a *App) runYazi(bin, dir string, what session.Record) (string, error) {
	tmp, err := os.MkdirTemp("", "unagit-yazi-")
	if err != nil {
		return "", fmt.Errorf("cannot prepare the file browser: %w", err)
	}
	defer os.RemoveAll(tmp)
	cwd, chosen := filepath.Join(tmp, "cwd"), filepath.Join(tmp, "chosen")
	cmd := exec.Command(bin, "--cwd-file", cwd, "--chooser-file", chosen, dir)
	cmd.Dir = dir
	what.Editor = "yazi"
	what.Branch, _ = workspace.WorktreeHead(dir)
	a.tv.QueueUpdateDraw(func() { a.closeModal(pageTask) })
	if err := a.runInTerminal(dir, what, cmd); err != nil {
		return "", err
	}
	last, err := yaziResult(cwd)
	if err != nil {
		return "", err
	}
	if last != "" && !sameDirectory(last, dir) {
		a.zoxideAdd(last)
	}
	file, err := yaziResult(chosen)
	if err != nil {
		return "", err
	}
	if file != "" && !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	return file, nil
}

// Yazi writes one path per line. Keep spaces in names, and open the first
// selection when the user marked more than one file.
func yaziResult(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the file browser's result: %w", err)
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return line, nil
}

func (a *App) yaziInit() string {
	if a.yaziInitPath != nil {
		return a.yaziInitPath()
	}
	if dir := os.Getenv("YAZI_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "init.lua")
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "yazi", "init.lua")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "yazi", "init.lua")
}

var yaziUpdateDB = regexp.MustCompile(`(?s)require\s*\(\s*["']zoxide["']\s*\)\s*:\s*setup\s*\(?\s*\{[^}]*\bupdate_db\s*=\s*true\b`)

// This is a hint from the source, never an attempt to execute a user's Lua.
func (a *App) yaziFound() string {
	data, _ := os.ReadFile(a.yaziInit())
	source := strings.Split(string(data), "\n")
	for i, line := range source {
		source[i], _, _ = strings.Cut(line, "--")
	}
	if yaziUpdateDB.MatchString(strings.Join(source, "\n")) {
		return "Yazi zoxide update_db found in init.lua"
	}
	return "Yazi zoxide update_db not detected in init.lua"
}
