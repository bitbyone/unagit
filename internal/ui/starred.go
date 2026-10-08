package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/md"
	"github.com/tobola/unagit/internal/workspace"
)

// The repositories starred on GitHub are a list of their own (: in
// Repositories, View Starred Repositories…): each can be read - its README,
// drawn from its markdown - opened in the browser, or cloned. One cloned
// from there joins Repositories like any other, though it belongs to none
// of the groups chosen, and wears a badge saying it came from the stars
// (index-starred.json keeps it there across refreshes).

// hasGitHub reports whether a GitHub account with a token is configured.
func (a *App) hasGitHub() bool {
	for _, inst := range a.cfg.Instances {
		if inst.IsGitHub() && a.client(inst.ID) != nil {
			return true
		}
	}
	return false
}

// loadStarred reads the starred repositories that were cloned.
func (a *App) loadStarred() {
	if s, err := index.Load[[]forge.Project](a.cfg.IndexPath("starred")); err == nil {
		a.starred = s
	}
}

// mergeStarred puts the starred repositories that were cloned into the list
// of repositories, unless a group of the user's has them already.
func (a *App) mergeStarred() {
	have := map[projectKey]bool{}
	for _, p := range a.projects {
		have[projectKey{p.Instance, p.PathWithNamespace}] = true
	}
	for _, p := range a.starred {
		if k := (projectKey{p.Instance, p.PathWithNamespace}); !have[k] {
			a.projects = append(a.projects, p)
			have[k] = true
		}
	}
}

// keepStarred remembers a starred repository that was cloned.
func (a *App) keepStarred(pr forge.Project) {
	for _, p := range a.starred {
		if p.Instance == pr.Instance && p.PathWithNamespace == pr.PathWithNamespace {
			return
		}
	}
	pr.Starred = true
	a.starred = append(a.starred, pr)
	if err := index.Save(a.cfg.IndexPath("starred"), a.starred); err != nil {
		a.errorf("cannot remember %s: %v", pr.PathWithNamespace, err)
	}
	a.reindexProjects()
}

// showStarred reads the starred repositories of every GitHub account and
// lists them.
func (a *App) showStarred() {
	if !a.hasGitHub() {
		a.note("no GitHub account with a token - add one in [4] Settings")
		return
	}
	var starred []forge.Project
	a.loadThen("Reading your starred repositories", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, inst := range a.cfg.Instances {
			client := a.client(inst.ID)
			if !inst.IsGitHub() || client == nil {
				continue
			}
			got, err := client.StarredProjects(ctx)
			if err != nil {
				return "", fmt.Errorf("%s: %w", inst.Label(), err)
			}
			for i := range got {
				got[i].Instance = inst.ID
			}
			starred = append(starred, got...)
		}
		return "", nil
	}, func(string) { a.listStarred(starred, 0) })
}

// listStarred lists starred repositories, the cursor on start.
func (a *App) listStarred(starred []forge.Project, start int) {
	if len(starred) == 0 {
		a.note("nothing is starred")
		return
	}
	items := make([]pickItem, len(starred))
	for i, p := range starred {
		items[i] = pickItem{About: esc(p.Description), Data: i}
	}
	label := func(items []pickItem, width int) { a.labelStarred(items, starred, width) }
	label(items, logRowWidth(a.screenWidth()))
	at := func(it pickItem) forge.Project { return starred[it.Data.(int)] }
	again := func(it pickItem) func() { return func() { a.listStarred(starred, it.Data.(int)) } }
	opts := pickerOptions{start: start, wide: true, explain: true, enterHint: "readme", relabel: label,
		enterName: "View README", enterAbout: "Read the repository's README, drawn from its markdown.",
		keys: []pickKey{
			{keys: "C", hint: "clone", name: "Clone", about: "Clone it under its root; it then joins Repositories, with a badge saying it is starred.",
				run: func(it pickItem) { a.cloneStarred(at(it), again(it)) }},
			a.browserKey("w", "browser", "Open in Browser", "The repository's page on the forge; the list stays open.",
				func(it pickItem) string { return at(it).WebURL }),
		}}
	a.showPickerWith(fmt.Sprintf("Starred repositories · %d", len(starred)), items, opts, func(it pickItem) {
		a.showReadme(at(it), again(it))
	})
}

// labelStarred writes the rows of the starred list for a row of width
// cells, in columns without headings: the name, as much of the description
// as the row leaves, and at the right the language, the stars and how long
// ago it moved, each column as wide as its longest.
func (a *App) labelStarred(items []pickItem, starred []forge.Project, width int) {
	nameW, langW, starsW, ageW := 0, 0, 0, 0
	for _, p := range starred {
		nameW = max(nameW, iconWidth(a.forgeIcon(p.Instance))+len([]rune(p.PathWithNamespace)))
		langW = max(langW, len([]rune(p.Language)))
		starsW = max(starsW, len(fmt.Sprint(p.Stars)))
		ageW = max(ageW, len(humanAge(p.LastActivityAt)))
	}
	nameW = min(nameW, 40)
	// The mark, the name and three gaps, then what floats at the right.
	right := langW + 2 + len([]rune(glyphStarred)) + 1 + starsW + 2 + ageW
	descW := max(0, width-2-nameW-2-right-2)
	for i, p := range starred {
		mark := " "
		if a.diskOf(p.Instance, p.PathWithNamespace).Cloned {
			mark = glyphDiskBranch
		}
		desc := ""
		if descW >= 8 {
			desc = trim(strings.Join(strings.Fields(p.Description), " "), descW)
		}
		items[i].Label = fmt.Sprintf("%s %s  %s  %s  %s%s %*d%s  %s",
			esc(mark), rowText([]field{{icon: a.forgeIcon(p.Instance), text: p.PathWithNamespace, width: nameW, colour: colText}}),
			tag(role("starred.description"))+esc(padTo(desc, descW))+tagEnd,
			tag(role("starred.language"))+esc(padTo(p.Language, langW))+tagEnd,
			tag(role("starred.stars")), glyphStarred, starsW, p.Stars, tagEnd,
			tag(role("starred.activity"))+esc(humanAge(p.LastActivityAt))+tagEnd)
		items[i].Sub = ""
	}
}

