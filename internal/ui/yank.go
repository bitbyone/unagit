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
		picks = append(picks, pickItem{Label: fmt.Sprintf("%-*s", width, it.what), Sub: esc(firstLine(it.text) + moreLines(it.text)), Data: it})
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
	said := firstLine(text) + moreLines(text)
	if err := copyToClipboard(text); err == nil {
		a.done(fmt.Sprintf("copied %s: %s", strings.ToLower(what), said))
		return
	}
	if a.screen == nil {
		a.flash("no clipboard: install pbcopy, wl-copy or xclip")
		return
	}
	a.screen.SetClipboard([]byte(text))
	a.done(fmt.Sprintf("sent %s to the terminal's clipboard: %s", strings.ToLower(what), said))
}

// moreLines says how many lines follow the first of a text, "" for one.
func moreLines(text string) string {
	if n := strings.Count(text, "\n"); n > 0 {
		return fmt.Sprintf(" (+%d more)", n)
	}
	return ""
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
		{"Link with text", linkWithText(mr.WebURL, path, a.mrReference(mr)[len(path):], trim(mr.Title, 60), mr.SourceBranch)},
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
		{"Link with text", linkWithText(pr.WebURL, pr.PathWithNamespace, trim(pr.Description, 60))},
		{"Path", pr.PathWithNamespace},
		{"Clone address", a.newManager(pr.Instance, pr.PathWithNamespace, nil).RemoteURL(pr)},
	}
	if a.disk[projectKey{pr.Instance, pr.PathWithNamespace}].Cloned {
		items = append(items, yankItem{"Directory", a.projectDir(pr.Instance, pr.PathWithNamespace)})
	}
	a.showYank("Copy "+pr.PathWithNamespace, items)
}

// yankProjects copies one thing of each of several repositories, one a line.
func (a *App) yankProjects(picked []forge.Project) {
	lines := func(of func(pr forge.Project) string) string {
		var out []string
		for _, pr := range picked {
			if s := of(pr); s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, "\n")
	}
	a.showYank("Copy "+counted(len(picked), "repository", "repositories"), []yankItem{
		{"Links", lines(func(pr forge.Project) string { return pr.WebURL })},
		{"Paths", lines(func(pr forge.Project) string { return pr.PathWithNamespace })},
		{"Clone addresses", lines(func(pr forge.Project) string {
			return a.newManager(pr.Instance, pr.PathWithNamespace, nil).RemoteURL(pr)
		})},
		{"Directories", lines(func(pr forge.Project) string {
			if !a.disk[projectKey{pr.Instance, pr.PathWithNamespace}].Cloned {
				return ""
			}
			return a.projectDir(pr.Instance, pr.PathWithNamespace)
		})},
	})
}

// yankMRs copies one thing of each of several merge requests, one a line.
func (a *App) yankMRs(picked []forge.MergeRequest) {
	lines := func(of func(mr forge.MergeRequest) string) string {
		var out []string
		for _, mr := range picked {
			if s := of(mr); s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, "\n")
	}
	a.showYank("Copy "+counted(len(picked), "merge request", "merge requests"), []yankItem{
		{"Links", lines(func(mr forge.MergeRequest) string { return mr.WebURL })},
		{"References", lines(func(mr forge.MergeRequest) string { return a.mrReference(mr) })},
		{"Source branches", lines(func(mr forge.MergeRequest) string { return mr.SourceBranch })},
		{"Titles", lines(func(mr forge.MergeRequest) string { return mr.Title })},
		{"Markdown links", lines(func(mr forge.MergeRequest) string {
			if mr.WebURL == "" {
				return ""
			}
			return fmt.Sprintf("[%s](%s)", mr.Title, mr.WebURL)
		})},
	})
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

// linkWithText is a line to paste into a chat: what it is, in words, then
// the link, which the chat makes clickable. Empty words are left out, and
// without a link there is nothing to paste.
func linkWithText(url string, words ...string) string {
	if url == "" {
		return ""
	}
	var kept []string
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " · ") + " " + url
}
