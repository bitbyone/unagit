package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/credential"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/secret"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/watch"
)

// unagit keeps tokens for unibox's sandboxes in its vault, beside the
// forge tokens and never among them, and hands them to sbx when it asks
// over a socket of its own while unagit runs unlocked (internal/credential,
// docs/sandbox-credentials.md). unibox secret set --command has sbx keep
// `unagit credential get <service>` as the source of a service's token, so
// unagit writes nothing of sbx's or unibox's. Every handout and every
// refusal goes to the Activity log.

// credentialName is what a stored token may be called: what sbx's
// command names, so nothing a shell would read twice.
var credentialName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// credentialSocket is where this unagit hands the tokens out.
func (a *App) credentialSocket() string {
	if a.credSocket != "" {
		return a.credSocket
	}
	return credential.SocketPath(a.cfg.Dir())
}

// serveCredentials opens the socket once the vault is unlocked and holds a
// token for the sandboxes; without one there is nothing to hand out and no
// socket. It runs on the loop, again whenever a token is stored.
func (a *App) serveCredentials() {
	if a.credServer != nil || a.vault == nil || len(a.vault.Credentials()) == 0 {
		return
	}
	vault, store := a.vault, a.watchStore
	s, err := credential.Serve(a.credentialSocket(), func(r credential.Request, p credential.Peer) (string, error) {
		return handOut(vault, store, r, p)
	})
	switch {
	case errors.Is(err, credential.ErrServed):
		a.note("another unagit hands out the sandbox tokens; this one does not")
	case err != nil:
		a.flash(fmt.Sprintf("cannot hand out the sandbox tokens: %v", err))
	default:
		a.credServer = s
	}
}

// stopCredentials closes the socket, so the runtime is told unagit is gone
// rather than kept waiting.
func (a *App) stopCredentials() {
	if a.credServer != nil {
		_ = a.credServer.Close()
		a.credServer = nil
	}
}

// handOut answers one request from the vault, and writes down that it did.
// It runs on the socket's goroutine: the vault locks itself, and the store
// is a file.
func handOut(vault tokenVault, store *watch.Store, r credential.Request, p credential.Peer) (string, error) {
	c, ok := vault.Credential(r.Name)
	who := "a process"
	if p.Parent != "" {
		who = p.Parent
	}
	if p.PID > 0 {
		who += fmt.Sprintf(" (pid %d)", p.PID)
	}
	e := watch.Event{Key: "credential\x00" + r.Name, What: "token " + r.Name, At: time.Now()}
	var err error
	if ok {
		e.Heading, e.Line, e.Level = "Token handed out", "to "+who, watch.Info
	} else {
		err = fmt.Errorf("no token %s is kept for the sandboxes in unagit; store it from the Sandboxes tab", r.Name)
		e.Heading, e.Line, e.Level = "Token refused", who+" asked for a token unagit does not keep", watch.Warning
	}
	if store != nil {
		_ = store.AppendHistory(false, e)
	}
	if err != nil {
		return "", err
	}
	return c.Token, nil
}

// credentialCommand is the host command sbx keeps for a stored
// token: this unagit by an absolute path - the runtime runs it from no
// directory and with no PATH of the user's - and its socket when it is
// not where an unagit started without the environment would look.
func (a *App) credentialCommand(name string) string {
	bin, err := a.executable("unagit")
	if err != nil || !filepath.IsAbs(bin) {
		if self, err := os.Executable(); err == nil {
			bin = self
		}
	}
	parts := []string{shellWord(bin), "credential", "get", name}
	if home, err := os.UserHomeDir(); err == nil && a.cfg.Dir() != filepath.Join(home, ".config", "unagit") {
		parts = append(parts, "--socket", shellWord(a.credentialSocket()))
	}
	return strings.Join(parts, " ")
}

