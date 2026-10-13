package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rivo/tview"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

func (a *App) showPublishFolder() { a.showPublishSource("") }

func (a *App) showPublishSource(dir string) {
	a.returnTo = nil
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Directory", dir, 0, nil, nil)
	field := form.GetFormItemByLabel("Directory").(*tview.InputField)
	form.AddButton("Continue", func() {
		dir := config.Expand(field.GetText())
		if dir == "" {
			a.flash("enter a directory")
			return
		}
		var folder workspace.LocalFolder
		var pending *workspace.Publication
		a.loadThen("Inspecting local folder", func(func(string)) (string, error) {
			var err error
			folder, err = workspace.InspectLocalFolder(dir)
			if err != nil {
				return "", err
			}
			pending, err = workspace.FindPublication(a.cfg.Dir(), folder.Directory)
			return "", err
		}, func(string) {
			proceed := func() {
				if pending != nil {
					a.resumePublication(pending)
					return
				}
				if len(folder.Remotes) != 0 {
					a.flash("this repository already has a remote; choose a folder without remotes")
					return
				}
				a.showPublishDetails(folder)
			}
			if !sameDirectory(config.Expand(dir), folder.Directory) {
				a.confirmWith("Use repository root", "This folder belongs to:\n"+esc(folder.Directory)+"\n\nPublish that repository?", "Use root", nil, proceed)
			} else {
				proceed()
			}
		})
	})
	if a.yaziOn() {
		form.AddButton("Browse", func() { a.choosePublishDirectory(field) })
	}
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Publish local folder", form, 80, 8)
}

// The existing browser owns suspension and temporary files. Directory picking
// consumes its final cwd instead of handing a chosen file to an editor.
func (a *App) choosePublishDirectory(field *tview.InputField) {
	bin, ok := a.yaziBinary()
	if !ok {
		return
	}
	dir := config.Expand(field.GetText())
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir, _ = os.Getwd()
	}
	go func() {
		a.editorMu.Lock()
		_, last, err := a.runYaziSelection(bin, dir, session.Record{Dir: dir})
		a.editorMu.Unlock()
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("yazi: %v", err)
				return
			}
			if last != "" {
				field.SetText(last)
			}
		})
	}()
}

func (a *App) showPublishDetails(folder workspace.LocalFolder) { a.showPublishDetailsWith(folder, nil) }

