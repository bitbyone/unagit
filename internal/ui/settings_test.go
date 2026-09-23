package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/secret"
)

// currentForm is the form of the modal currently on top, if any.
func currentForm(a *App) *tview.Form {
	done := make(chan *tview.Form, 1)
	a.tv.QueueUpdate(func() {
		_, prim := a.pages.GetFrontPage()
		mb, ok := prim.(*modalBox)
		if !ok {
			done <- nil
			return
		}
		form, _ := mb.content.(*tview.Form)
		done <- form
	})
	select {
	case f := <-done:
		return f
	case <-time.After(2 * time.Second):
		return nil
	}
}

func setField(t *testing.T, a *App, form *tview.Form, index int, value string) {
	t.Helper()
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		field, ok := form.GetFormItem(index).(*tview.InputField)
		if !ok {
			t.Errorf("form item %d is not an input field", index)
			close(done)
			return
		}
		field.SetMaskCharacter(0)
		field.SetText(value)
		close(done)
	})
	<-done
}

// pressButton activates a form button the way Enter on it would.
func pressButton(t *testing.T, a *App, sc tcell.SimulationScreen, form *tview.Form, label string) {
	t.Helper()
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		idx := form.GetButtonIndex(label)
		if idx < 0 {
			t.Errorf("no button %q", label)
			close(done)
			return
		}
		form.SetFocus(form.GetFormItemCount() + idx)
		a.tv.SetFocus(form)
		close(done)
	})
	<-done
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(100 * time.Millisecond)
}

// TestGeneralSectionEditsTheConfig: the editor and the root are set from the
// interface, not from the file.
func TestGeneralSectionEditsTheConfig(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGeneral)
	waitFor(t, a, sc, "Default root")

	form := a.settings.general
	setField(t, a, form, 0, "/tmp/unagit-root")
	setField(t, a, form, 1, "hx")
	setField(t, a, form, 2, "--config foo .")
	pressButton(t, a, sc, form, "Save")

	if a.cfg.RootDir != "/tmp/unagit-root" || a.cfg.Editor != "hx" {
		t.Fatalf("cfg = %+v", a.cfg)
	}
	if strings.Join(a.cfg.EditorArgs, " ") != "--config foo ." {
		t.Errorf("editor args = %q", a.cfg.EditorArgs)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Editor != "hx" || saved.RootDir != "/tmp/unagit-root" {
		t.Errorf("not written to disk: %+v", saved)
	}
}

// TestAddServerFromTheInterface is the whole point of this screen: a second
// GitLab, with its own token, without touching a file.
func TestAddServerFromTheInterface(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGitLab)
	waitFor(t, a, sc, "acme")

	typeRunes(sc, "a")
	waitFor(t, a, sc, "Add a GitLab server")
	form := currentForm(a)
	if form == nil {
		t.Fatal("no form on screen")
	}
	// Name, URL, Root directory, Clone over, Token.
	setField(t, a, form, 0, "Personal")
	setField(t, a, form, 1, "https://gitlab.com")
	setField(t, a, form, 2, "~/personal")
	setField(t, a, form, 4, "glpat-personal-token")
	pressButton(t, a, sc, form, "Save")

	if len(a.cfg.Instances) != 2 {
		t.Fatalf("instances = %+v", a.cfg.Instances)
	}
	added := a.cfg.Instances[1]
	if added.Name != "Personal" || added.URL != "https://gitlab.com" || added.RootDir != "~/personal" {
		t.Fatalf("added = %+v", added)
	}
	if added.ID != "gitlab-com" {
		t.Errorf("id = %q", added.ID)
	}
	if a.vault.Token(added.ID) != "glpat-personal-token" {
		t.Error("the token was not stored")
	}
	if a.client(added.ID) == nil {
		t.Error("no API client for the new server")
	}
	// It survives a restart, and the token is not in the config file.
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Instances) != 2 {
		t.Fatalf("saved instances = %+v", saved.Instances)
	}
	raw := readConfigFile(t)
	if strings.Contains(raw, "glpat-personal-token") {
		t.Fatal("the token was written to config.yaml")
	}
	// With two servers the lists say where a row came from.
	waitFor(t, a, sc, "Personal")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "SERVER")
}

