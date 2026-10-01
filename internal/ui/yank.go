package ui

import (
	"fmt"
	"strings"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/workspace"
)

// copyToClipboard hands text to the system clipboard. Tests replace it.
var copyToClipboard = workspace.CopyToClipboard

// yankItem is one thing the copy menu offers: what it is, and the text.
type yankItem struct{ what, text string }

// showYank offers what can be copied about the row under the cursor, the
// link first, so yy is the common case.
func (a *App) showYank(title string, items []yankItem) {
	width := 0
	for _, it := range items {
		if it.text != "" {
			width = max(width, len(it.what))
		}
	}
	var picks []pickItem
	for _, it := range items {
		if it.text == "" {
			continue
		}
		// Padded, so the values start in one column and read as a table.
		picks = append(picks, pickItem{Label: fmt.Sprintf("%-*s", width, it.what), Sub: esc(it.text), Data: it})
	}
	if len(picks) == 0 {
		a.flash("nothing to copy here")
		return
	}
	// The link is under the cursor, so y again copies it: yy, as in vim.
	a.showPickerWith(title, picks, pickerOptions{again: 'y'}, func(p pickItem) {
		it := p.Data.(yankItem)
		a.yank(it.what, it.text)
	})
}

// yank puts text on the clipboard. Without a clipboard program - over ssh,
// typically - the terminal is asked to do it (OSC 52), which most do; the note
// says which happened, since the second cannot be confirmed.
func (a *App) yank(what, text string) {
	if err := copyToClipboard(text); err == nil {
		a.note(fmt.Sprintf("copied %s: %s", strings.ToLower(what), text))
		return
	}
	if a.screen == nil {
		a.flash("no clipboard: install pbcopy, wl-copy or xclip")
		return
	}
	a.screen.SetClipboard([]byte(text))
	a.note(fmt.Sprintf("sent %s to the terminal's clipboard: %s", strings.ToLower(what), text))
}

// mrReference is how the forge itself writes a merge request in text.
func (a *App) mrReference(mr forge.MergeRequest) string {
	sep := "!"
	if inst := a.cfg.Instance(mr.Instance); inst != nil && inst.IsGitHub() {
		sep = "#"
	}
	return a.projectPathOfMR(mr) + sep + fmt.Sprint(mr.IID)
}

func (a *App) yankMR(mr forge.MergeRequest) {
	path := a.projectPathOfMR(mr)
	items := []yankItem{
		{"Link", mr.WebURL},
		{"Reference", a.mrReference(mr)},
		{"Source branch", mr.SourceBranch},
		{"Title", mr.Title},
	}
	if mr.WebURL != "" {
		items = append(items, yankItem{"Markdown link", fmt.Sprintf("[%s](%s)", mr.Title, mr.WebURL)})
	}
	d := a.disk[projectKey{mr.Instance, path}].MRs[mr.IID]
	if d.Branch {
		items = append(items, yankItem{"Branch worktree", a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)})
	}
	if d.Review {
		items = append(items, yankItem{"Review worktree", a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)})
	}
	a.showYank("Copy "+a.mrReference(mr), items)
}

func (a *App) yankProject(pr forge.Project) {
	items := []yankItem{
		{"Link", pr.WebURL},
		{"Path", pr.PathWithNamespace},
		{"Clone address", a.newManager(pr.Instance, pr.PathWithNamespace, nil).RemoteURL(pr)},
	}
	if a.disk[projectKey{pr.Instance, pr.PathWithNamespace}].Cloned {
		items = append(items, yankItem{"Directory", a.projectDir(pr.Instance, pr.PathWithNamespace)})
	}
	a.showYank("Copy "+pr.PathWithNamespace, items)
}

func (a *App) yankWorktree(r worktreeRow) {
	if r.grouped() {
		items := []yankItem{{"Directory", r.Dir}}
		if r.Branch != "" {
			items = append(items, yankItem{"Branch", r.Branch})
		}
		a.showYank("Copy "+r.Path, items)
		return
	}
	items := []yankItem{
		{"Directory", r.Dir},
		{"Branch", r.Branch},
	}
	if mr, ok := a.openMRFor(r); ok {
		items = append(items, yankItem{"Merge request link", mr.WebURL}, yankItem{"Reference", a.mrReference(mr)})
	}
	a.showYank("Copy "+r.Branch, items)
}
