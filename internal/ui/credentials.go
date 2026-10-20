package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/credential"
	"github.com/tobola/unagit/internal/secret"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/watch"
)

// unagit keeps tokens for unibox's sandboxes in its vault, beside the
// forge tokens and never among them, and hands them to the runtime that
// asks over a socket of its own while it runs unlocked (internal/credential,
// docs/sandbox-credentials.md). A profile names `unagit credential get
// <name>` as the source of an agent's or a service's token; unibox writes
// that into the profile and tells the runtime, so unagit writes no YAML.
// Every handout and every refusal goes to the Activity log.

// credentialName is what a stored token may be called: what a profile's
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

// credentialCommand is the host command a profile names for a stored
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

// agentTokenHints says, per agent, which token signs it in.
var agentTokenHints = map[string]string{
	"claude":  "The token claude setup-token prints: it signs\nyour subscription in for a year.",
	"copilot": "A fine-grained GitHub token with Account\npermissions > Copilot Requests: Read-only alone.",
	"codex":   "An OpenAI API key.",
}

// signInAgents lists a profile's agents to sign in: Enter keeps the token
// in unagit, u leaves it to unibox, which asks in this terminal.
func (a *App) signInAgents(pr unibox.Profile) {
	if len(pr.Agents) == 0 {
		a.uniboxSignIn("Signing in the agents of "+pr.Name, "agent", "login", "-p", pr.Name)
		return
	}
	var items []pickItem
	for _, id := range pr.Agents {
		sub, about := "", "Keep its token in unagit's vault; the profile names unagit credential get as its source."
		if _, ok := agentTokenHints[id]; !ok {
			about = "unibox has no token sign-in for it; u runs its own."
		}
		if a.vault != nil {
			if _, ok := a.vault.Credential(id); ok {
				sub = "token in unagit"
			}
		}
		items = append(items, pickItem{Label: agentLabel(id), Sub: sub, About: about, Data: id})
	}
	keys := []pickKey{
		{keys: "u", hint: "with unibox", name: "Sign In with unibox…", about: "unibox signs the agent in, in this terminal, and keeps its token in the platform's store.",
			run: func(it pickItem) {
				id := it.Data.(string)
				a.uniboxSignIn("Signing in "+id+" of "+pr.Name, "agent", "login", id, "-p", pr.Name)
			}},
	}
	a.showPickerWith("Sign in · "+pr.Name, items, pickerOptions{pack: true, explain: true, keys: keys,
		enterName: "Sign In with a Token in unagit…", enterAbout: "Type the agent's token; unagit keeps it and hands it to the sandboxes while it runs unlocked.", enterHint: "token in unagit"},
		func(it pickItem) { a.askAgentToken(pr, it.Data.(string)) })
}

// askAgentToken takes an agent's token into the vault and has unibox name
// unagit as its source in the profile.
func (a *App) askAgentToken(pr unibox.Profile, id string) {
	hint, ok := agentTokenHints[id]
	if !ok {
		a.flash("unibox signs " + id + " in by no token - press u to sign it in with unibox")
		return
	}
	_, stored := a.vault.Credential(id)
	if stored {
		hint += "\nLeave it empty to keep the stored one."
	}
	form := tview.NewForm()
	styleForm(form)
	field := addPassword(form, "Token", 0)
	lines := strings.Count(hint, "\n") + 1
	form.AddTextView("", hint, 0, lines, true, false)
	form.AddButton("Sign In", func() {
		token := strings.TrimSpace(field.GetText())
		if token == "" && !stored {
			a.flash("enter the token - " + strings.ReplaceAll(strings.SplitN(hint, ":", 2)[0], "\n", " "))
			return
		}
		if !a.storeCredential(secret.Credential{Name: id, Kind: secret.CredentialAgent, Token: token}) {
			return
		}
		a.closeModal(pageForm)
		a.uniboxDo("Signing "+id+" in through unagit", "agent", "login", id, "-p", pr.Name, "--command", a.credentialCommand(id))
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Sign in "+agentLabel(id)+" · "+pr.Name, form, 64, 7+lines)
}

// serviceKinds are the services unibox knows by kind, and their hosts
// when none is given.
var serviceKinds = []string{"gitlab", "github", "artifactory", "generic"}

// askServiceToken adds a service to a profile whose token unagit keeps.
func (a *App) askServiceToken(pr unibox.Profile) {
	const nameLabel, kindLabel, hostsLabel, tokenLabel = "Service", "Kind", "Hosts", "Token"
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(nameLabel, "", 0, nil, nil)
	kind := addSelect(form, kindLabel, serviceKinds, 0)
	form.AddInputField(hostsLabel, "", 0, nil, nil)
	token := addPassword(form, tokenLabel, 0)
	form.AddTextView("", "Hosts: empty for the kind's own - gitlab.com,\ngithub.com; github.com/<owner> for one owner.\nToken: a narrow one, for the sandboxes alone.", 0, 3, true, false)
	form.AddButton("Add", func() {
		name := strings.TrimSpace(form.GetFormItemByLabel(nameLabel).(*tview.InputField).GetText())
		hosts := strings.FieldsFunc(form.GetFormItemByLabel(hostsLabel).(*tview.InputField).GetText(), func(r rune) bool { return r == ',' || r == ' ' })
		value := strings.TrimSpace(token.GetText())
		_, k := kind.GetCurrentOption()
		switch {
		case !credentialName.MatchString(name):
			a.flash("enter the service's name: letters, digits, dots, dashes - like gitlab-2n")
			return
		case value == "":
			a.flash("enter the service's token")
			return
		case len(hosts) == 0 && (k == "artifactory" || k == "generic"):
			a.flash("enter the hosts the token is for")
			return
		}
		if !a.storeCredential(secret.Credential{Name: name, Kind: secret.CredentialService, Hosts: hosts, Token: value}) {
			return
		}
		a.closeModal(pageForm)
		args := []string{"service", "add", name, "-p", pr.Name, "--kind", k, "--command", a.credentialCommand(name)}
		for _, h := range hosts {
			args = append(args, "--host", h)
		}
		a.uniboxDo("Adding "+name+" with a token in unagit", args...)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Add a service · "+pr.Name, form, 64, 16)
}

// showCredentials lists the tokens unagit keeps for the sandboxes: Enter
// replaces one, d removes it, y copies the command a profile names.
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
		items = append(items, pickItem{Label: "no tokens yet", About: "Sign In Agents… (A) and Services… (v) of a profile keep a token here."})
	}
	named := func(it pickItem) bool { return it.Data != nil }
	keys := []pickKey{
		{keys: "y", hint: "copy command", name: "Copy Command", about: "Put the command that prints the token on the clipboard, for a profile written by hand.",
			when: named, run: func(it pickItem) {
				if c, ok := it.Data.(secret.Credential); ok {
					a.yank("Command", a.credentialCommand(c.Name))
				}
			}},
		{keys: "d", hint: "remove", name: "Remove Token…", about: "Take the token out of the vault; a profile that names it fails at its next start until it is stored again.",
			when: named, run: func(it pickItem) {
				c, ok := it.Data.(secret.Credential)
				if !ok {
					return
				}
				a.confirmWith("Remove token", "Remove token "+c.Name+" from unagit's vault?\n\nA profile that names it fails at its next start until it is stored again.", "Remove", nil, func() {
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
		enterName: "Replace Token…", enterAbout: "Type a new value for the token; the profiles that name it take it at their next fetch.", enterHint: "replace"},
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
