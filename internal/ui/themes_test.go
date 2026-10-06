package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// The theme is the process's, so every test that puts one on is serial and
// puts the default back when it is done.

func restoreDefaultTheme(t *testing.T) {
	t.Cleanup(func() { setTheme(loadThemes("").byName[defaultThemeName]) })
}

// TestTheDefaultThemeIsTodaysLook: the default names every colour and glyph
// itself - nothing is left to a zero value - and is the palette unagit had
// before themes.
func TestTheDefaultThemeIsTodaysLook(t *testing.T) {
	t.Parallel()
	d := loadThemes("").byName[defaultThemeName]
	for key, value := range d.colours() {
		if value == "" {
			t.Errorf("the default theme leaves %s out", key)
		}
	}
	for key, value := range d.glyphs() {
		if value == "" {
			t.Errorf("the default theme leaves %s out", key)
		}
	}
	if len(d.Tags) != len(tagPalette) {
		t.Errorf("the default theme paints %d tag colours of %d", len(d.Tags), len(tagPalette))
	}
	for key, want := range map[string]tcell.Color{
		"background": tcell.ColorDefault, "text.normal": tcell.Color252, "text.muted": tcell.Color244,
		"text.dim": tcell.Color240, "text.accent": tcell.Color109, "state.good": tcell.Color108,
		"state.warning": tcell.Color179, "state.bad": tcell.Color167, "surface.field": tcell.Color236,
		"selection.background": tcell.Color238, "selection.text": tcell.Color231,
	} {
		if got := colour(d.colours()[key]); got != want {
			t.Errorf("%s = %v, want %v as before themes", key, got, want)
		}
	}
}

