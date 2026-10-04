package ui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// A new repository is made where the others are: in one of the groups picked
// in Settings, or a subgroup of one taken with its subgroups. It is cloned at
// once and joins the list, so from then on it is one of the others.

// repoHome is a group a repository can be made in.
type repoHome struct {
	inst  config.Instance
	group forge.Group
}

func (h repoHome) label(multi bool) string {
	if multi {
		return h.inst.Label() + " · " + h.group.FullPath
	}
	return h.group.FullPath
}

// repoHomes are the groups a repository can be made in: those picked in
// Settings on a server with a token, and the subgroups known below one picked
// with its subgroups.
func (a *App) repoHomes() []repoHome {
	var homes []repoHome
	seen := map[string]bool{}
	add := func(inst config.Instance, g forge.Group) {
		key := inst.ID + "\x00" + g.FullPath
		if !seen[key] {
			seen[key] = true
			homes = append(homes, repoHome{inst: inst, group: g})
		}
	}
	for _, inst := range a.cfg.Instances {
		if a.client(inst.ID) == nil {
			continue
		}
		for _, g := range inst.Groups {
			add(inst, forgeGroup(g))
			if !g.IncludesSubgroups() || inst.IsGitHub() {
				continue
			}
			for _, sub := range a.groups {
				if sub.Instance == inst.ID && strings.HasPrefix(sub.FullPath, g.FullPath+"/") {
					add(inst, sub)
				}
			}
		}
	}
	sort.SliceStable(homes, func(i, j int) bool {
		if homes[i].inst.ID != homes[j].inst.ID {
			return false
		}
		return homes[i].group.FullPath < homes[j].group.FullPath
	})
	return homes
}

// The choices of the form, as shown and as the forge names them.
var (
	visibilityChoices = []struct{ label, value string }{
		{"private", forge.VisibilityPrivate},
		{"internal · GitLab; private on GitHub", forge.VisibilityInternal},
		{"public", forge.VisibilityPublic},
	}
	licenseChoices = []struct{ label, value string }{
		{"none", ""},
		{"MIT", "mit"},
		{"Apache 2.0", "apache-2.0"},
		{"GPL 3.0", "gpl-3.0"},
		{"LGPL 3.0", "lgpl-3.0"},
		{"BSD 3-Clause", "bsd-3-clause"},
		{"MPL 2.0", "mpl-2.0"},
		{"Unlicense", "unlicense"},
	}
	gitignoreChoices = []string{"none", "Go", "Node", "Python", "Java", "Gradle", "Maven", "Rust", "Swift"}
)

func labelsOf(choices []struct{ label, value string }) []string {
	out := make([]string, len(choices))
	for i, c := range choices {
		out[i] = c.label
	}
	return out
}

// Labels of the new repository form, which the code finds its fields by.
const (
	labelRepoWhere      = "Where"
	labelRepoName       = "Name"
	labelRepoDesc       = "Description"
	labelRepoVisibility = "Visibility"
	labelRepoBranch     = "First branch"
	labelRepoReadme     = "README"
	labelRepoLicense    = "License"
	labelRepoGitignore  = ".gitignore"
)