func readConfigFile(t *testing.T) string {
	t.Helper()
	b, err := readFileString(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestTokenFormStoresAndRemoves
func TestTokenFormStoresAndRemoves(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGitLab)
	waitFor(t, a, sc, "stored")

	id := a.cfg.Instances[0].ID
	typeRunes(sc, "t")
	waitFor(t, a, sc, "Token · acme")
	form := currentForm(a)
	setField(t, a, form, 0, "glpat-replaced")
	pressButton(t, a, sc, form, "Save")
	if a.vault.Token(id) != "glpat-replaced" {
		t.Fatalf("token = %q", a.vault.Token(id))
	}

	typeRunes(sc, "t")
	waitFor(t, a, sc, "Token · acme")
	pressButton(t, a, sc, currentForm(a), "Remove token")
	if a.vault.Has(id) {
		t.Fatal("the token was not removed")
	}
	waitFor(t, a, sc, "missing")
	if a.client(id) != nil {
		t.Error("the API client outlived its token")
	}
}

// TestGroupRootOverrideMovesTheClone: a per-group directory is the point of
// the Groups & roots section.
func TestGroupRootOverrideMovesTheClone(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	id := a.cfg.Instances[0].ID
	before := a.projectDir(id, "acme/gateway")

	openSection(t, a, sc, sectionGroups)
	waitFor(t, a, sc, "incl. subgroups")
	typeRunes(sc, "j") // from the server node onto the group
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Clone directory · acme")

	form := currentForm(a)
	setField(t, a, form, 0, "/tmp/unagit-acme")
	pressButton(t, a, sc, form, "Save")

	after := a.projectDir(id, "acme/gateway")
	if after == before {
		t.Fatalf("the clone directory did not move: %s", after)
	}
	if !strings.HasPrefix(after, "/tmp/unagit-acme/") {
		t.Fatalf("clone directory = %s", after)
	}
	if got := a.cfg.Instance(id).Group(1).RootDir; got != "/tmp/unagit-acme" {
		t.Errorf("stored root = %q", got)
	}

	// Inherit puts it back.
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Clone directory · acme")
	pressButton(t, a, sc, currentForm(a), "Inherit")
	if got := a.projectDir(id, "acme/gateway"); got != before {
		t.Errorf("after Inherit the clone directory is %s, want %s", got, before)
	}
}

// TestServerRootOverrideAppliesToEverythingOnIt
func TestServerRootOverrideAppliesToEverythingOnIt(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	id := a.cfg.Instances[0].ID

	openSection(t, a, sc, sectionGroups)
	waitFor(t, a, sc, "incl. subgroups")
	// The cursor starts on the server node.
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Clone directory · acme")
	form := currentForm(a)
	setField(t, a, form, 0, "/tmp/unagit-server")
	pressButton(t, a, sc, form, "Save")

	if got := a.projectDir(id, "acme/gateway"); !strings.HasPrefix(got, "/tmp/unagit-server/") {
		t.Fatalf("clone directory = %s", got)
	}
	if got := a.cfg.Instance(id).RootDir; got != "/tmp/unagit-server" {
		t.Errorf("stored server root = %q", got)
	}
}

// TestRemoveServerForgetsItsToken
func TestRemoveServerForgetsItsToken(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	id := a.cfg.Instances[0].ID

	openSection(t, a, sc, sectionGitLab)
	waitFor(t, a, sc, "stored")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Remove server")
	typeRunes(sc, "y")

	time.Sleep(100 * time.Millisecond)
	if len(a.cfg.Instances) != 0 {
		t.Fatalf("instances = %+v", a.cfg.Instances)
	}
	if a.vault.Has(id) {
		t.Error("the token outlived the server")
	}
}

// TestSecuritySectionChangesThePassphrase
func TestSecuritySectionChangesThePassphrase(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionSecurity)
	waitFor(t, a, sc, "change the passphrase")

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Change the passphrase")
	form := currentForm(a)
	setField(t, a, form, 0, "brand-new")
	setField(t, a, form, 1, "mismatch")
	pressButton(t, a, sc, form, "Change")
	waitFor(t, a, sc, "do not match")

	setField(t, a, form, 0, "brand-new")
	setField(t, a, form, 1, "brand-new")
	pressButton(t, a, sc, form, "Change")
	waitFor(t, a, sc, "Passphrase changed")

	// The vault on disk now opens with the new passphrase only.
	if _, err := openVaultFile(t, "brand-new"); err != nil {
		t.Errorf("the new passphrase does not open the vault: %v", err)
	}
	if _, err := openVaultFile(t, "test-passphrase"); err == nil {
		t.Error("the old passphrase still opens the vault")
	}
}