// TestUserThemesExtendAndSayWhatIsWrong: a theme of the user's names what it
// changes and takes the rest from what it extends; one that cannot be used is
// named with the key that is wrong, and the others still load.
func TestUserThemesExtendAndSayWhatIsWrong(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) {
		must(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	write("mine.json", `{"description": "Mine", "extends": "gruvbox-dark", "text": {"accent": "#ff0000"}, "glyphs": {"favourite": "♥"}}`)
	write("broken.json", `{"name": "broken", "state": {"bad": "reddish"}, "glyphs": {"check": "ok"}}`)
	write("loop.json", `{"name": "loop", "extends": "loop"}`)
	write("unreadable.json", `{"name": `)

	set := loadThemes(dir)
	mine, ok := set.byName["mine"]
	if !ok {
		t.Fatalf("mine.json was not loaded; problems: %v", set.problems)
	}
	gruvbox := set.byName["gruvbox-dark"]
	if mine.Text.Accent != "#ff0000" || mine.Glyphs.Favourite != "♥" {
		t.Errorf("what mine changes is lost: %+v", mine.Text)
	}
	if mine.Background != gruvbox.Background || mine.Text.Normal != gruvbox.Text.Normal {
		t.Error("what mine leaves out is not gruvbox's")
	}
	if mine.Glyphs.Check != "✓" {
		t.Errorf("what neither changes is not the default's: %q", mine.Glyphs.Check)
	}
	if mine.file == "" {
		t.Error("mine does not say where it came from")
	}
	problems := strings.Join(set.problems, "\n")
	for _, want := range []string{"broken.json", "state.bad", "glyphs.check", "loop.json", "unreadable.json"} {
		if !strings.Contains(problems, want) {
			t.Errorf("the problems do not name %s:\n%s", want, problems)
		}
	}
	for _, name := range []string{"broken", "loop"} {
		if _, ok := set.byName[name]; ok {
			t.Errorf("%s is offered though it cannot be used", name)
		}
	}
	if set.names[0] != defaultThemeName {
		t.Errorf("the default is not listed first: %v", set.names)
	}
}

// TestEveryThemeIsLegible walks the dialogs and Settings in every theme unagit
// comes with: nothing in its own background, nothing at the terminal's ink on
// a chosen colour, and, in a theme with a background of its own, nothing left
// on the terminal's.
func TestEveryThemeIsLegible(t *testing.T) {
	restoreDefaultTheme(t)
	for _, name := range loadThemes("").names {
		t.Run(name, func(t *testing.T) {
			a, sc := newThemedApp(t, name)
			walkDialogs(t, a, sc)
			b, sc2 := newThemedApp(t, name)
			walkSettings(t, b, sc2)
			c, sc3 := newThemedApp(t, name)
			walkDrawnByHand(t, c, sc3)
			for _, firstRun := range []bool{false, true} {
				d, sc4 := newLockedApp(t, name, firstRun)
				waitFor(t, d, sc4, "Passphrase")
				assertLegible(t, d, sc4, "the unlock dialog")
			}
		})
	}
}

// walkDrawnByHand looks at what is drawn cell by cell rather than by a
// widget - a grouped worktree's view of blocks - and at the dialogs of a
// merge request's end.
func walkDrawnByHand(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Merge acme/gateway !7")
	assertLegible(t, a, sc, "the merge form")
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Merge acme/gateway !7")
	typeRunes(sc, "a")
	waitFor(t, a, sc, "Mike Moe")
	assertLegible(t, a, sc, "the reviewers")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Mike Moe")
	typeRunes(sc, "1")
	waitFor(t, a, sc, "acme/billing")
	lookGroup(t, a, sc)
	assertLegible(t, a, sc, "a grouped worktree's view")
}

// newThemedApp is newTestApp with the theme chosen in its configuration, the
// way a user's start would have it.
func newThemedApp(t *testing.T, name string) (*App, tcell.SimulationScreen) {
	t.Helper()
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	cfg.Theme = name
	must(t, cfg.Save())
	a, sc := startApp(t, newApp(cfg, testVault(t, cfg)))
	if got := onLoop(a, func() string { return theme.Name }); got != name {
		t.Fatalf("the app drew with %q, not %q", got, name)
	}
	return a, sc
}

// TestChoosingAThemePutsItOnAndKeepsIt: Enter in Settings › Theme draws the
// screen again in the theme, stays in the section, and saves the choice,
// which a later start puts on.
func TestChoosingAThemePutsItOnAndKeepsIt(t *testing.T) {
	restoreDefaultTheme(t)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionTheme)
	waitFor(t, a, sc, "catppuccin-mocha")
	row := lineOf(a.screenText(sc), "catppuccin-mocha") - lineOf(a.screenText(sc), "unagit ")
	for i := 0; i < row; i++ {
		typeRunes(sc, "j")
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Theme: catppuccin-mocha")
	if got := onLoop(a, func() tcell.Color { return colBackground }); got != tcell.GetColor("#1e1e2e") {
		t.Errorf("the background is %v, not catppuccin's", got)
	}
	if got := onLoop(a, func() int { return a.settings.current }); got != sectionTheme {
		t.Errorf("the switch left Settings › Theme for section %d", got)
	}
	assertLegible(t, a, sc, "Settings › Theme in catppuccin")

	saved := readConfigFile(t, a)
	if !strings.Contains(saved, "theme: catppuccin-mocha") {
		t.Errorf("the choice was not saved:\n%s", saved)
	}

	setTheme(loadThemes("").byName[defaultThemeName])
	again := newApp(a.cfg, testVault(t, a.cfg))
	b, sc2 := startApp(t, again)
	waitFor(t, b, sc2, "acme/gateway")
	if got := onLoop(b, func() string { return theme.Name }); got != "catppuccin-mocha" {
		t.Errorf("a new start drew with %q", got)
	}
}

// TestSwitchThemeFromAnyScreen: : on a list offers Switch Theme…, which
// lists the themes with the one on marked and the cursor on it; Enter on
// another puts it on, and the list behind is drawn again in it.
func TestSwitchThemeFromAnyScreen(t *testing.T) {
	restoreDefaultTheme(t)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, ":")
	waitFor(t, a, sc, "Switch Theme…")
	typeRunes(sc, "switch theme")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "catppuccin-mocha")
	if line := lineAt(a.screenText(sc), glyphCheck+" "+defaultThemeName+" "); line == "" {
		t.Errorf("the theme on is not marked:\n%s", a.screenText(sc))
	}
	assertLegible(t, a, sc, "the theme picker")
	typeRunes(sc, "/")
	typeRunes(sc, "catppuccin")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Theme: catppuccin-mocha")
	if got := onLoop(a, func() string { return theme.Name }); got != "catppuccin-mocha" {
		t.Errorf("the theme is %q", got)
	}
	waitFor(t, a, sc, "Rate limiting")
	assertLegible(t, a, sc, "the merge requests in catppuccin")
	if !strings.Contains(readConfigFile(t, a), "theme: catppuccin-mocha") {
		t.Error("the choice was not saved")
	}
}

