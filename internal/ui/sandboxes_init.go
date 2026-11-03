package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/unibox"
)

// Unibox Init on a repository is a short guide in front of unibox's own
// walk. It says which sbxenv files the repository holds and which of them
// are envs already, lets the person pick, and hands the rest - sbx, the
// values, the tokens, the sandboxes - to unibox init in this terminal. The
// questions stay unibox's: a wrapper over --json would have to ask them
// again, and would drift from the walk the moment unibox learns a new one.
// A repository not cloned is cloned first, by unagit where it keeps the
// repository, or by unibox under its own data directory, which unibox then
// keeps current.

// sbxenvSuffix ends the name of every file unibox makes an env of.
const sbxenvSuffix = ".sbxenv.yaml"

// uniboxInitAction is Unibox Init… for one repository.
func (a *App) uniboxInitAction(pr forge.Project, cloned func() bool) uiAction {
	return uiAction{name: "Unibox Init…", about: "Set unibox up from this repository, your team's sbxenv files: pick the files to make envs of, then unibox init walks you through sbx, your values, the tokens and the sandboxes, in this terminal.",
		rank: 630, aliases: []string{"unibox", "sandbox", "init", "setup", "set up unibox"},
		when: func() bool {
			_, err := a.executable("unibox")
			return err == nil && (cloned() || a.uniboxCloneAddress(pr) != "")
		},
		run: func() { a.uniboxInit(pr) }}
}

// uniboxInit starts the guide: on the clone, or by cloning it first.
func (a *App) uniboxInit(pr forge.Project) {
	if _, err := a.uniboxClient(); err != nil {
		a.flash(err.Error())
		return
	}
	if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		a.chooseSBXEnvFiles(pr.PathWithNamespace, a.projectDir(pr.Instance, pr.PathWithNamespace))
		return
	}
	address := a.uniboxCloneAddress(pr)
	if address == "" {
		a.flash(pr.PathWithNamespace + " has no address to clone from; clone it with C, then try again")
		return
	}
	a.confirmChoices("Unibox Init", pr.PathWithNamespace+" is not cloned. Clone it where unagit keeps it and pick its sbxenv files here, or let unibox clone it under its own data directory, keep it current and ask which files.", nil, []choice{
		{label: "Clone Here", run: func() {
			a.runTaskThen("Cloning "+pr.PathWithNamespace, func(log func(string)) (string, error) {
				return a.newManager(pr.Instance, pr.PathWithNamespace, log).CloneProject(pr)
			}, func(dir string) {
				a.refreshDisk()
				a.chooseSBXEnvFiles(pr.PathWithNamespace, dir)
			})
		}},
		{label: "Let Unibox Clone", run: func() { a.runUniboxInit("Setting up "+pr.PathWithNamespace, address, nil) }},
	})
}

// uniboxCloneAddress is the address unibox clones the repository from, as
// unagit would clone it: unibox clones with the user's own git, so the
// protocol chosen for the server is the one that authenticates.
func (a *App) uniboxCloneAddress(pr forge.Project) string {
	if a.cfg.Instance(pr.Instance) == nil {
		return a.httpsAddress(pr)
	}
	return a.newManager(pr.Instance, pr.PathWithNamespace, nil).RemoteURL(pr)
}

// chooseSBXEnvFiles lists the sbxenv files of the clone, each with the env
// unibox has made of it already; Enter sets up the one under the cursor,
// i the marked or, with none marked, every file.
func (a *App) chooseSBXEnvFiles(name, dir string) {
	files, err := findSBXEnvs(dir)
	if err != nil {
		a.flash("cannot read " + tildePath(dir) + ": " + err.Error())
		return
	}
	if len(files) == 0 {
		a.flash("no " + sbxenvSuffix + " file in " + tildePath(dir) + "; unibox init wants your team's repository of sbxenv files")
		return
	}
	c, _ := a.uniboxClient()
	var envs []unibox.Env
	a.loadThen("Reading unibox's envs", func(func(string)) (string, error) {
		// An unibox that cannot list its envs yet - one never set up - has
		// none; init is what makes them.
		envs, _, _ = c.Envs()
		return "", nil
	}, func(string) {
		a.showSBXEnvFiles(name, dir, files, envs)
	})
}

