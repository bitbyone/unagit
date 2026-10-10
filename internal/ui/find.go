package ui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// Find Repository… (+, from anywhere) looks through every repository the
// accounts are members of - in the groups not chosen too, the hidden ones
// too - and brings the one chosen into Repositories. A hidden one is shown
// again; one no chosen group has is listed on its own (Instance.Repositories,
// which Settings › Groups lists under its server), never by choosing its
// whole group and hiding the rest, which would let every repository made
// there later in; one listed already is only gone to.
//
// What there is to look through is index-catalogue.json: the picker opens
// on it at once, and reads it again behind itself when it is older than
// catalogueAge, putting the answer in while it is open.

// catalogueAge is how long the repositories read are trusted before the
// picker asks again. Membership changes seldom; a large GitLab takes a
// while to list.
const catalogueAge = 10 * time.Minute

// findState is where a repository of the catalogue stands in unagit.
type findState int

const (
	findNew    findState = iota // in no group chosen, and not chosen on its own
	findListed                  // listed in Repositories
	findHidden                  // listed, but hidden
)

// findStateOf says where a repository stands: listed when a chosen group
// has it, it was chosen on its own, or it is in the list for another
// reason (a starred one cloned); hidden when the filters keep it out.
func (a *App) findStateOf(p forge.Project) findState {
	if a.cfg.Filters.IsHidden(p.Instance, p.PathWithNamespace) {
		return findHidden
	}
	if _, ok := a.projByKey[projectKey{p.Instance, p.PathWithNamespace}]; ok {
		return findListed
	}
	if inst := a.cfg.Instance(p.Instance); inst != nil && inst.Covers(p.PathWithNamespace) {
		return findListed
	}
	return findNew
}

// loadCatalogue reads the repositories last read, once.
func (a *App) loadCatalogue() {
	if a.catalogue != nil {
		return
	}
	if c, err := index.Load[index.Projects](a.cfg.IndexPath("catalogue")); err == nil {
		a.catalogue, a.catalogueAt = c.Items, c.UpdatedAt
	}
}

// showFindRepository opens the picker over every repository there is, the
// filter typing: a repository is looked for by name.
func (a *App) showFindRepository() {
	var instances []config.Instance
	for _, inst := range a.cfg.Instances {
		if a.client(inst.ID) != nil {
			instances = append(instances, inst)
		}
	}
	if len(instances) == 0 {
		a.note("no server with a token - add one in " + settingsTab)
		return
	}
	a.loadCatalogue()
	at := func(it pickItem) forge.Project { return it.Data.(forge.Project) }
	opts := pickerOptions{filter: true, wide: true, explain: true, enterHint: "add",
		enterName: "Add to Unagit", enterAbout: "Show it in Repositories: listed on its own when no group of yours has it, shown again when hidden, gone to when listed already.",
		same: func(x, y pickItem) bool {
			p, q := at(x), at(y)
			return p.Instance == q.Instance && p.PathWithNamespace == q.PathWithNamespace
		},
		keys: []pickKey{
			a.browserKey("w", "browser", "Open in Browser", "The repository's page on the forge; the list stays open.",
				func(it pickItem) string { return at(it).WebURL }),
		}}
	var live *livePicker
	width := 100
	opts.relabel = func(items []pickItem, w int) {
		width = w
		header := a.labelFind(items, w)
		if live != nil {
			live.setHeader(header)
		}
	}
	title, items, header := a.findItems(width)
	opts.header = header
	live = a.showPickerWith(title, items, opts, func(it pickItem) { a.bringIn(at(it)) })
	if time.Since(a.catalogueAt) < catalogueAge && a.catalogue != nil {
		return
	}
	var read []forge.Project
	a.waitInDialog("reading the repositories of your accounts", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, inst := range instances {
			got, err := a.client(inst.ID).MemberProjects(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", inst.Label(), err)
			}
			for i := range got {
				got[i].Instance = inst.ID
			}
			read = append(read, got...)
		}
		return nil
	}, func(stillOpen bool) {
		a.catalogue, a.catalogueAt = index.DedupeProjects(read), time.Now()
		if a.catalogue == nil {
			a.catalogue = []forge.Project{}
		}
		if err := index.Save(a.cfg.IndexPath("catalogue"), index.Projects{Version: index.Version, UpdatedAt: a.catalogueAt, Items: a.catalogue}); err != nil {
			a.errorf("cannot keep the repositories read: %v", err)
		}
		if stillOpen && live.open() {
			title, items, header := a.findItems(width)
			live.setHeader(header)
			live.set(title, items)
		}
	})
}

// findItems are the catalogue's items, the most recently active first,
// their labels written for a picker width cells wide.
func (a *App) findItems(width int) (string, []pickItem, string) {
	all := append([]forge.Project(nil), a.catalogue...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].LastActivityAt.After(all[j].LastActivityAt) })
	items := make([]pickItem, len(all))
	for i, p := range all {
		items[i] = pickItem{About: p.Description, Data: p}
	}
	return fmt.Sprintf("Find Repository · %d", len(all)), items, a.labelFind(items, width)
}