// showNewRepository asks what the new repository is and where it goes.
func (a *App) showNewRepository() {
	homes := a.repoHomes()
	if len(homes) == 0 {
		a.flash("pick a group in Settings › Groups first, on a server with a token")
		return
	}
	multi := a.multiInstance()
	where := make([]string, len(homes))
	at := 0
	// The group of the repository under the cursor is the likely one.
	current, path := a.selectedProjectOf(a.projectsPane)
	for i, h := range homes {
		where[i] = h.label(multi)
		if h.inst.ID == current && namespaceOf(path) == h.group.FullPath {
			at = i
		}
	}

	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(labelRepoName, "", 0, nil, nil)
	addSelect(form, labelRepoWhere, where, at)
	form.AddInputField(labelRepoDesc, "", 0, nil, nil)
	addSelect(form, labelRepoVisibility, labelsOf(visibilityChoices), 0)
	form.AddInputField(labelRepoBranch, "main", 0, nil, nil)
	addCheckbox(form, labelRepoReadme, true)
	addSelect(form, labelRepoLicense, labelsOf(licenseChoices), 0)
	addSelect(form, labelRepoGitignore, gitignoreChoices, 0)

	text := func(label string) string {
		return strings.TrimSpace(form.GetFormItemByLabel(label).(*tview.InputField).GetText())
	}
	choice := func(label string) int {
		i, _ := form.GetFormItemByLabel(label).(*tview.DropDown).GetCurrentOption()
		return max(i, 0)
	}
	create := func() {
		req := forge.NewProject{
			Name:          text(labelRepoName),
			Description:   text(labelRepoDesc),
			Visibility:    visibilityChoices[choice(labelRepoVisibility)].value,
			Readme:        form.GetFormItemByLabel(labelRepoReadme).(*tview.Checkbox).IsChecked(),
			License:       licenseChoices[choice(labelRepoLicense)].value,
			DefaultBranch: text(labelRepoBranch),
		}
		if g := choice(labelRepoGitignore); g > 0 {
			req.Gitignore = gitignoreChoices[g]
		}
		switch {
		case req.Name == "":
			a.flash("enter a name")
			return
		case strings.ContainsAny(req.DefaultBranch, " ~^:?*[\\"):
			a.flash("the first branch cannot be called that - git refuses it")
			return
		}
		a.closeModal(pageForm)
		a.createRepository(homes[choice(labelRepoWhere)], req)
	}
	form.AddButton("Create", create)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("New repository · cloned once made", form, 76, 21)
}

// createRepository makes the repository, lists it, and clones it. The list
// takes it in before the clone, so a clone that fails leaves it listed and
// one C away from being on disk.
func (a *App) createRepository(home repoHome, req forge.NewProject) {
	client := a.client(home.inst.ID)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [4]", home.inst.Label())
		return
	}
	var made projectKey
	a.runTaskThen("Creating "+home.group.FullPath+"/"+req.Name, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		p, err := client.CreateProject(ctx, home.group, req)
		if p == nil {
			return "", err
		}
		// Made but not quite as asked - a template that did not come - is
		// still made, and is listed and cloned like any other.
		if err != nil {
			log("! " + err.Error())
		}
		p.Instance = home.inst.ID
		project := *p
		log("created " + project.WebURL)
		done := make(chan struct{})
		a.tv.QueueUpdateDraw(func() {
			a.adoptProject(project)
			made = projectKey{project.Instance, project.PathWithNamespace}
			close(done)
		})
		<-done
		return a.newManager(project.Instance, project.PathWithNamespace, log).CloneProject(project)
	}, func(string) {
		a.refreshDisk()
		a.switchTab(pageProjects)
		a.projectsPane.reload()
		a.selectProject(made)
		a.done("created and cloned " + made.Path)
	})
}

// adoptProject adds a repository the forge just created to the list and to
// the saved index, leaving the time the list was indexed as it was. It runs
// on the event loop.
func (a *App) adoptProject(p forge.Project) {
	for _, have := range a.projects {
		if have.Instance == p.Instance && have.PathWithNamespace == p.PathWithNamespace {
			return
		}
	}
	if p.LastActivityAt.IsZero() {
		p.LastActivityAt = time.Now()
	}
	a.projects = append(a.projects, p)
	a.reindexProjects()
	_ = index.Save(config.IndexPath("projects"), index.Projects{
		Version: index.Version, UpdatedAt: a.projUpdated, Items: a.projects})
	// The marks point into the list as it was.
	a.projectsPane.marks = nil
	a.projectsPane.reload()
}

// selectProject puts the cursor of Repositories on a repository, when it is
// in the list as filtered.
func (a *App) selectProject(want projectKey) {
	t := a.projectsPane.table
	for row := 1; row < t.GetRowCount(); row++ {
		i, ok := t.GetCell(row, 0).GetReference().(int)
		if ok && i < len(a.projects) && a.projects[i].Instance == want.Instance &&
			a.projects[i].PathWithNamespace == want.Path {
			t.Select(row, 0)
			return
		}
	}
}