// showSBXEnvFiles is the picker of chooseSBXEnvFiles, once the envs are
// read.
func (a *App) showSBXEnvFiles(name, dir string, files []string, envs []unibox.Env) {
	var items []pickItem
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		it := pickItem{Label: rel, Sub: "new", SubRole: "sandboxes.problem", Data: f,
			About: "Make an env of " + rel + " and walk through it in this terminal: sbx, the values it asks of you, its tokens, its sandbox."}
		if env := envOfFile(envs, f); env != "" {
			it.Sub, it.SubRole = "env "+env, "sandboxes.running"
			it.About = rel + " is env " + env + " already. Init again says what is in place and offers what is still missing - a value, a token, the sandbox."
		}
		items = append(items, it)
	}
	marks := &pickMarks{}
	initFiles := func(picked []pickItem) {
		var chosen []string
		for _, it := range picked {
			chosen = append(chosen, it.Data.(string))
		}
		a.runUniboxInit("Setting up "+name, dir, chosen)
	}
	a.showPickerWith("Unibox Init · "+name, items, pickerOptions{pack: true, explain: true, marks: marks,
		enterName: "Init This File", enterAbout: "Make an env of the file under the cursor and walk unibox init through it, in this terminal.", enterHint: "init",
		keys: []pickKey{{keys: "i", hint: "init all", name: "Init All Files", marked: true,
			about: "Walk unibox init through the marked files, or every file when none is marked, in this terminal.",
			named: func() (string, string) {
				if marks.count() > 0 {
					return "Init Marked Files", "Walk unibox init through the marked files, in this terminal."
				}
				return "Init All Files", "Walk unibox init through every file, in this terminal."
			},
			run: func(pickItem) {
				if picked := marks.marked(); len(picked) > 0 {
					initFiles(picked)
					return
				}
				initFiles(items)
			}}},
	}, func(it pickItem) { initFiles([]pickItem{it}) })
}

// runUniboxInit walks unibox init through target - a clone or a git
// address - in this terminal, limited to files when some are chosen, and
// then lists what a first sandbox still needs.
func (a *App) runUniboxInit(title, target string, files []string) {
	args := []string{"init"}
	for _, f := range files {
		args = append(args, "--file", f)
	}
	a.uniboxHereThen(title, a.showUniboxSetup, append(args, target)...)
}

// envOfFile is the env unibox has made of the file, or "".
func envOfFile(envs []unibox.Env, file string) string {
	for _, e := range envs {
		if slices.ContainsFunc(e.Files, func(f string) bool { return sameFile(f, file) }) {
			return e.Name
		}
	}
	return ""
}

// sameFile says whether two paths name one file, through links - a clone
// under a linked home is named both ways.
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}

// findSBXEnvs is every sbxenv file under dir, sorted, as unibox init
// finds them: hidden directories - git's, unagit's worktrees - and
// node_modules are passed over, and a tree too big to be a team's
// repository of envs is refused rather than walked for minutes.
func findSBXEnvs(dir string) ([]string, error) {
	var out []string
	seen := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != dir {
				return fs.SkipDir
			}
			return err
		}
		if seen++; seen > sbxenvFindLimit {
			return errTooManyFiles
		}
		if d.IsDir() {
			if path != dir && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), sbxenvSuffix) {
			out = append(out, path)
		}
		return nil
	})
	if errors.Is(err, errTooManyFiles) {
		return nil, fmt.Errorf("it holds more than %d files; unibox init wants your team's repository of sbxenv files", sbxenvFindLimit)
	}
	slices.Sort(out)
	return out, err
}

// sbxenvFindLimit is unibox's own limit on the files init looks through.
const sbxenvFindLimit = 50000

var errTooManyFiles = errors.New("too many files")