// shellWord quotes a word with a space or a quote in it for a shell.
func shellWord(s string) string {
	if !strings.ContainsAny(s, " '\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// storeCredential keeps a token for the sandboxes and starts handing it
// out.
func (a *App) storeCredential(c secret.Credential) bool {
	if a.vault == nil {
		a.flash("the vault is locked - unlock unagit first")
		return false
	}
	a.vault.SetCredential(c)
	if err := a.vault.Save(a.cfg.VaultPath()); err != nil {
		a.errorf("cannot save the tokens: %v", err)
		return false
	}
	a.serveCredentials()
	return true
}

// guideSecretToken makes a service's token where it is made, then asks
// for it: claude setup-token in this terminal for the subscription's
// token, which signs it in through the browser and prints it, or the page
// the env names. What setup-token prints is the user's to copy - read from
// its output, the token would hang on the wording of a program unagit does
// not own.
func (a *App) guideSecretToken(e unibox.Env, s unibox.Secret) {
	if a.vault == nil {
		a.flash("the vault is locked - unlock unagit first")
		return
	}
	if _, stored := a.vault.Credential(s.Service); stored {
		// Kept already: the form keeps the token when left empty.
		a.askSecretToken(e, s)
		return
	}
	if s.Page != "" {
		_ = openBrowser(s.Page)
		a.askSecretToken(e, s)
		return
	}
	if !claudeService(s) {
		a.askSecretToken(e, s)
		return
	}
	bin, err := a.executable("claude")
	if err != nil {
		a.askSecretToken(e, s, "claude is not installed here: run claude setup-token\nwhere it is.")
		return
	}
	cmd := exec.Command(bin, "setup-token")
	go func() {
		err := a.runTerminalEditor(cmd, editors.Editor{Name: "claude setup-token"})
		a.tv.QueueUpdateDraw(func() {
			a.askSecretToken(e, s)
			if err != nil {
				a.flash(fmt.Sprintf("claude setup-token did not finish (%v) - run it yourself and type the token it prints", err))
			}
		})
	}()
}

// claudeService says whether a token is Claude's subscription, which
// claude setup-token makes.
func claudeService(s unibox.Secret) bool {
	if s.Service == "anthropic" || s.Service == "claude" {
		return true
	}
	for _, k := range s.Kits {
		if strings.Contains(k, "claude") {
			return true
		}
	}
	return false
}

// askSecretToken takes a service's token into the vault and has sbx keep
// unagit as its source.
func (a *App) askSecretToken(e unibox.Env, s unibox.Secret, notes ...string) {
	_, stored := a.vault.Credential(s.Service)
	hint := firstOf(s.Hint, "A token for "+s.Service+", for the sandboxes alone.")
	if claudeService(s) && s.Hint == "" {
		hint = "The token claude setup-token printed: it signs\nyour subscription in for a year."
	}
	for _, n := range notes {
		hint += "\n" + n
	}
	if stored {
		hint += "\nLeave it empty to keep the stored one."
	}
	form := tview.NewForm()
	styleForm(form)
	field := addPassword(form, "Token", 0)
	lines := strings.Count(hint, "\n") + 1
	form.AddTextView("", hint, 0, lines, true, false)
	form.AddButton("Keep", func() {
		token := strings.TrimSpace(field.GetText())
		if token == "" && !stored {
			a.flash("enter the token for " + s.Service)
			return
		}
		if !a.storeCredential(secret.Credential{Name: s.Service, Kind: secret.CredentialService, Hosts: s.Domains, Token: token}) {
			return
		}
		a.closeModal(pageForm)
		a.uniboxDo("Keeping "+s.Service+" in unagit", "secret", "set", s.Service, "-e", e.Name, "--command", a.credentialCommand(s.Service))
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Token of "+s.Service+" · "+e.Name, form, 64, 7+lines)
}

// showCredentials lists the tokens unagit keeps for the sandboxes: Enter
// replaces one, d removes it, y copies the command sbx runs for it.
func (a *App) showCredentials() {
	if a.vault == nil {
		a.flash("the vault is locked - unlock unagit first")
		return
	}
	var items []pickItem
	for _, c := range a.vault.Credentials() {
		sub := c.Kind
		if len(c.Hosts) > 0 {
			sub += " · " + strings.Join(c.Hosts, ", ")
		}
		items = append(items, pickItem{Label: c.Name, Sub: sub, Data: c,
			About: "Handed to the sandboxes by " + a.credentialCommand(c.Name) + " while unagit runs unlocked. Its value is never shown."})
	}
	serving := "handed out while unagit runs unlocked"
	if a.credServer == nil {
		serving = "not handed out by this unagit"
	}
	if len(items) == 0 {
		items = append(items, pickItem{Label: "no tokens yet", About: "Tokens… (K) of an env keeps one here with t."})
	}
	named := func(it pickItem) bool { return it.Data != nil }
	keys := []pickKey{
		{keys: "y", hint: "copy command", name: "Copy Command", about: "Put the command that prints the token on the clipboard, for unibox secret set --command by hand.",
			when: named, run: func(it pickItem) {
				if c, ok := it.Data.(secret.Credential); ok {
					a.yank("Command", a.credentialCommand(c.Name))
				}
			}},
		{keys: "d", hint: "remove", name: "Remove Token…", about: "Take the token out of the vault; sbx fails to get it until it is stored again.",
			when: named, run: func(it pickItem) {
				c, ok := it.Data.(secret.Credential)
				if !ok {
					return
				}
				a.confirmWith("Remove token", "Remove token "+c.Name+" from unagit's vault?\n\nsbx fails to get it until it is stored again.", "Remove", nil, func() {
					a.closeModal(pageConfirm)
					a.vault.RemoveCredential(c.Name)
					a.saveVault()
					if len(a.vault.Credentials()) == 0 {
						a.stopCredentials()
					}
					a.done("removed token " + c.Name)
				})
			}},
	}
	a.showPickerWith("Tokens in unagit · "+serving, items, pickerOptions{pack: true, explain: true, keys: keys,
		enterName: "Replace Token…", enterAbout: "Type a new value for the token; sbx takes it at its next fetch.", enterHint: "replace"},
		func(it pickItem) {
			if c, ok := it.Data.(secret.Credential); ok {
				a.askReplaceCredential(c)
			}
		})
}

// askReplaceCredential takes a new value for a stored token.
func (a *App) askReplaceCredential(c secret.Credential) {
	form := tview.NewForm()
	styleForm(form)
	field := addPassword(form, "Token", 0)
	form.AddButton("Replace", func() {
		value := strings.TrimSpace(field.GetText())
		if value == "" {
			a.flash("enter the new token")
			return
		}
		c.Token = value
		if !a.storeCredential(c) {
			return
		}
		a.closeModal(pageForm)
		a.done("replaced token " + c.Name)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Replace "+c.Name, form, 60, 7)
}
