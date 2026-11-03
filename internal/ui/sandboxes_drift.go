package ui

import (
	"fmt"
	"strings"

	"github.com/tobola/unagit/internal/unibox"
)

// A sandbox is made from its env as the env was then. When the team's
// repository the env comes from moves on - a pull, a rebase - the sandbox
// is older than its env until it is recreated, and nothing says so before
// the next run or the doctor. Only unibox's doctor can tell, by comparing
// what it recorded at the creation, so unagit asks it right after it moved
// such a repository itself, offers to recreate what is behind, and keeps
// what the doctor said for the Sandboxes tab's count.

// driftOf are the sandboxes the doctor's checks say are older than their
// env: those whose fix is recreating them.
func driftOf(checks []unibox.DoctorCheck) map[string]bool {
	out := map[string]bool{}
	for _, ch := range checks {
		if ch.Status == "pass" {
			continue
		}
		if fix := fixCommand(ch.Hint); len(fix) == 3 && fix[0] == "sandbox" && fix[1] == "recreate" {
			out[fix[2]] = true
		}
	}
	return out
}

// setDrift keeps what the doctor said, for the tab and the envs.
func (a *App) setDrift(checks []unibox.DoctorCheck) {
	a.ubxDrift = driftOf(checks)
	a.drawTabs()
	if a.sandboxesPane != nil {
		a.sandboxesPane.reload()
	}
}

// drifted says whether a sandbox unibox has is older than its env, as the
// doctor last said.
func (a *App) drifted(sandbox string) bool {
	if !a.ubxDrift[sandbox] {
		return false
	}
	_, ok := a.ubx.overview.Sandbox(sandbox)
	return ok
}

// sandboxProblems is how many things stand between the sandboxes and a
// start, as far as unagit knows without asking: an env that does not load
// or lacks a value of the person's, a sandbox older than its env, unibox
// not answering at all. The Sandboxes tab wears the count on every screen.
func (a *App) sandboxProblems() int {
	if !a.uniboxOn() || !a.ubx.read {
		return 0
	}
	if a.ubx.err != nil {
		return 1
	}
	n := 0
	for _, e := range a.ubx.overview.Envs {
		if e.Error != "" || len(e.Missing) > 0 {
			n++
		}
	}
	for name := range a.ubxDrift {
		if a.drifted(name) {
			n++
		}
	}
	return n
}

// envRepoDirs are the directories among dirs that an env of unibox's is
// read from, as the overview last said.
func (a *App) envRepoDirs(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if len(a.envsOfRepos([]string{d})) > 0 {
			out = append(out, d)
		}
	}
	return out
}

// envsOfRepos are the envs read from one of dirs.
func (a *App) envsOfRepos(dirs []string) []unibox.Env {
	if !a.uniboxOn() || !a.ubx.read || a.ubx.err != nil {
		return nil
	}
	var out []unibox.Env
	for _, e := range a.ubx.overview.Envs {
		if e.Repo == "" {
			continue
		}
		for _, d := range dirs {
			if resolved(e.Repo) == resolved(d) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// checkDriftAfterMove asks the doctor, behind the interface, whether the
// sandboxes of envs are older than them now, and offers to recreate those
// that are.
func (a *App) checkDriftAfterMove(envs []unibox.Env) {
	if len(envs) == 0 {
		return
	}
	c, err := a.uniboxClient()
	if err != nil {
		return
	}
	var names []string
	for _, e := range envs {
		names = append(names, e.Name)
	}
	job := a.startJob("Asking unibox's doctor about " + strings.Join(names, ", "))
	go func() {
		checks, err := c.Doctor()
		a.tv.QueueUpdateDraw(func() {
			a.endJob(job)
			if err != nil {
				a.flash("unibox's doctor could not say whether the sandboxes of " + strings.Join(names, ", ") + " are older than their envs now: " + err.Error())
				return
			}
			a.setDrift(checks)
			var behind, envsBehind []string
			for _, e := range envs {
				if name := firstOf(e.Sandbox, e.Name); a.drifted(name) {
					behind = append(behind, name)
					envsBehind = append(envsBehind, e.Name)
				}
			}
			if len(behind) > 0 {
				a.offerRecreate(envsBehind, behind)
			}
		})
	}()
}

// offerRecreate asks to recreate sandboxes their envs changed under. Over
// a dialog it only says so, since a question popping up over what the
// person is doing would take the keys they are typing; the tab keeps
// counting it.
func (a *App) offerRecreate(envs, sandboxes []string) {
	what := "sandbox " + sandboxes[0]
	if len(sandboxes) > 1 {
		what = "sandboxes " + strings.Join(sandboxes, ", ")
	}
	if a.modalOpen() {
		a.note(fmt.Sprintf("env %s changed under %s - Recreate Sandbox… on the Sandboxes tab", strings.Join(envs, ", "), what))
		return
	}
	made := what + " were"
	if len(sandboxes) == 1 {
		made = what + " was"
	}
	body := fmt.Sprintf("The repository moved on: env %s changed, and %s made from it as it was. Recreate now?\n\nThe tasks stay: mounts are made again, clones cloned again; work not pushed makes unibox refuse. sbx asks in this terminal to approve the env.",
		strings.Join(envs, ", "), made)
	warnings := []string{"it discards what a sandbox holds outside its tasks"}
	for _, name := range sandboxes {
		if s, ok := a.ubx.overview.Sandbox(name); ok && len(s.Runs) > 0 {
			warnings = append(warnings, fmt.Sprintf("sandbox %s has %s open, which recreating ends", name, counted(len(s.Runs), "agent session", "agent sessions")))
		}
	}
	a.confirmChoices("Env changed", body, warnings, []choice{{"Recreate", func() {
		a.closeModal(pageConfirm)
		a.recreateDrifted(sandboxes)
	}}})
}

// recreateDrifted recreates the sandboxes one after another, in this
// terminal, each no longer counted as older once it is made again.
func (a *App) recreateDrifted(names []string) {
	if len(names) == 0 {
		return
	}
	name := names[0]
	a.uniboxHereThen("Recreating "+name, func() {
		delete(a.ubxDrift, name)
		a.drawTabs()
		a.recreateDrifted(names[1:])
	}, "sandbox", "recreate", name)
}