func (a *App) showPublishDetailsWith(folder workspace.LocalFolder, draft *workspace.Publication) {
	homes := a.repoHomes()
	if len(homes) == 0 {
		a.flash("pick a group in Settings › Groups first, on a server with a token")
		return
	}
	where := make([]string, len(homes))
	for i, h := range homes {
		where[i] = h.label(a.multiInstance())
	}
	form := tview.NewForm()
	styleForm(form)
	addRepositoryIdentity(form, where, 0, filepath.Base(folder.Directory), folder.Branch)
	addCheckbox(form, "All branches", false)
	addCheckbox(form, "Tags", false)
	form.AddTextView("Source", esc(folder.Directory), 0, 2, true, false)
	if draft != nil {
		form.GetFormItemByLabel(labelRepoName).(*tview.InputField).SetText(draft.Request.Name)
		form.GetFormItemByLabel(labelRepoDesc).(*tview.InputField).SetText(draft.Request.Description)
		form.GetFormItemByLabel(labelRepoBranch).(*tview.InputField).SetText(draft.Branch)
		for i, h := range homes {
			if h.inst.ID == draft.Instance && h.group.FullPath == draft.Group.FullPath {
				form.GetFormItemByLabel(labelRepoWhere).(*tview.DropDown).SetCurrentOption(i)
			}
		}
		for i, v := range visibilityChoices {
			if v.value == draft.Request.Visibility {
				form.GetFormItemByLabel(labelRepoVisibility).(*tview.DropDown).SetCurrentOption(i)
			}
		}
		form.GetFormItemByLabel("All branches").(*tview.Checkbox).SetChecked(draft.AllBranches)
		form.GetFormItemByLabel("Tags").(*tview.Checkbox).SetChecked(draft.Tags)
	}
	read := func() (*workspace.Publication, bool) {
		text := func(label string) string {
			return strings.TrimSpace(form.GetFormItemByLabel(label).(*tview.InputField).GetText())
		}
		name, branch := text(labelRepoName), text(labelRepoBranch)
		if name == "" || strings.ContainsAny(name, "/\\ \t\n") || name == "." || name == ".." {
			a.flash("enter a repository name without spaces or slashes")
			return nil, false
		}
		if folder.Repository && branch != folder.Branch {
			a.flash("keep the existing branch name; rename it in the repository first if needed")
			return nil, false
		}
		at, _ := form.GetFormItemByLabel(labelRepoWhere).(*tview.DropDown).GetCurrentOption()
		vis, _ := form.GetFormItemByLabel(labelRepoVisibility).(*tview.DropDown).GetCurrentOption()
		p := &workspace.Publication{Directory: folder.Directory, Instance: homes[at].inst.ID, Group: homes[at].group, Branch: branch,
			Request:     forge.NewProject{Name: name, Description: text(labelRepoDesc), DefaultBranch: branch, Visibility: visibilityChoices[vis].value},
			AllBranches: form.GetFormItemByLabel("All branches").(*tview.Checkbox).IsChecked(), Tags: form.GetFormItemByLabel("Tags").(*tview.Checkbox).IsChecked()}
		return p, true
	}
	form.AddButton("Continue", func() {
		p, ok := read()
		if !ok {
			return
		}
		// Initialisation is local, reversible, and happens before any server request.
		a.runTaskThen("Preparing local repository", func(func(string)) (string, error) {
			return "", workspace.InitLocalFolder(p.Directory, p.Branch)
		}, func(string) {
			a.leaveReturn("Publish destination", func() { a.showPublishDetailsWith(folder, p) }, nil)
			a.showPublicationReady(p)
		})
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.leaveReturn("Publish local folder", func() { a.showPublishSource(folder.Directory) }, nil)
	a.showFormModalSized("Publish local folder · destination", form, 84, 23)
}

// reviewWidth is the width the publication review asks for.
const reviewWidth = 88

func (a *App) showPublicationReady(p *workspace.Publication) {
	var folder workspace.LocalFolder
	var changes []gitx.Change
	a.loadThen("Reading local history", func(func(string)) (string, error) {
		var err error
		folder, err = workspace.InspectLocalFolder(p.Directory)
		if err != nil {
			return "", err
		}
		changes, err = gitx.New("", nil).Changes(p.Directory)
		return "", err
	}, func(string) {
		form := tview.NewForm()
		styleForm(form)
		planned := a.cloneDir(p.Instance, p.Group.FullPath+"/"+p.Request.Name)
		// One line a row, the way a detail pane reads: what matters in its
		// colour, the words around it muted.
		scope := "branch " + tag(colBranch) + esc(p.Branch)
		if p.AllBranches {
			scope = tag(colBranch) + "all local branches"
		}
		if p.Tags {
			scope += tag(colMuted) + " and " + tag(colBranch) + "tags"
		}
		if !folder.HasCommits {
			scope = tag(colMuted) + "an empty repository, no commits to push"
		}
		local := tag(colOn) + "nothing uncommitted"
		if len(changes) != 0 {
			local = fmt.Sprintf("%s%d uncommitted %s stay local %sunless committed with Changes", tag(colWarn), len(changes), plural(len(changes), "file", "files"), tag(colMuted))
		}
		form.AddTextView("Remote", tag(colMuted)+esc(a.instanceLabel(p.Instance))+" · "+tag(colAccent)+esc(p.Group.FullPath+"/"+p.Request.Name), 0, 1, true, false)
		form.AddTextView("Publish", tag(colText)+scope, 0, 1, true, false)
		form.AddTextView("Local files", local, 0, 1, true, false)
		// A row is one line, so a path longer than the dialog is shortened by
		// what tells it apart; wrapped, tview would show only its last line.
		room := min(reviewWidth, a.screenWidth()) - len("Planned path") - 7
		form.AddTextView("Source", tag(colText)+esc(shortenPath(tildePath(p.Directory), room)), 0, 1, true, false)
		form.AddTextView("Planned path", tag(colText)+esc(shortenPath(tildePath(planned), room)), 0, 1, true, false)
		form.AddButton("Publish", func() {
			a.closeModal(pageForm)
			a.publishFolder(p)
		})
		if len(changes) != 0 {
			form.AddButton("Changes", func() {
				a.closeModal(pageForm)
				back := func() { a.showPublicationReady(p) }
				a.showChanges(changesPlace{instance: p.Instance, path: p.Group.FullPath + "/" + p.Request.Name, dir: p.Directory, title: filepath.Base(p.Directory), back: back,
					afterCommit: func() { a.closeChanges() }})
			})
		}
		form.AddButton("Cancel", func() { a.closeModal(pageForm) })
		form.SetFocus(form.GetFormItemCount())
		a.showFormModalSized("Publish local folder · review", form, reviewWidth, 16)
	})
}

func (a *App) resumePublication(p *workspace.Publication) {
	if a.client(p.Instance) == nil {
		a.flash("connect the publication's server in Settings before continuing")
		return
	}
	if p.Complete {
		if err := workspace.New(workspace.Options{LocalDirectory: p.Directory}, nil).CheckLocalProject(*p.Project); err != nil {
			a.errorf("%v", err)
			return
		}
		a.closeModal(pageForm)
		a.offerPublicationMove(p)
		return
	}
	if !p.Requested || p.Project != nil {
		a.showPublicationReady(p)
		return
	}
	var found *forge.Project
	var matching []forge.Project
	client := a.client(p.Instance)
	a.loadThen("Looking for the repository already requested", func(func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		projects, err := client.GroupProjects(ctx, p.Group, false)
		if err != nil {
			return "", err
		}
		for _, pr := range projects {
			if namespaceOf(pr.PathWithNamespace) == p.Group.FullPath && (pr.PathWithNamespace == p.Group.FullPath+"/"+p.Request.Name || pr.Name == p.Request.Name) {
				matching = append(matching, pr)
			}
		}
		return "", nil
	}, func(string) {
		if len(matching) > 1 {
			items := make([]pickItem, len(matching))
			for i, pr := range matching {
				items[i] = pickItem{Label: pr.PathWithNamespace, Sub: pr.WebURL, Data: pr}
			}
			a.showPicker("Recover publication", items, func(item pickItem) {
				pr := item.Data.(forge.Project)
				a.confirmRecoveredPublication(p, &pr)
			})
			return
		}
		if len(matching) == 1 {
			found = &matching[0]
		}
		a.confirmRecoveredPublication(p, found)
	})
}

func (a *App) confirmRecoveredPublication(p *workspace.Publication, found *forge.Project) {
	body, button := "The earlier create request was not confirmed. No matching repository is visible. Retry creating it?", "Retry"
	if found != nil {
		body, button = "The earlier create request was not confirmed. Continue with this repository?\n\n"+esc(found.WebURL)+"\n\nExisting remote history will never be overwritten.", "Use repository"
	}
	a.confirmWith("Recover publication", body, button, nil, func() {
		p.Project = found
		if found != nil {
			p.Project.Instance = p.Instance
		} else {
			p.Requested = false
		}
		if err := p.Save(p.JournalPath(a.cfg.Dir())); err != nil {
			a.errorf("%v", err)
			return
		}
		a.showPublicationReady(p)
	})
}

func (a *App) rememberPublication(p forge.Project, dir string) error {
	old := a.cfg.State.LocalRepository(p.Instance, p.PathWithNamespace)
	a.cfg.State.SetLocalRepository(p.Instance, p.PathWithNamespace, dir)
	if err := a.cfg.SaveState(); err != nil {
		a.cfg.State.SetLocalRepository(p.Instance, p.PathWithNamespace, old)
		return err
	}
	for i := range a.projects {
		if a.projects[i].Instance == p.Instance && a.projects[i].PathWithNamespace == p.PathWithNamespace {
			a.projects[i] = p
		}
	}
	a.adoptProject(p)
	a.reindexProjects()
	// Unlike an ordinary cache update, losing this write must stop publication.
	return index.Save(a.cfg.IndexPath("projects"), index.Projects{Version: index.Version, UpdatedAt: a.projUpdated, Items: a.projects})
}

func (a *App) publishFolder(p *workspace.Publication) {
	client := a.client(p.Instance)
	if client == nil {
		a.flash("connect this server in Settings before publishing")
		return
	}
	mgr := a.newManager(p.Instance, p.Group.FullPath+"/"+p.Request.Name, nil)
	journal := p.JournalPath(a.cfg.Dir())
	a.runTaskThen("Publishing "+filepath.Base(p.Directory), func(log func(string)) (string, error) {
		mgr.Git().Log = log
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := mgr.PublishLocal(ctx, client, p, journal, func(pr forge.Project) error {
			var err error
			a.tv.QueueUpdateDraw(func() { err = a.rememberPublication(pr, p.Directory); a.refreshDisk(); a.projectsPane.reload() })
			return err
		})
		return "", err
	}, func(string) {
		a.refreshDisk()
		a.switchTab(pageProjects)
		a.projectsPane.reload()
		a.selectProject(projectKey{p.Instance, p.Project.PathWithNamespace})
		a.offerPublicationMove(p)
		a.done("published " + p.Project.PathWithNamespace)
	})
}

func (a *App) offerPublicationMove(p *workspace.Publication) {
	if p.MoveSource != "" {
		a.flash("the repository is at " + p.Directory + "; a copy remains at " + p.MoveSource + " after interrupted cleanup; inspect that copy before removing it")
		return
	}

	target := a.cloneDir(p.Instance, p.Project.PathWithNamespace)
	if sameDirectory(p.Directory, target) {
		a.done("published " + p.Project.PathWithNamespace)
		return
	}
	a.returnTo = nil
	form := tview.NewForm()
	styleForm(form)
	form.AddTextView("Current folder", esc(p.Directory), 0, 3, true, false)
	form.AddTextView("Planned folder", esc(target), 0, 3, true, false)
	form.AddTextView("", "Moving keeps all files and local Git history. Close editors and shells using this folder before moving.", 0, 2, true, false)
	form.AddButton("Keep here", func() { a.closeModal(pageForm); a.done("kept " + p.Directory) })
	form.AddButton("Move", func() { a.closeModal(pageForm); a.movePublication(p, target) })
	form.SetFocus(form.GetFormItemCount())
	a.showFormModalSized("Local repository location", form, 88, 19)
}

func (a *App) movePublication(p *workspace.Publication, target string) {
	if a.managedDir(p.Instance, p.Project.PathWithNamespace) != "" {
		a.flash("chezmoi manages this checkout; keep it where it is")
		return
	}
	source := p.Directory
	journal := p.JournalPath(a.cfg.Dir())
	a.runTaskThen("Moving "+p.Project.PathWithNamespace, func(log func(string)) (string, error) {
		for _, r := range a.sessions.List() {
			opened, err := filepath.EvalSymlinks(r.Dir)
			if err != nil {
				continue
			}
			canonical, err := filepath.EvalSymlinks(source)
			if err != nil {
				return "", err
			}
			rel, err := filepath.Rel(canonical, opened)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("%s has this folder open; close that session before moving", r.Editor)
			}
		}
		log("Moving " + source + " to " + target)
		if err := p.MoveLocal(target, journal); err != nil {
			if p.Directory != source {
				var saveErr error
				a.tv.QueueUpdateDraw(func() { saveErr = a.rememberPublication(*p.Project, p.Directory); a.refreshDisk() })
				if saveErr != nil {
					log(saveErr.Error())
				}
			}
			return "", err
		}
		var err error
		a.tv.QueueUpdateDraw(func() { err = a.rememberPublication(*p.Project, p.Directory) })
		return target, err
	}, func(string) {
		a.zoxideRemove(source)
		a.zoxideAdd(p.Directory)
		a.refreshDisk()
		a.projectsPane.reload()
		a.done("moved to " + p.Directory)
	})
}

// Journal entries also recover the gap between a completed move and saving the
// machine's directory map. They never make an unrecognised folder usable.
func (a *App) loadLocalPublications() {
	paths, _ := filepath.Glob(filepath.Join(a.cfg.Dir(), "publications", "*.json"))
	for _, path := range paths {
		p, err := workspace.ReadPublication(path)
		if err != nil || p == nil || p.Project == nil {
			continue
		}
		dir := p.Directory
		if p.MoveTo != "" {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				dir = p.MoveTo
			}
		}
		mgr := workspace.New(workspace.Options{LocalDirectory: dir}, nil)
		if mgr.CheckLocalProject(*p.Project) != nil {
			continue
		}
		a.cfg.State.SetLocalRepository(p.Instance, p.Project.PathWithNamespace, dir)
		exists := false
		for _, pr := range a.projects {
			if pr.Instance == p.Instance && pr.PathWithNamespace == p.Project.PathWithNamespace {
				exists = true
				break
			}
		}
		if !exists {
			a.projects = append(a.projects, *p.Project)
		}
	}
}

func (a *App) manageLocalFolder(pr forge.Project) {
	dir := a.cfg.State.LocalRepository(pr.Instance, pr.PathWithNamespace)
	var p *workspace.Publication
	a.loadThen("Reading publication progress", func(func(string)) (string, error) {
		var err error
		p, err = workspace.FindPublication(a.cfg.Dir(), dir)
		if err == nil && p == nil {
			err = fmt.Errorf("publication progress is missing; restore it before moving this folder")
		}
		return "", err
	}, func(string) { a.resumePublication(p) })
}