func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func openVaultFile(t *testing.T, passphrase string) (*secret.Vault, error) {
	t.Helper()
	return secret.OpenVault(config.VaultPath(), []byte(passphrase))
}

// TestAddGitHubAccount: the GitHub form has no URL, because github.com is the
// only address there is.
func TestAddGitHubAccount(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGitHub)
	waitFor(t, a, sc, "No GitHub account yet")

	typeRunes(sc, "a")
	waitFor(t, a, sc, "Add a GitHub account")
	form := currentForm(a)
	if form == nil {
		t.Fatal("no form on screen")
	}
	// Name, Root directory, Clone over, Token, and the note: no URL.
	if got := form.GetFormItemCount(); got != 5 {
		t.Fatalf("the GitHub form has %d items; it should not ask for a URL", got)
	}
	if label := form.GetFormItem(1).(*tview.InputField).GetLabel(); !strings.HasPrefix(label, "Root") {
		t.Fatalf("item 1 is %q, so the URL was asked for after all", label)
	}
	setField(t, a, form, 0, "Personal")
	setField(t, a, form, 1, "~/github")
	setField(t, a, form, 3, "ghp-token")
	pressButton(t, a, sc, form, "Save")

	gh := a.cfg.InstancesOfKind(config.KindGitHub)
	if len(gh) != 1 {
		t.Fatalf("github instances = %+v", gh)
	}
	if gh[0].URL != config.GitHubURL || gh[0].Name != "Personal" || gh[0].RootDir != "~/github" {
		t.Fatalf("instance = %+v", gh[0])
	}
	if a.vault.Token(gh[0].ID) != "ghp-token" {
		t.Error("the token was not stored")
	}
	if a.client(gh[0].ID) == nil || a.client(gh[0].ID).Kind() != forge.KindGitHub {
		t.Error("no GitHub client for the new account")
	}
	// It stays out of the GitLab section.
	if len(a.cfg.InstancesOfKind(config.KindGitLab)) != 1 {
		t.Errorf("gitlab instances = %+v", a.cfg.InstancesOfKind(config.KindGitLab))
	}
	waitFor(t, a, sc, "github.com")
}

// TestGitHubOrgIsOnOrOff: there are no subgroups on GitHub, so space toggles
// rather than cycles.
func TestGitHubOrgIsOnOrOff(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	// Add a GitHub account with one organisation in the group cache.
	done := make(chan string, 1)
	a.tv.QueueUpdateDraw(func() {
		inst := a.cfg.AddInstance(config.Instance{Kind: config.KindGitHub, Name: "Personal"})
		a.vault.Set(inst.ID, "ghp-token")
		a.groups = append(a.groups, forge.Group{ID: 10, Name: "widgets", FullPath: "widgets", Instance: inst.ID})
		a.saveConfig()
		a.settings.reload()
		done <- inst.ID
	})
	id := <-done

	openSection(t, a, sc, sectionGroups)
	waitFor(t, a, sc, "widgets")

	// Walk down to the organisation: gitlab server, its group, github account.
	typeRunes(sc, "jjj")
	typeRunes(sc, " ")
	waitFor(t, a, sc, "selected")
	if got := a.cfg.Instance(id).GroupScope(10); got != config.ScopeGroup {
		t.Fatalf("scope = %q, want %q", got, config.ScopeGroup)
	}
	// A GitHub organisation never says "incl. subgroups".
	if strings.Contains(a.screenText(sc), "incl. subgroups\n") {
		t.Log(a.screenText(sc))
	}

	typeRunes(sc, " ")
	waitFor(t, a, sc, "unselected")
	if got := a.cfg.Instance(id).GroupScope(10); got != "" {
		t.Fatalf("scope = %q, want unselected", got)
	}
}

