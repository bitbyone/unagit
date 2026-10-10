package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// Find Repository… (+, from anywhere) looks for a repository among those
// the accounts are members of - in the groups not chosen too, the hidden
// ones too - and brings the one chosen into Repositories. A hidden one is
// shown again; one no chosen group has is listed on its own
// (Instance.Repositories, which Settings › Groups lists under its server),
// never by choosing its whole group and hiding the rest, which would let
// every repository made there later in; one listed already is gone to.
//
// It looks in two places. What unagit knows - every repository of the
// lists, the hidden ones with them - is matched at once, as any picker
// matches. What it does not is asked of each forge once typing pauses
// (SearchProjects): nothing is listed ahead. Whatever the forges answer
// joins the items and every item is matched again, so a forge's good match
// goes above a poorer one known before; a spinner in the dialog's edge says
// the rows may still move.

// searchPause is how long typing stops before the forges are asked.
const searchPause = 300 * time.Millisecond

// searchLeast is the shortest text the forges are asked about: shorter,
// every repository matches.
const searchLeast = 3

// finder is one Find Repository… while it is open: the repositories known
// when it opened, what the forges found since, and the search under way.
type finder struct {
	a         *App
	instances []config.Instance
	known     []forge.Project
	found     map[projectKey]forge.Project
	live      *livePicker
	front     tview.Primitive
	width     int

	// gen counts the searches; an answer to one that is not the last is
	// dropped. pending is how many forges the last has still to answer.
	gen     int
	pending int
	timer   *time.Timer
	cancel  context.CancelFunc
	waiting bool
	failed  []string
}

// showFindRepository opens the picker typing: a repository is looked for
// by name.
func (a *App) showFindRepository() {
	f := &finder{a: a, found: map[projectKey]forge.Project{}, width: 100}
	for _, inst := range a.cfg.Instances {
		if a.client(inst.ID) != nil {
			f.instances = append(f.instances, inst)
		}
	}
	if len(f.instances) == 0 {
		a.note("no server with a token - add one in " + settingsTab)
		return
	}
	f.known = append([]forge.Project(nil), a.projects...)
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
		},
		relabel: func(items []pickItem, w int) {
			f.width = w
			header := a.labelFind(items, w)
			if f.live != nil {
				f.live.setHeader(header)
			}
		},
		searched: f.typed,
		cancel:   f.stop,
	}
	title, items, header := f.items()
	opts.header = header
	f.live = a.showPickerWith(title, items, opts, func(it pickItem) {
		f.stop()
		a.bringIn(at(it))
	})
	_, f.front = a.pages.GetFrontPage()
}

// items are what is known and what was found, the most recently active
// first, as the picker lists them before anything is typed.
func (f *finder) items() (string, []pickItem, string) {
	all := append([]forge.Project(nil), f.known...)
	have := map[projectKey]bool{}
	for _, p := range all {
		have[projectKey{p.Instance, p.PathWithNamespace}] = true
	}
	for k, p := range f.found {
		if !have[k] {
			all = append(all, p)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].LastActivityAt.Equal(all[j].LastActivityAt) {
			return all[i].LastActivityAt.After(all[j].LastActivityAt)
		}
		return all[i].PathWithNamespace < all[j].PathWithNamespace
	})
	items := make([]pickItem, len(all))
	for i, p := range all {
		items[i] = pickItem{About: p.Description, Data: p}
	}
	return fmt.Sprintf("Find Repository · %d", len(all)), items, f.a.labelFind(items, f.width)
}

// typed hears the filter change: the search under way is given up, and
// the forges are asked about the new text once typing pauses.
func (f *finder) typed(query string) {
	f.halt()
	query = strings.TrimSpace(query)
	if len([]rune(query)) < searchLeast {
		return
	}
	gen := f.gen
	f.timer = time.AfterFunc(searchPause, func() {
		f.a.tv.QueueUpdateDraw(func() {
			if gen == f.gen && f.live.open() {
				f.search(query)
			}
		})
	})
}

// halt gives up the search under way, or the one waiting for typing to
// pause. It runs on the event loop.
func (f *finder) halt() {
	f.gen++
	if f.timer != nil {
		f.timer.Stop()
	}
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.pending, f.failed = 0, nil
	f.quiet()
}

// stop ends the searches for good: the picker is closing.
func (f *finder) stop() { f.halt() }

// quiet takes the spinner out of the dialog's edge.
func (f *finder) quiet() {
	a := f.a
	if !f.waiting {
		return
	}
	f.waiting = false
	a.waits--
	if a.dialogSaid.waiting && a.dialogSaid.on == f.front {
		a.dialogSaid = dialogWord{}
	}
}