// padTo pads text with spaces to width cells.
func padTo(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-len([]rune(text))))
}

// cloneStarred clones a starred repository, keeps it in Repositories, and
// comes back to the list.
func (a *App) cloneStarred(pr forge.Project, back func()) {
	if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		a.keepStarred(pr)
		a.note(pr.PathWithNamespace + " is already cloned, at " + tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace)))
		back()
		return
	}
	a.runTaskThen("Cloning "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		_, err := a.newManager(pr.Instance, pr.PathWithNamespace, log).CloneProject(pr)
		return "", err
	}, func(string) {
		a.keepStarred(pr)
		a.refreshDisk()
		a.projectsPane.reload()
		back()
		a.done(pr.PathWithNamespace + " is cloned and in Repositories")
	})
}

// showReadme reads a repository's README and shows it rendered; e opens it
// in the editor, w the repository in the browser, Esc goes back.
func (a *App) showReadme(pr forge.Project, back func()) {
	client := a.client(pr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(pr.Instance))
		return
	}
	var text string
	a.loadThen("Reading the README of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		text, err = client.Readme(ctx, pr)
		return "", err
	}, func(string) {
		if strings.TrimSpace(text) == "" {
			a.note(pr.PathWithNamespace + " has no README")
			back()
			return
		}
		view := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetScrollable(true)
		view.SetText(md.RenderTheme(text, mdTheme))
		box(view.Box, "README · "+esc(pr.PathWithNamespace))
		hintPanel(view.Box, func() string {
			return "O open in the editor · w browser"
		}, 0, 0, 1, 1)
		view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if halfPage(view, ev) {
				return nil
			}
			switch {
			case ev.Key() == tcell.KeyEsc, ev.Rune() == 'q', ev.Rune() == 'h':
				a.closeModal(pageCommit)
				back()
				return nil
			case ev.Rune() == 'O':
				a.openReadme(pr, text)
				return nil
			case ev.Rune() == 'w':
				a.openWeb(pr.WebURL)
				return nil
			}
			return ev
		})
		a.pages.AddPage(pageCommit, modalPct(view, 80, 85), true, true)
		a.tv.SetFocus(view)
	})
}

// openReadme opens a README in the favourite editor: the clone's own when
// the repository is cloned, else a copy kept beside the configuration.
func (a *App) openReadme(pr forge.Project, text string) {
	path := ""
	if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		dir := a.projectDir(pr.Instance, pr.PathWithNamespace)
		for _, name := range []string{"README.md", "readme.md", "README.markdown", "README"} {
			if workspace.Exists(filepath.Join(dir, name)) {
				path = filepath.Join(dir, name)
				break
			}
		}
	}
	if path == "" {
		path = filepath.Join(a.cfg.Dir(), "readmes", pr.Instance, filepath.FromSlash(pr.PathWithNamespace), "README.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			a.errorf("cannot keep the README: %v", err)
			return
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			a.errorf("cannot keep the README: %v", err)
			return
		}
	}
	a.withEditor(false, func(ed *editors.Editor) { go a.openFile(path, ed) })
}

// openFile opens one file in an editor - nil for the favourite - with no
// session recorded: a file is not a place to cd into.
func (a *App) openFile(path string, ed *editors.Editor) {
	if ed == nil {
		fav, ok := editors.Favourite(a.editorsOn(), a.cfg.FavouriteEditor)
		if !ok {
			a.tv.QueueUpdateDraw(func() {
				a.withEditor(true, func(chosen *editors.Editor) { go a.openFile(path, chosen) })
			})
			return
		}
		ed = &fav
	}
	cmd, err := ed.Command(path)
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%s did not start: %v", ed.Name, err) })
		return
	}
	if !ed.Terminal {
		err = cmd.Start()
	} else {
		a.tv.Suspend(func() {
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			err = cmd.Run()
		})
	}
	a.tv.QueueUpdateDraw(func() {
		if err != nil {
			a.errorf("%s: %v", ed.Name, err)
			return
		}
		a.done("opened " + tildePath(path))
	})
}

// starredBadge is the badge of a repository cloned from the stars: a star
// and the forge it is starred on, as short as the room asks.
func (a *App) starredBadge(pr forge.Project, room int, style, behind string) (string, int) {
	forgeName := "GitHub"
	if inst := a.cfg.Instance(pr.Instance); inst != nil && !inst.IsGitHub() {
		forgeName = "GitLab"
	}
	c := tagColour{name: "starred", ink: role("badge.starred.ink").String(), fill: role("badge.starred.fill").String()}
	for _, text := range []string{glyphStarred + " " + forgeName, glyphStarred} {
		if markup, w := pillOf(text, c, style, behind); w <= room {
			return markup, w
		}
	}
	return "", 0
}