// setDropDown picks an option of a form's select box.
func setDropDown(t *testing.T, a *App, form *tview.Form, index int, option string) {
	t.Helper()
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		defer close(done)
		drop, ok := form.GetFormItem(index).(*tview.DropDown)
		if !ok {
			t.Errorf("form item %d is not a drop down", index)
			return
		}
		for i := 0; i < drop.GetOptionCount(); i++ {
			drop.SetCurrentOption(i)
			if _, text := drop.GetCurrentOption(); text == option {
				return
			}
		}
		t.Errorf("no option %q", option)
	})
	<-done
}

// TestSwitchingToSSHOffersToRepointExistingClones is the whole point of the
// setting: the repositories already on disk keep the old protocol otherwise.
func TestSwitchingToSSHOffersToRepointExistingClones(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	id := a.cfg.Instances[0].ID
	if got := a.cfg.Instance(id).Protocol(); got != config.ProtocolHTTPS {
		t.Fatalf("protocol starts at %q", got)
	}

	openSection(t, a, sc, sectionGitLab)
	waitFor(t, a, sc, "https")

	typeRunes(sc, "e")
	waitFor(t, a, sc, "Edit server")
	form := currentForm(a)
	setDropDown(t, a, form, 3, config.ProtocolSSH)
	pressButton(t, a, sc, form, "Save")

	if got := a.cfg.Instance(id).Protocol(); got != config.ProtocolSSH {
		t.Fatalf("protocol = %q", got)
	}
	// Nothing is cloned in this fixture, so it says so rather than asking.
	waitFor(t, a, sc, "will be cloned over ssh from now on")
	// And the table shows it.
	waitFor(t, a, sc, "ssh")

	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Instance(id).Protocol() != config.ProtocolSSH {
		t.Errorf("not written to disk: %q", saved.Instance(id).Protocol())
	}
}

// TestNewServersDefaultToHTTPS keeps the behaviour unagit always had.
func TestNewServersDefaultToHTTPS(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGitLab)
	typeRunes(sc, "a")
	waitFor(t, a, sc, "Add a GitLab server")

	form := currentForm(a)
	setField(t, a, form, 0, "Other")
	setField(t, a, form, 1, "https://gitlab.other")
	pressButton(t, a, sc, form, "Save")

	added := a.cfg.Instance("gitlab-other")
	if added == nil {
		t.Fatalf("instances = %+v", a.cfg.Instances)
	}
	if added.Protocol() != config.ProtocolHTTPS {
		t.Errorf("protocol = %q", added.Protocol())
	}
}

// TestProtocolSelectIsLegibleWhenFocused pins a fix: tview builds a focused
// drop-down out of Styles.PrimaryTextColor on Styles.ContrastBackgroundColor,
// and this interface leaves the latter at the terminal default so panels stay
// transparent - which painted the text in its own background's colour and left
// a blank block on screen.
func TestProtocolSelectIsLegibleWhenFocused(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGitLab)
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Edit server")

	form := currentForm(a)
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		form.SetFocus(3) // the Clone over select
		a.tv.SetFocus(form)
		close(done)
	})
	<-done
	// It says what is selected, and looks like something that opens.
	waitFor(t, a, sc, "https ▾")

	row := rowOf(t, a, sc, "Clone over")
	line := strings.Split(a.screenText(sc), "\n")[row]
	at := strings.Index(line, "https")
	if at < 0 {
		t.Fatalf("the value is not on screen: %q", line)
	}
	// The line is full of box drawing, so byte offsets are not columns.
	col := len([]rune(line[:at]))
	for _, x := range []int{col, col + 1, col + 2} {
		r, style := cellAt(a, sc, x, row)
		fg, bg, _ := style.Decompose()
		if fg == bg {
			t.Fatalf("%q at column %d is invisible: foreground and background are both %v", r, x, fg)
		}
	}
}

