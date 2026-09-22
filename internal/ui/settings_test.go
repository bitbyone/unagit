package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
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
	openSection(t, a, sc, sectionServers)
	waitFor(t, a, sc, "acme")

	typeRunes(sc, "a")
	waitFor(t, a, sc, "Add a GitLab server")
	form := currentForm(a)
	if form == nil {
		t.Fatal("no form on screen")
	}
	setField(t, a, form, 0, "Personal")
	setField(t, a, form, 1, "https://gitlab.com")
	setField(t, a, form, 2, "~/personal")
	setField(t, a, form, 3, "glpat-personal-token")
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
	typeRunes(sc, "P")
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
	openSection(t, a, sc, sectionServers)
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

	openSection(t, a, sc, sectionServers)
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
