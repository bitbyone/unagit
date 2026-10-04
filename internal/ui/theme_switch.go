package ui

import (
	"fmt"
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
	// The default is already on unless another application of this process
	// put on another; putting it on again would only write what is there.
	if t.Name != theme.Name || t.file != "" {
		setTheme(t)
	}
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
	a.rebuildInterface()
	a.done("Theme: " + name)
}

// rebuildInterface makes the main screens again - after a theme was put on -
// and comes back to the tab and the section that were in front.
func (a *App) rebuildInterface() {
	tab := a.currentTab()
	section, inside := sectionGeneral, false
	if a.settings != nil {
		section, inside = a.settings.current, a.settings.contentFocused
	}
	a.tv.SetRoot(a.buildInterface(), true)
	a.refreshDisk()
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