// borderColours reads the colour of the two box corners on the Settings
// header row: the sidebar's and the content pane's.
func borderColours(t *testing.T, a *App, sc tcell.SimulationScreen) (sidebar, content tcell.Color) {
	t.Helper()
	row := rowOf(t, a, sc, "╭ Settings")
	line := []rune(strings.Split(a.screenText(sc), "\n")[row])

	var corners []int
	for i, r := range line {
		if r == '╭' {
			corners = append(corners, i)
		}
	}
	if len(corners) < 2 {
		t.Fatalf("expected two boxes on the row, got %d: %q", len(corners), string(line))
	}
	fg := func(x int) tcell.Color {
		_, style := cellAt(a, sc, x, row)
		c, _, _ := style.Decompose()
		return c
	}
	return fg(corners[0]), fg(corners[1])
}

// TestSettingsShowsWhichHalfHasTheKeyboard: without it there is no telling
// whether typing goes to the sidebar or to the pane beside it.
func TestSettingsShowsWhichHalfHasTheKeyboard(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "S")
	waitFor(t, a, sc, "GitLab servers")

	sidebar, content := borderColours(t, a, sc)
	if sidebar == content {
		t.Fatalf("both borders are %v; the focused half is not marked", sidebar)
	}
	if sidebar != colBorderFocus {
		t.Errorf("the sidebar has the keyboard but its border is %v", sidebar)
	}

	// Move into the content and the highlight moves with it.
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	sidebar, content = borderColours(t, a, sc)
	if content != colBorderFocus {
		t.Errorf("the content has the keyboard but its border is %v", content)
	}
	if sidebar == colBorderFocus {
		t.Error("the sidebar is still marked as focused")
	}

	// And back.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	sidebar, content = borderColours(t, a, sc)
	if sidebar != colBorderFocus || content == colBorderFocus {
		t.Errorf("after Esc: sidebar %v, content %v", sidebar, content)
	}
}

// TestSelectBoxRefusesTyping: tview would otherwise feed the keys into a
// hidden search field and open the list on them.
func TestSelectBoxRefusesTyping(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGitLab)
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Edit server")

	form := currentForm(a)
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		form.SetFocus(3)
		a.tv.SetFocus(form)
		close(done)
	})
	<-done
	waitFor(t, a, sc, "https ▾")

	typeRunes(sc, "ssh")
	time.Sleep(100 * time.Millisecond)

	// Nothing was typed anywhere, and the value did not change behind our back.
	waitFor(t, a, sc, "https ▾")
	drop := make(chan string, 1)
	a.tv.QueueUpdateDraw(func() {
		_, text := form.GetFormItem(3).(*tview.DropDown).GetCurrentOption()
		drop <- text
	})
	if got := <-drop; got != config.ProtocolHTTPS {
		t.Fatalf("typing changed the selection to %q", got)
	}

	// The arrows still work.
	sc.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(100 * time.Millisecond)
	a.tv.QueueUpdateDraw(func() {
		_, text := form.GetFormItem(3).(*tview.DropDown).GetCurrentOption()
		drop <- text
	})
	if got := <-drop; got != config.ProtocolSSH {
		t.Fatalf("the arrows do not select either: %q", got)
	}
}

func TestIncommIntegrationSetting(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionIntegrations)
	waitFor(t, a, sc, "not installed")
	typeRunes(sc, "e")
	if onLoop(a, func() bool { return a.cfg.Integrations.Incomm }) {
		t.Fatal("missing binary can be enabled")
	}
	if err := os.WriteFile(bin+"/incomm", []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	typeRunes(sc, "c")
	waitFor(t, a, sc, "● disabled")
	waitFor(t, a, sc, "e toggle")
	assertLegible(t, a, sc, "disabled integration")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "● enabled")
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Integrations.Incomm {
		t.Fatal("integration was not saved")
	}
	assertLegible(t, a, sc, "enabled integration")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "● disabled")
	saved, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Integrations.Incomm {
		t.Fatal("disabled integration was not saved")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	deadline := time.Now().Add(2 * time.Second)
	for onLoop(a, func() bool { return strings.Contains(a.settings.integrations.cards[0].view.GetText(true), "e toggle") }) {
		if time.Now().After(deadline) {
			t.Fatal("unfocused integration still shows action keys")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
