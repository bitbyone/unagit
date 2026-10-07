package ui

import (
	"encoding/json"
	"fmt"
	"github.com/tobola/unagit/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// The theme in use is chosen in Settings › Theme and kept in the
// configuration by name. Choosing one puts it on at once: the widgets are made
// again in its colours, and the screen comes back where it was.

// chooseTheme reads the themes there are and puts on the one the
// configuration names. One that cannot be found, or cannot be read, leaves
// the default on and is said once the interface is up.
func (a *App) chooseTheme() {
	a.themes = loadThemes(a.cfg.ThemesDir())
	want := a.cfg.Theme
	if want == "" {
		want = defaultThemeName
	}
	t, ok := a.themes.byName[want]
	if !ok {
		a.themeProblem = fmt.Sprintf("there is no theme %q - see Settings › Theme; the default is on", want)
		t = a.themes.byName[defaultThemeName]
	}
	on, why := useNerdFont(a.cfg.NerdFont)
	a.nerdWhy = why
	// The default is already on unless another application of this process
	// put on another; putting it on again would only write what is there.
	if t.Name != theme.Name || t.file != "" || on != nerdFont || a.cfg.TerminalBackground != terminalBackground {
		nerdFont, terminalBackground = on, a.cfg.TerminalBackground
		setTheme(t)
	}
	a.themeFile.Store(t.file)
}

// cycleNerdFont steps the Nerd Font icons from telling by the terminal, to
// on, to off, and draws everything again with the glyphs that follow.
func (a *App) cycleNerdFont() {
	next := map[string]string{"": config.NerdFontOn, config.NerdFontOn: config.NerdFontOff, config.NerdFontOff: ""}[a.cfg.NerdFont]
	a.cfg.NerdFont = next
	if err := a.cfg.Save(); err != nil {
		a.errorf("cannot save the config: %v", err)
		return
	}
	on, why := useNerdFont(next)
	a.nerdWhy = why
	if on != nerdFont {
		nerdFont = on
		setTheme(theme)
		a.rebuildInterface()
	} else if a.settings != nil {
		a.settings.fillThemes()
	}
	a.done("Nerd Font icons: " + why)
}

// toggleTerminalBackground leaves the terminal's own background under every
// theme, or gives the theme its background back, and draws everything
// again.
func (a *App) toggleTerminalBackground() {
	a.cfg.TerminalBackground = !a.cfg.TerminalBackground
	if err := a.cfg.Save(); err != nil {
		a.errorf("cannot save the config: %v", err)
		return
	}
	terminalBackground = a.cfg.TerminalBackground
	setTheme(theme)
	a.rebuildInterface()
	if terminalBackground {
		a.done("Background: the terminal's own, the theme's colours on it")
		return
	}
	a.done("Background: the theme's")
}

// backgroundWords says whose background is under unagit.
func backgroundWords(terminal bool) string {
	if terminal {
		return "the terminal's own"
	}
	return "the theme's"
}

// switchTheme puts a theme on, remembers it, and draws everything again in
// it.
func (a *App) switchTheme(name string) {
	t, ok := a.themes.byName[name]
	if !ok {
		a.flash(fmt.Sprintf("there is no theme %q", name))
		return
	}
	a.cfg.Theme = name
	if name == defaultThemeName {
		a.cfg.Theme = ""
	}
	if err := a.cfg.Save(); err != nil {
		a.errorf("cannot save the config: %v", err)
		return
	}
	setTheme(t)
	a.themeFile.Store(t.file)
	a.rebuildInterface()
	a.done("Theme: " + name)
}

// rebuildInterface makes the main screens again - after a theme was put on -
// and comes back to the tab and the section that were in front.
func (a *App) rebuildInterface() { a.remakeInterface(a.refreshDisk) }

// repaintInterface is rebuildInterface with what was last read from disk:
// a theme tried for a moment, as the cursor passes it, need not read it
// all again.
func (a *App) repaintInterface() {
	a.remakeInterface(func() {
		a.reloadWorktreeView()
		if a.worktreesPane != nil && a.worktreesPane.reload != nil {
			a.worktreesPane.reload()
		}
	})
}

func (a *App) remakeInterface(disk func()) {
	tab := a.currentTab()
	section, inside := sectionGeneral, false
	if a.settings != nil {
		section, inside = a.settings.current, a.settings.contentFocused
	}
	a.tv.SetRoot(a.buildInterface(), true)
	disk()
	a.projectsPane.reload()
	a.mrsPane.reload()
	a.settings.reload()
	a.switchTab(tab)
	if tab == pageSettings {
		a.settings.selectSection(section)
		if inside {
			a.settings.focusContent()
		}
	}
}

// themeWatchInterval is how often the file of the theme in use is looked at,
// unless the App says otherwise (App.themeWatchEvery; tests make it shorter).
const themeWatchInterval = 500 * time.Millisecond

// watchTheme follows the file of the theme in use, when it is the user's: a
// save in the editor puts it on again, so colours can be tuned by hand with
// unagit open beside. It only looks at the file's time, until stop closes.
func (a *App) watchTheme(stop <-chan struct{}) {
	stat := a.themeStat
	if stat == nil {
		stat = os.Stat
	}
	every := a.themeWatchEvery
	if every == 0 {
		every = themeWatchInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	var path string
	var seen time.Time
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		now, _ := a.themeFile.Load().(string)
		if now == "" {
			path = ""
			continue
		}
		fi, err := stat(now)
		if err != nil {
			continue
		}
		if now != path {
			path, seen = now, fi.ModTime()
			continue
		}
		if fi.ModTime().Equal(seen) {
			continue
		}
		seen = fi.ModTime()
		a.tv.QueueUpdateDraw(a.reloadThemes)
	}
}

// reloadThemes reads the themes again - on r in Settings › Theme, or when the
// file of the one in use was saved - and puts the one in use on again when
// its file changed. A file that no longer reads keeps what was on and says
// why; a dialog in front is not torn down, the theme waits until it is gone.
func (a *App) reloadThemes() {
	a.themes = loadThemes(a.cfg.ThemesDir())
	if a.settings != nil {
		a.settings.fillThemes()
	}
	t, ok := a.themes.byName[theme.Name]
	if !ok {
		// Say what is wrong with its own file, not with every other.
		why := a.themes.problems
		for _, p := range a.themes.problems {
			if theme.file != "" && strings.HasPrefix(p, filepath.Base(theme.file)+":") {
				why = []string{p}
			}
		}
		a.flash(fmt.Sprintf("%s cannot be used as it is now, so the last of it stays on: %s",
			theme.Name, strings.Join(why, "; ")))
		return
	}
	if reflect.DeepEqual(t, theme) {
		return
	}
	if a.modalOpen() {
		time.AfterFunc(time.Second, func() { a.tv.QueueUpdateDraw(a.reloadThemes) })
		return
	}
	setTheme(t)
	a.themeFile.Store(t.file)
	a.rebuildInterface()
	a.note("Theme " + t.Name + " read again")
}

// themeFileName is what a forked theme may be called: it is a file name too.
var themeFileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// forkTheme writes a theme out as a file of the user's, under a name of its
// own, with every colour and glyph spelled out rather than extended - the
// whole of it to tune - and puts it on.
func (a *App) forkTheme(from, name string) error {
	src, ok := a.themes.byName[from]
	if !ok {
		return fmt.Errorf("there is no theme %q", from)
	}
	if !themeFileName.MatchString(name) {
		return fmt.Errorf("name it with small letters, digits, dots and dashes")
	}
	if _, taken := a.themes.byName[name]; taken {
		return fmt.Errorf("there is a theme %s already - name the fork otherwise", name)
	}
	path := filepath.Join(a.cfg.ThemesDir(), name+".json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists already - name the fork otherwise", tildePath(path))
	}
	fork := src
	fork.Name, fork.Extends = name, ""
	fork.Description = "Fork of " + from
	if src.Description != "" {
		fork.Description += " - " + src.Description
	}
	data, err := json.MarshalIndent(fork, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.cfg.ThemesDir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	a.themes = loadThemes(a.cfg.ThemesDir())
	a.switchTheme(name)
	a.done(fmt.Sprintf("Forked %s into %s - edit it, and unagit follows each save", from, tildePath(path)))
	return nil
}

// showForkForm asks what to call a fork of a theme.
func (s *settingsView) showForkForm(from string) {
	a := s.app
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Name", from+"-mine", 0, nil, nil)
	form.AddTextView("", "Written to "+tview.Escape(tildePath(a.cfg.ThemesDir()))+"/<name>.json with every colour and glyph in it, and put on.", 0, 2, true, false)
	fork := func() {
		name := strings.TrimSpace(form.GetFormItemByLabel("Name").(*tview.InputField).GetText())
		a.closeModal(pageForm)
		if err := a.forkTheme(from, name); err != nil {
			a.flash(err.Error())
		}
	}
	form.AddButton("Fork", fork)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModal("Fork theme "+from, form, 9)
}
