package ui

import (
	"os/exec"

	"github.com/tobola/unagit/internal/chezmoi"
	"github.com/tobola/unagit/internal/forge"
)

// chezmoiState is chezmoi's checkout and the repository of the index it was
// paired with. It is found again at every start and never saved: which
// repository chezmoi keeps is a fact of this machine, not of the
// configuration, and it changes whenever chezmoi is pointed elsewhere.
type chezmoiState struct {
	checkout chezmoi.Checkout
	repo     projectKey // zero while no repository of the index is it
}

// chezmoiOn says whether the chezmoi integration is on: as chosen in
// Settings, or, until something was chosen, whenever chezmoi is installed.
func (a *App) chezmoiOn() bool {
	if on := a.cfg.Integrations.Chezmoi; on != nil {
		return *on
	}
	_, err := exec.LookPath("chezmoi")
	return err == nil
}

// findChezmoiCheckout asks the installed chezmoi; tests replace it.
func findChezmoiCheckout() (chezmoi.Checkout, error) {
	bin, err := exec.LookPath("chezmoi")
	if err != nil {
		return chezmoi.Checkout{}, err
	}
	return chezmoi.Find(bin)
}

// detectChezmoi looks for chezmoi's checkout off the event loop and pairs it
// with the index when it comes back. With the integration off it forgets the
// checkout at once, so the repository is unagit's to clone again.
func (a *App) detectChezmoi() {
	if !a.chezmoiOn() {
		a.chezmoi.Store(nil)
		a.chezmoiProblem = ""
		a.afterChezmoi()
		return
	}
	find := a.findChezmoi
	if find == nil {
		find = findChezmoiCheckout
	}
	go func() {
		checkout, err := find()
		a.tv.QueueUpdateDraw(func() {
			if !a.chezmoiOn() {
				return
			}
			a.chezmoiProblem = ""
			if err != nil {
				a.chezmoi.Store(nil)
				a.chezmoiProblem = err.Error()
			} else {
				a.chezmoi.Store(&chezmoiState{checkout: checkout})
				a.pairChezmoi()
			}
			a.afterChezmoi()
		})
	}()
}

// afterChezmoi shows what a change of the checkout changed: where a
// repository is on disk, its row and the integration's card.
func (a *App) afterChezmoi() {
	if a.projectsPane == nil {
		return
	}
	a.refreshDisk()
	a.projectsPane.reload()
	a.mrsPane.reload()
	a.settings.integrations.paintFocus(a.settings.integrations.active)
}

// pairChezmoi finds the repository of the index chezmoi's origin points at,
// by every address the server gave for it and, when it gave none, by the
// one unagit would clone from.
func (a *App) pairChezmoi() {
	st := a.chezmoi.Load()
	if st == nil {
		return
	}
	paired := &chezmoiState{checkout: st.checkout}
	for _, p := range a.projects {
		if a.isChezmoiOrigin(p, st.checkout.Origin) {
			paired.repo = projectKey{p.Instance, p.PathWithNamespace}
			break
		}
	}
	a.chezmoi.Store(paired)
}

func (a *App) isChezmoiOrigin(p forge.Project, origin string) bool {
	for _, addr := range []string{p.HTTPURLToRepo, p.SSHURLToRepo, p.WebURL} {
		if chezmoi.Same(addr, origin) {
			return true
		}
	}
	if inst := a.cfg.Instance(p.Instance); inst != nil && p.HTTPURLToRepo == "" && p.SSHURLToRepo == "" {
		return chezmoi.Same(inst.URL+"/"+p.PathWithNamespace, origin)
	}
	return false
}

// managedDir is chezmoi's checkout when it is this repository, else "". It
// is read from task goroutines as well as the loop, hence the atomic.
func (a *App) managedDir(instanceID, projectPath string) string {
	if st := a.chezmoi.Load(); st != nil && st.repo == (projectKey{instanceID, projectPath}) && projectPath != "" {
		return st.checkout.Dir
	}
	return ""
}

// chezmoiBadge is not a tag - nobody puts it on or takes it off, and it is
// never saved - so it is told apart without shouting: a pill like the tags,
// but dark ink on a middle grey where every tag is light on a deep colour,
// and always first. It shortens rather than be counted away when the column
// is narrow.
var chezmoiColour = tagColour{name: "chezmoi", ink: "#1c1b19", fill: "#8f8f8f"}

func chezmoiBadge(room int, style, behind string) (string, int) {
	for _, text := range []string{"↗ Managed by Chezmoi", "↗ Chezmoi", "↗"} {
		if markup, w := pillOf(text, chezmoiColour, style, behind); w <= room {
			return markup, w
		}
	}
	return "", 0
}

// chezmoiFound is the line of the integration's card that says what was
// found: the checkout and the repository it was paired with, or why not.
func (a *App) chezmoiFound() string {
	if !a.chezmoiOn() {
		return ""
	}
	if a.chezmoiProblem != "" {
		return tag(colWarn) + esc(a.chezmoiProblem) + tagEnd
	}
	st := a.chezmoi.Load()
	if st == nil {
		return tag(colDim) + "looking for chezmoi's repository…" + tagEnd
	}
	line := tag(colText) + esc(tildePath(st.checkout.Dir)) + tagEnd + " → "
	if st.repo.Path == "" {
		// The key, not the address: an https origin can carry a token.
		return line + tag(colWarn) + esc(chezmoi.Key(st.checkout.Origin)) + " is in no list - refresh with p" + tagEnd
	}
	return line + tag(colOn) + esc(a.instanceLabel(st.repo.Instance)+" · "+st.repo.Path) + tagEnd
}

// chezmoiLine heads the detail of the repository chezmoi keeps with its
// badge, and says where its checkout and its worktrees are.
func (a *App) chezmoiLine(d *detailBuf, pr forge.Project) {
	dir := a.managedDir(pr.Instance, pr.PathWithNamespace)
	if dir == "" {
		return
	}
	// The detail has room to be plain about it: a square heading in amber,
	// not the quiet pill of the list.
	d.blank()
	d.raw("[#1c1b19:#f2b55c:b] ↗ Managed by Chezmoi [-:-:-]\n")
	d.kv("Checkout", esc(tildePath(dir)))
	d.kv("Worktrees", esc(tildePath(a.pathManager(pr.Instance, pr.PathWithNamespace).MRRoot(pr.PathWithNamespace))))
}