// labelFind writes the rows of the catalogue for a row width cells wide -
// the server when there is more than one, the path, where it stands, when
// it was last active - and gives their header. A path too long is
// shortened by what tells it apart.
func (a *App) labelFind(items []pickItem, width int) string {
	var servers, paths []int
	ageWidth := len("ACTIVE")
	for _, it := range items {
		p := it.Data.(forge.Project)
		if a.multiInstance() {
			servers = append(servers, cells(a.instanceLabel(p.Instance)))
		}
		paths = append(paths, cells(p.PathWithNamespace))
		ageWidth = max(ageWidth, cells(humanAge(p.LastActivityAt)))
	}
	server := flexColumn("SERVER", servers, 6, 0.5)
	if !a.multiInstance() {
		server = fixedColumn(0)
	}
	repo := flexColumn("REPOSITORY", paths, 16, 2)
	state := fixedColumn(cells(glyphHidden + " hidden"))
	age := fixedColumn(ageWidth)
	layoutColumns(width-4, server, repo, state, age)
	rows := make([][]string, len(items))
	for i, it := range items {
		p := it.Data.(forge.Project)
		name := ""
		if a.multiInstance() {
			name = tag(role("column.server")) + esc(a.instanceLabel(p.Instance)) + tagEnd
		}
		where := ""
		switch a.findStateOf(p) {
		case findListed:
			where = tag(colMuted) + "listed" + tagEnd
		case findHidden:
			where = tag(colWarn) + glyphHidden + " hidden" + tagEnd
		}
		rows[i] = []string{name, tag(role("column.name")) + esc(shortenRepo(p.PathWithNamespace, repo.width)) + tagEnd, where,
			tag(colMuted) + humanAge(p.LastActivityAt) + tagEnd}
	}
	header, labels := pickTable([]string{"SERVER", "REPOSITORY", "", "ACTIVE"}, rows)
	for i := range items {
		items[i].Label = labels[i]
	}
	return header
}

// bringIn makes a repository of the catalogue one of Repositories, and
// goes to it: shown again if hidden, chosen on its own if nothing lists
// it, and taken into the list at once rather than at the next refresh,
// its merge requests read behind the interface.
func (a *App) bringIn(p forge.Project) {
	inst := a.cfg.Instance(p.Instance)
	if inst == nil {
		a.flash(a.instanceLabel(p.Instance) + " is no longer configured")
		return
	}
	key := projectKey{p.Instance, p.PathWithNamespace}
	var did string
	if a.cfg.Filters.IsHidden(p.Instance, p.PathWithNamespace) {
		a.cfg.Filters.ToggleHidden(p.Instance, p.PathWithNamespace)
		did = "shown again"
	}
	if !inst.Covers(p.PathWithNamespace) {
		if _, listed := a.projByKey[key]; !listed {
			inst.AddRepository(config.Repository{ID: p.ID, FullPath: p.PathWithNamespace})
			did = "added on its own"
		}
	}
	if did != "" {
		a.saveConfig()
		a.settings.reload()
	}
	if _, listed := a.projByKey[key]; !listed {
		a.adoptProject(p)
		a.readMRsOf(p)
	}
	a.goToProject(key)
	switch {
	case a.cfg.Filters.ClonedOnly && !a.diskOf(p.Instance, p.PathWithNamespace).Cloned:
		a.flash(p.PathWithNamespace + " is " + orListed(did) + ", but not cloned, and only the cloned are shown - C clones it, L shows every repository")
	case did != "":
		a.done(p.PathWithNamespace + " " + did)
	}
}

// orListed is what was done, or that it was listed already.
func orListed(did string) string {
	if did == "" {
		return "listed"
	}
	return did
}

// goToProject shows Repositories with the cursor on a repository, the
// list's filter cleared when it hides it.
func (a *App) goToProject(key projectKey) {
	a.switchTab(pageProjects)
	a.projectsPane.reload()
	if a.selectProject(key) {
		return
	}
	a.projectsPane.clearFilter()
	a.projectsPane.reload()
	a.selectProject(key)
}

// readMRsOf reads the open merge requests of a repository just taken into
// the list, as a refresh would, and puts them into theirs.
func (a *App) readMRsOf(p forge.Project) {
	client := a.client(p.Instance)
	if client == nil {
		return
	}
	job := a.startJob("reading the merge requests of " + p.PathWithNamespace)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		ms, err := projectsMRs(ctx, client, []forge.Project{p})
		which := make([]int, len(ms))
		for i := range ms {
			ms[i].Instance = p.Instance
			which[i] = i
		}
		if err == nil {
			mrExtras(ctx, ms, which, map[string]forge.Provider{p.Instance: client}, nil)
		}
		a.tv.QueueUpdateDraw(func() {
			a.endJob(job)
			if err != nil {
				a.errorf("merge requests of %s: %v", p.PathWithNamespace, err)
				return
			}
			if len(ms) == 0 {
				return
			}
			a.mrs = index.DedupeMergeRequests(append(a.mrs, ms...))
			a.colourAllLabels()
			a.mrsPane.marks = nil
			_ = index.Save(a.cfg.IndexPath("mrs"), index.MergeRequests{
				Version: index.Version, UpdatedAt: a.mrsUpdated, Items: a.mrs, Me: a.me})
			a.mrsPane.reload()
			a.projectsPane.reload()
		})
	}()
}