// TestAForkIsAFileToTuneAndIsFollowed: f writes the theme under the cursor
// out whole as the user's own and puts it on; a save of the file puts the
// change on at once, and a save that breaks it keeps the last good one and
// says what is wrong.
func TestAForkIsAFileToTuneAndIsFollowed(t *testing.T) {
	restoreDefaultTheme(t)
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	app := newApp(cfg, testVault(t, cfg))
	app.themeWatchEvery = 30 * time.Millisecond
	observed := make(chan struct{}, 1)
	app.themeStat = func(path string) (os.FileInfo, error) {
		info, err := os.Stat(path)
		if err == nil {
			select {
			case observed <- struct{}{}:
			default:
			}
		}
		return info, err
	}
	a, sc := startApp(t, app)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionTheme)
	waitFor(t, a, sc, "retro-block")
	typeRunes(sc, "f")
	waitFor(t, a, sc, "Fork theme unagit")
	assertLegible(t, a, sc, "the fork form")
	pressButton(t, a, sc, currentForm(a), "Fork")
	waitFor(t, a, sc, "Forked unagit")

	path := filepath.Join(a.cfg.ThemesDir(), "unagit-mine.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the fork was not written: %v", err)
	}
	var written Theme
	must(t, json.Unmarshal(data, &written))
	for key, value := range written.colours() {
		if value == "" {
			t.Errorf("the fork leaves %s out; it should spell out everything", key)
		}
	}
	if written.Extends != "" || written.Name != "unagit-mine" {
		t.Errorf("the fork is named %q and extends %q", written.Name, written.Extends)
	}
	if got := onLoop(a, func() string { return theme.Name }); got != "unagit-mine" {
		t.Fatalf("the fork is not on: %q", got)
	}
	// A file saved before the watcher first reads it becomes its baseline;
	// that is not an edit it could have detected.
	select {
	case <-observed:
	case <-time.After(patience):
		t.Fatal("the theme watcher never read the fork")
	}

	// A save of the file shows at once.
	edited := strings.Replace(string(data), `"accent": "109"`, `"accent": "#ff0000"`, 1)
	if edited == string(data) {
		t.Fatal("the fork has no text.accent to tune")
	}
	resave := func(body string) {
		must(t, os.WriteFile(path, []byte(body), 0o644))
		later := time.Now().Add(2 * time.Second)
		must(t, os.Chtimes(path, later, later))
	}
	resave(edited)
	accent := func() tcell.Color { return onLoop(a, func() tcell.Color { return colAccent }) }
	deadline := time.Now().Add(patience)
	for accent() != tcell.GetColor("#ff0000") {
		if time.Now().After(deadline) {
			t.Fatalf("the saved accent never came on: %v", accent())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// A save that breaks it keeps what was on.
	resave(strings.Replace(edited, `"#ff0000"`, `"reddish"`, 1))
	waitFor(t, a, sc, "cannot be used as it is now")
	waitFor(t, a, sc, "text.accent")
	if got := accent(); got != tcell.GetColor("#ff0000") {
		t.Errorf("a broken save changed the accent to %v", got)
	}
}

// TestTheForkFormFitsItsFrame draws the form down to a small terminal.
func TestTheForkFormFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resize(sc, size.w, size.h)
			openSection(t, a, sc, sectionTheme)
			waitFor(t, a, sc, "retro-block")
			typeRunes(sc, "f")
			waitFor(t, a, sc, "Fork theme unagit")
			assertFormInFrame(t, a, sc, currentForm(a))
			text := a.screenText(sc)
			for _, want := range []string{"Name", "unagit-mine", "every colour and glyph", "Fork", "Cancel"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
		})
	}
}
