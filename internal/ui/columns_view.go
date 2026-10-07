package ui

import "github.com/tobola/unagit/internal/config"

// columnChoice is a column of a list as View options offers it: the name it
// is kept under in the configuration and its heading, in the order the
// list draws them.
type columnChoice struct {
	id, heading string
	// always marks the column a row is: it cannot be hidden, and is listed
	// only so the order reads as the list does.
	always bool
	// offered says whether the column can be there at all - the server
	// only with more than one, PUB only with Incomm; nil is always.
	offered func(a *App) bool
}

func multiServer(a *App) bool { return a.multiInstance() }
func withIncomm(a *App) bool  { return a.cfg.Integrations.Incomm }

// columnChoices are a list's columns, left to right.
func columnChoices(list string) []columnChoice {
	switch list {
	case config.ListRepositories:
		return []columnChoice{
			{id: "marks", heading: "MARKS"},
			{id: "server", heading: "SERVER", offered: multiServer},
			{id: "repository", heading: "REPOSITORY", always: true},
			{id: "tags", heading: "TAGS"},
			{id: "branch", heading: "BRANCH"},
			{id: "ci", heading: "CI"},
			{id: "remote", heading: "RMT"},
			{id: "edits", heading: "EDITS"},
			{id: "path", heading: "PATH"},
			{id: "mr", heading: "MR"},
			{id: "wt", heading: "WT"},
			{id: "size", heading: "SIZE"},
			{id: "activity", heading: "ACTIVITY"},
		}
	case config.ListMergeRequests:
		return []columnChoice{
			{id: "marks", heading: "MARKS"},
			{id: "server", heading: "SERVER", offered: multiServer},
			{id: "repository", heading: "REPO"},
			{id: "iid", heading: "MR"},
			{id: "title", heading: "TITLE", always: true},
			{id: "author", heading: "AUTHOR"},
			{id: "ci", heading: "CI"},
			{id: "branch", heading: "BRANCH"},
			{id: "new", heading: "NEW"},
			{id: "comments", heading: "COM"},
			{id: "pub", heading: "PUB", offered: withIncomm},
			{id: "approvals", heading: "APPR"},
			{id: "updated", heading: "UPDATED"},
		}
	case config.ListWorktrees:
		return []columnChoice{
			{id: "marks", heading: "MARKS"},
			{id: "server", heading: "SERVER", offered: multiServer},
			{id: "repository", heading: "REPOSITORY", always: true},
			{id: "repos", heading: "REPOS"},
			{id: "branch", heading: "BRANCH"},
			{id: "ci", heading: "CI"},
			{id: "remote", heading: "RMT"},
			{id: "path", heading: "PATH"},
			{id: "edits", heading: "EDITS"},
			{id: "mr", heading: "MR"},
			{id: "comments", heading: "COM", offered: withIncomm},
			{id: "size", heading: "SIZE"},
			{id: "created", heading: "CREATED"},
			{id: "activity", heading: "ACTIVITY"},
		}
	}
	return nil
}

// hidesColumn says whether a list leaves a column out, as chosen in View
// options.
func (a *App) hidesColumn(list, id string) bool { return a.cfg.Filters.HidesColumn(list, id) }

// columnRow is what a row of the Columns section of View options toggles;
// columnsHeading is the section's heading, which toggles nothing.
type (
	columnRow struct {
		list, id string
		always   bool
	}
	columnsHeading struct{}
)

// columnItems are the rows of View options' Columns section: a heading and
// under it each column of the list, an eye when it is shown and the eye
// struck through when it is not.
func (a *App) columnItems(list string) []toggleItem {
	items := []toggleItem{{Label: tag(colMuted) + "[::b]Columns[::-]" + tagEnd, Search: "columns", Data: columnsHeading{}}}
	for _, c := range columnChoices(list) {
		if c.offered != nil && !c.offered(a) {
			continue
		}
		heading := c.heading
		if c.id == "edits" {
			heading = glyphEdits + " " + heading
		}
		eye, colour, note := glyphColumnShown, colText, ""
		switch {
		case c.always:
			colour, note = colDim, "  always shown"
		case a.hidesColumn(list, c.id):
			eye, colour = glyphColumnHidden, colDim
		}
		label := "  " + tag(colour) + eye + "  " + esc(heading) + tagEnd + tag(colDim) + note + tagEnd
		items = append(items, toggleItem{Label: label, Search: c.heading, Data: columnRow{list, c.id, c.always}})
	}
	return items
}

// toggleColumnItem hides or shows the column a row of the Columns section
// stands for, and reports whether the row was one of that section.
func (a *App) toggleColumnItem(it toggleItem) bool {
	switch d := it.Data.(type) {
	case columnRow:
		if d.always {
			a.note("The " + d.id + " column is what a row is, and stays")
			return true
		}
		a.cfg.Filters.ToggleColumn(d.list, d.id)
		a.applyFilters()
		return true
	case columnsHeading:
		return true
	}
	return false
}