// search asks every forge about the text at once, and puts each answer
// into the list as it comes.
func (f *finder) search(query string) {
	a := f.a
	gen := f.gen
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	f.cancel, f.pending = cancel, len(f.instances)
	var names []string
	for _, inst := range f.instances {
		names = append(names, inst.Label())
	}
	a.dialogSaid = dialogWord{on: f.front, text: tag(colMuted) + esc("searching "+strings.Join(names, ", ")) + tagEnd, waiting: true}
	if !f.waiting {
		f.waiting = true
		a.waits++
		a.keepSpinning()
	}
	for _, inst := range f.instances {
		client := a.client(inst.ID)
		go func() {
			got, err := client.SearchProjects(ctx, query)
			a.tv.QueueUpdateDraw(func() {
				if gen != f.gen {
					return
				}
				f.pending--
				switch {
				case err != nil && ctx.Err() == nil:
					f.failed = append(f.failed, fmt.Sprintf("%s: %v", inst.Label(), err))
				case err == nil:
					for _, p := range got {
						p.Instance = inst.ID
						f.found[projectKey{p.Instance, p.PathWithNamespace}] = p
					}
					if f.live.open() {
						title, items, header := f.items()
						f.live.setHeader(header)
						f.live.set(title, items)
					}
				}
				if f.pending > 0 {
					return
				}
				cancel()
				f.cancel = nil
				f.quiet()
				if len(f.failed) > 0 && f.live.open() {
					a.flash("cannot search " + strings.Join(f.failed, "; "))
				}
			})
		}()
	}
}

// labelFind writes the rows for a row width cells wide - whether it is on
// disk, the server when there is more than one, the forge's icon and the
// path, where it stands, when it was last active - and gives their header.
// A path too long is shortened by what tells it apart.
func (a *App) labelFind(items []pickItem, width int) string {
	var servers, paths []int
	ageWidth := len("ACTIVE")
	for _, it := range items {
		p := it.Data.(forge.Project)
		if a.multiInstance() {
			servers = append(servers, cells(a.instanceLabel(p.Instance)))
		}
		paths = append(paths, iconWidth(a.forgeIcon(p.Instance))+cells(p.PathWithNamespace))
		ageWidth = max(ageWidth, cells(humanAge(p.LastActivityAt)))
	}
	mark := fixedColumn(cells(glyphDiskBranch))
	server := flexColumn("SERVER", servers, 6, 0.5)
	if !a.multiInstance() {
		server = fixedColumn(0)
	}
	repo := flexColumn("REPOSITORY", paths, 16, 2)
	state := fixedColumn(max(findStateWidth(glyphCheck, "listed"), findStateWidth(glyphHidden, "hidden"), findStateWidth(glyphUnlisted, "new")))
	age := fixedColumn(ageWidth)
	layoutColumns(width-4, mark, server, repo, state, age)
	rows := make([][]string, len(items))
	for i, it := range items {
		p := it.Data.(forge.Project)
		disk := " "
		if a.diskOf(p.Instance, p.PathWithNamespace).Cloned {
			disk = tag(role("find.cloned")) + esc(glyphDiskBranch) + tagEnd
		}
		name := ""
		if a.multiInstance() {
			name = tag(role("find.server")) + esc(a.instanceLabel(p.Instance)) + tagEnd
		}
		var where string
		switch a.findStateOf(p) {
		case findListed:
			where = tag(role("find.listed")) + esc(glyphCheck) + " listed" + tagEnd
		case findHidden:
			where = tag(role("find.hidden")) + esc(glyphHidden) + " hidden" + tagEnd
		default:
			where = tag(role("find.unlisted")) + esc(glyphUnlisted) + " new" + tagEnd
		}
		path := rowText([]field{{icon: a.forgeIcon(p.Instance), text: p.PathWithNamespace, width: repo.width, colour: role("find.name"), shorten: shortenRepo}})
		rows[i] = []string{disk, name, path, where, tag(role("find.activity")) + humanAge(p.LastActivityAt) + tagEnd}
	}
	header, labels := pickTable([]string{"", "SERVER", "REPOSITORY", "", "ACTIVE"}, rows)
	for i := range items {
		items[i].Label = labels[i]
	}
	return header
}

// findStateWidth is what a state takes: its icon, a space, its word.
func findStateWidth(icon, word string) int { return cells(icon) + 1 + len(word) }

// findState is where a repository stands in unagit.
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
