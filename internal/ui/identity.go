package ui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/workspace"
)

// Who commits are made as is set in Settings - General for everything, a
// server, a group or a repository for what is below it - and unagit writes
// the outcome into each clone's own git configuration, where git, every
// editor and every agent read it before the global one. So an agent at work
// in a client's private repository commits under the address chosen for it,
// not under whatever ~/.gitconfig says.

// identityFor is the identity a repository's clone is given, for its
// workspace manager; nil leaves the clone alone (unagit -s, which writes
// nothing of unagit's).
func (a *App) identityFor(instance, path string) *workspace.Identity {
	if a.isSolo(instance, path) {
		return nil
	}
	id := a.cfg.IdentityOf(instance, path)
	return &workspace.Identity{Name: id.Name, Email: id.Email}
}

// identityFrom says where an identity's half was set, for a sentence.
func identityFrom(from string) string {
	switch from {
	case "":
		return "git's global configuration"
	case config.IdentityGlobal:
		return "General"
	case config.IdentityServer:
		return "the server"
	}
	return from
}

// identityLine is a repository's identity as its detail shows it: who, and
// where each half comes from.
func (a *App) identityLine(instance, path string) string {
	id := a.cfg.IdentityOf(instance, path)
	if id.Empty() {
		return tag(colDim) + "git's global one - none set in Settings" + tagEnd
	}
	from := identityFrom(id.EmailFrom)
	if id.NameFrom != id.EmailFrom {
		from = "name from " + identityFrom(id.NameFrom) + ", address from " + identityFrom(id.EmailFrom)
	}
	if from == path {
		from = "this repository's own"
	} else if !strings.Contains(from, ",") {
		from = "from " + from
	}
	return esc(id.String()) + tag(colDim) + " · " + esc(from) + tagEnd
}

// identitySweep remembers, by clone, the identity last written into it in
// this run, so a look at the disk asks git only about the clones whose
// identity changed since.
type identitySweep struct {
	mu      sync.Mutex
	written map[string]config.GitIdentity
	// generation counts the passes begun. A pass writes only while it is
	// the latest, holding write: one begun earlier, with the identities as
	// they were then, must not write over what a later one wrote.
	generation int
	write      sync.Mutex
}

// applyIdentities writes the identity of every clone on disk whose identity
// is not the one written last, in the background, and then calls done with
// how many clones this pass changed (done may be nil); another pass may
// have been first to some.
func (a *App) applyIdentities(done func(changed int)) {
	if a.solo != nil {
		return
	}
	type job struct {
		dir string
		id  config.GitIdentity
	}
	var jobs []job
	a.identities.mu.Lock()
	if a.identities.written == nil {
		a.identities.written = map[string]config.GitIdentity{}
	}
	a.identities.generation++
	generation := a.identities.generation
	for key, info := range a.disk {
		if !info.Cloned {
			continue
		}
		dir := a.projectDir(key.Instance, key.Path)
		id := a.cfg.IdentityOf(key.Instance, key.Path).GitIdentity
		if was, ok := a.identities.written[dir]; ok && was == id {
			continue
		}
		jobs = append(jobs, job{dir, id})
	}
	a.identities.mu.Unlock()
	if len(jobs) == 0 {
		if done != nil {
			done(0)
		}
		return
	}
	go func() {
		g := gitx.New("", nil)
		var mu sync.Mutex
		changed := 0
		var failed []string
		var wg sync.WaitGroup
		sem := make(chan struct{}, syncFanOut)
		for _, j := range jobs {
			wg.Add(1)
			sem <- struct{}{}
			go func(j job) {
				defer func() { <-sem; wg.Done() }()
				a.identities.write.Lock()
				defer a.identities.write.Unlock()
				a.identities.mu.Lock()
				stale := a.identities.generation != generation
				a.identities.mu.Unlock()
				if stale {
					return
				}
				did, err := g.SetIdentity(j.dir, j.id.Name, j.id.Email)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					failed = append(failed, tildePath(j.dir))
					return
				}
				if did {
					changed++
				}
				a.identities.mu.Lock()
				a.identities.written[j.dir] = j.id
				a.identities.mu.Unlock()
			}(j)
		}
		wg.Wait()
		a.tv.QueueUpdateDraw(func() {
			if len(failed) > 0 {
				a.flash("the git identity could not be written into " + strings.Join(failed, ", ") + " - is the clone's .git/config writable?")
			}
			if done != nil {
				done(changed)
			}
		})
	}()
}

// identityChanged is what Settings does after an identity is saved for a
// server (path ""), a group or a repository - or for everything, with an
// instance of "": every clone below it gets it now, and the user is told how
// many clones that is.
func (a *App) identityChanged(instance, path, subject string) {
	below := 0
	for key, info := range a.disk {
		if info.Cloned && (instance == "" || key.Instance == instance && (path == "" || key.Path == path || strings.HasPrefix(key.Path, path+"/"))) {
			below++
		}
	}
	a.applyIdentities(func(int) {
		switch below {
		case 0:
			a.done("Git identity of " + subject + " saved")
		case 1:
			a.done("Git identity of " + subject + " saved and written into its 1 clone")
		default:
			a.done(fmt.Sprintf("Git identity of %s saved and written into its %d clones", subject, below))
		}
	})
}

// showIdentityForm sets who commits are made as for a server (path ""), a
// group or a repository, and so for everything below it. A blank field
// inherits.
func (a *App) showIdentityForm(instance, path, subject string) {
	own := a.cfg.OwnIdentity(instance, path)
	inherited := a.cfg.InheritedIdentity(instance, path)

	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Name", own.Name, 0, nil, nil)
	form.AddInputField("Email", own.Email, 0, nil, nil)
	note := "Blank inherits git's global one."
	if !inherited.Empty() {
		note = "Blank inherits " + inherited.String() + "."
	}
	form.AddTextView("", note+"\nWritten into the clones at once; their\nworktrees and agents commit as it.", 0, 3, true, false)

	apply := func(id config.GitIdentity) {
		a.cfg.SetIdentity(instance, path, id)
		a.saveConfig()
		a.closeModal(pageForm)
		if a.settings != nil {
			a.settings.reload()
		}
		a.identityChanged(instance, path, subject)
	}
	text := func(i int) string { return form.GetFormItem(i).(*tview.InputField).GetText() }
	form.AddButton("Save", func() { apply(config.GitIdentity{Name: text(0), Email: text(1)}) })
	form.AddButton("Inherit", func() { apply(config.GitIdentity{}) })
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModal("Git identity · "+subject, form, 13)
}
