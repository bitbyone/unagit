package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/watch"
)

// The Activity screen is what runs in the background and what waits for
// the user, in one place (docs/activity.md): the editors open, as cards; a
// count of what is watched; the watches and the agents in one list, by how
// much each wants the user; the story of the one chosen; and a log of
// everything, what came since the last visit set apart.

// activityKind is what a row of the list is.
type activityKind int

const (
	activityWatch activityKind = iota
	activityAgent
)

// The sections of the list, by how much they want the user.
const (
	sectionNeedsYou = iota
	sectionUnderWay
	sectionQuiet
)

// activityItem is one row of the list: a watch or an agent.
type activityItem struct {
	kind    activityKind
	watch   watch.Watch
	agent   agentRow
	section int
	// at is when it last changed, which orders a section.
	at time.Time
}

// key is the item's name in the histories.
func (it activityItem) key() string {
	if it.kind == activityAgent {
		return agentKey(it.agent)
	}
	return it.watch.Key()
}

// activitySections names the sections, with the role of their colour.
var activitySections = []struct {
	title, role string
}{
	{"NEEDS YOU", "activity.needs"},
	{"UNDER WAY", "activity.under_way"},
	{"QUIET", "activity.quiet"},
}

// activityItems is the list: the watches and the agents, each in its
// section, the newest change first.
func (a *App) activityItems() []activityItem {
	latest := map[string]watch.Event{}
	for _, e := range a.activityLog {
		if _, ok := latest[e.Key]; !ok {
			latest[e.Key] = e
		}
	}
	var items []activityItem
	for _, w := range a.watches {
		st := a.watchSnap.States[w.Key()]
		it := activityItem{kind: activityWatch, watch: w, section: sectionQuiet, at: w.Since}
		if !st.Changed.IsZero() {
			it.at = st.Changed
		}
		last, told := latest[w.Key()]
		switch {
		case st.Status == "manual" || ciStateOf(st.Status) == ciFailed:
			it.section = sectionNeedsYou
		case told && (last.Level == watch.Danger || last.Level == watch.Warning) && a.activityUnseen(last):
			// An approval withdrawn, a force push: news that wants a look,
			// for as long as it has not had one.
			it.section = sectionNeedsYou
		case ciStateOf(st.Status) == ciRunning:
			it.section = sectionUnderWay
		}
		if told && last.At.After(it.at) {
			it.at = last.At
		}
		items = append(items, it)
	}
	for _, r := range a.agentRows {
		it := activityItem{kind: activityAgent, agent: r, section: sectionQuiet}
		if r.Record != nil {
			it.at = r.Record.Since
		}
		switch r.Status {
		case "blocked":
			it.section = sectionNeedsYou
		case "working":
			it.section = sectionUnderWay
		}
		if last, ok := latest[agentKey(r)]; ok && last.At.After(it.at) {
			it.at = last.At
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].section != items[j].section {
			return items[i].section < items[j].section
		}
		return items[i].at.After(items[j].at)
	})
	return items
}

// activityUnseen reports whether an event came after the last visit.
func (a *App) activityUnseen(e watch.Event) bool {
	return !e.At.IsZero() && e.At.After(a.activitySince)
}

// itemUnseen reports whether a row changed since the screen was last
// looked at: a watch by the poller's count, as the tab counts it; an agent
// by its history.
func (a *App) itemUnseen(it activityItem) bool {
	if it.kind == activityWatch {
		return a.watchUnseenRow(it.watch)
	}
	for _, e := range a.activityLog {
		if e.Key == it.key() {
			return a.activityUnseen(e) && e.Level == watch.Warning
		}
	}
	return false
}

// setActivityLog puts the histories in place, as the follower read them.
// It runs on the loop.
func (a *App) setActivityLog(events []watch.Event) {
	a.activityLog = events
	a.redrawActivity()
}

// redrawActivity draws the screen again where it is built.
func (a *App) redrawActivity() {
	if a.activityPane != nil {
		a.activityPane.reload()
	}
}

// itemWhat is how a row names its thing.
func (a *App) itemWhat(it activityItem) (icon, text string) {
	if it.kind == activityAgent {
		return agentIcons[it.agent.Kind], agentName(it.agent.Kind)
	}
	return a.forgeIcon(it.watch.Instance), it.watch.Label()
}

// itemAbout is what a row says the thing is about: a merge request's
// title, where an agent works and on what.
func (a *App) itemAbout(it activityItem) string {
	if it.kind == activityAgent {
		what := a.agentWhat(it.agent)
		if what == "" {
			what = tildePath(it.agent.Dir)
		}
		if it.agent.Title != "" {
			return what + " · " + it.agent.Title
		}
		return what
	}
	return a.watchTitle(it.watch)
}

// itemState is a row's state: a glyph and words, in the state's colour.
func (a *App) itemState(it activityItem) (glyph, words string, colour string) {
	if it.kind == activityAgent {
		glyph, word, role := agentState(it.agent.Status, a.spinFrame)
		if word == "waiting" {
			word = "waits for an answer"
		}
		return glyph, word, role
	}
	st := a.watchSnap.States[it.watch.Key()]
	glyph, _ = ciMark(st.Status)
	words = watchPipelineWords(st)
	if st.Error != "" {
		return glyph, words, "watched.error"
	}
	return glyph, words, ""
}

func filterActivity(a *App, items []activityItem, query string) []int {
	var hits []scored
	for i, it := range items {
		_, what := a.itemWhat(it)
		_, words, _ := a.itemState(it)
		hay := strings.Join([]string{what, a.itemAbout(it), words}, " ")
		score, ok := fuzzy.Match(query, hay)
		if !ok {
			continue
		}
		hits = append(hits, scored{idx: i, score: score})
	}
	if strings.TrimSpace(query) != "" {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

// sectionHeading is a section's row: its name as a pill in its colour, how
// many it holds, and a rule to the edge.
func sectionHeading(section, count, width int) string {
	s := activitySections[section]
	ink, fill := role(s.role), role(s.role+"_fill")
	label := fmt.Sprintf(" %s · %d ", s.title, count)
	rule := max(0, width-cells(label)-2)
	return " " + "[" + ink.String() + ":" + fill.String() + ":b]" + esc(label) + "[-:-:-]" +
		" " + tag(role("activity.rule")) + strings.Repeat(glyphBar, max(0, rule-1)) + tagEnd
}

// drawActivity lays the list out: a heading for each section that has
// rows, the rows under it.
func (a *App) drawActivity(p *pane, items []activityItem, filtered []int) {
	previous := p.selectedIndex()
	p.table.Clear()
	const markW, glyphW = 1, 1
	var whats, abouts, states []int
	ageW := headingWidth("CHANGED")
	for _, idx := range filtered {
		it := items[idx]
		icon, what := a.itemWhat(it)
		_, words, _ := a.itemState(it)
		whats = append(whats, iconWidth(icon)+cells(what))
		abouts = append(abouts, cells(a.itemAbout(it)))
		states = append(states, cells(words))
		ageW = max(ageW, cells(humanAge(it.at)))
	}
	whatCol := flexColumn("WHAT", whats, 18, 1.6)
	stateCol := flexColumn("STATE", states, 14, 1.3)
	aboutCol := flexColumn("ABOUT", abouts, 10, 1)
	ageCol := fixedColumn(ageW)
	aboutCol.drop, ageCol.drop = 2, 1
	spare := layoutColumns(p.contentWidth()-1, fixedColumn(markW), fixedColumn(glyphW), whatCol, stateCol, aboutCol, ageCol)
	if aboutCol.shown() {
		aboutCol.width += spare
	} else {
		whatCol.width += spare
	}
	head := role("activity.label")
	header := []field{{width: markW}, {width: glyphW},
		{text: "WHAT", width: whatCol.width, colour: head},
		{text: "STATE", width: stateCol.width, colour: head}}
	if aboutCol.shown() {
		header = append(header, field{text: "ABOUT", width: aboutCol.width, colour: head})
	}
	if ageCol.shown() {
		header = append(header, field{text: "CHANGED", width: ageCol.width, colour: head, right: true})
	}
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(withHeadingIcons(header))).SetSelectable(false).SetExpansion(1))

	counts := map[int]int{}
	for _, idx := range filtered {
		counts[items[idx].section]++
	}
	row := 1
	section := -1
	for _, idx := range filtered {
		it := items[idx]
		if it.section != section {
			section = it.section
			if row > 1 {
				p.table.SetCell(row, 0, tview.NewTableCell("").SetSelectable(false))
				row++
			}
			p.table.SetCell(row, 0, tview.NewTableCell(sectionHeading(section, counts[section], p.contentWidth())).SetSelectable(false).SetExpansion(1))
			row++
		}
		mark := field{width: markW}
		if a.itemUnseen(it) {
			mark = field{text: glyphNew, width: markW, colour: role("activity.new")}
		}
		glyph, words, stateRole := a.itemState(it)
		glyphColour := role("activity.quiet")
		if it.kind == activityWatch {
			_, glyphColour = ciMark(a.watchSnap.States[it.watch.Key()].Status)
		} else {
			glyphColour = role(stateRole)
		}
		stateColour := role("activity.state")
		if stateRole != "" {
			stateColour = role(stateRole)
		} else if it.kind == activityWatch {
			stateColour = stateColour2(a.watchSnap.States[it.watch.Key()].Status)
		}
		icon, what := a.itemWhat(it)
		whatColour := role("activity.what")
		if it.section == sectionQuiet {
			whatColour = role("activity.what_quiet")
		}
		cells := []field{mark, {text: glyph, width: glyphW, colour: glyphColour},
			{icon: icon, text: what, width: whatCol.width, colour: whatColour, shorten: shortenRepo},
			{text: words, width: stateCol.width, colour: stateColour}}
		if aboutCol.shown() {
			cells = append(cells, field{text: a.itemAbout(it), width: aboutCol.width, colour: role("activity.about")})
		}
		if ageCol.shown() {
			cells = append(cells, field{text: humanAge(it.at), width: ageCol.width, colour: role("activity.age"), right: true})
		}
		cell := tview.NewTableCell(rowText(cells)).SetReference(idx).SetExpansion(1)
		if p.marks[idx] {
			bandMarked.paint(cell)
		}
		p.table.SetCell(row, 0, cell)
		row++
	}
	if len(filtered) == 0 {
		hint := "Nothing under way. Watch Merge Request or Watch Branch - in the actions of a row (Alt-Enter) - or an agent opened from unagit shows here."
		if len(items) > 0 {
			hint = "Nothing matches the filter."
		}
		p.table.SetCell(1, 0, tview.NewTableCell(" "+tag(colMuted)+esc(hint)+tagEnd).SetSelectable(false).SetExpansion(1))
	}
	first := 0
	for r := 1; r < p.table.GetRowCount(); r++ {
		if _, ok := p.table.GetCell(r, 0).GetReference().(int); ok {
			first = r
			break
		}
	}
	p.selectRow(previous, first)
}

// stateColour2 is a pipeline's words in the colour of how it stands, the
// quiet colour when it stands nowhere.
func stateColour2(status string) tcell.Color {
	if status == "" {
		return role("activity.state")
	}
	return stateColour(status)
}

// eventTime is when an event came, as short as tells it: the time today,
// the day and time this week, the date before.
func eventTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	switch {
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return t.Format("15:04")
	case now.Sub(t) < 6*24*time.Hour:
		return t.Format("Mon 15:04")
	}
	return t.Format("Jan 2")
}

// levelGlyph is an event's mark, in its level's colour.
func levelGlyph(e watch.Event) (string, string) {
	sev := levelSeverity(e.Level)
	return toastIcons[sev], "toast." + toastLevel(sev) + ".border"
}

// eventHeading is an event's few words; an older one has only its line.
func eventHeading(e watch.Event) string {
	if e.Heading != "" {
		return e.Heading
	}
	return e.Line
}

// visitLine is the line that sets apart what came since the last visit.
func (a *App) visitLine(width int) string {
	label := " since your last visit "
	if !a.activitySince.IsZero() {
		label = " since your last visit · " + eventTime(a.activitySince) + " "
	}
	left := 2
	right := max(0, width-left-cells(label))
	return tag(role("activity.visit")) + strings.Repeat(glyphBar, left) + "[::b]" + esc(label) + "[::-]" + strings.Repeat(glyphBar, right) + tagEnd
}

// activityDetail is the detail of a row: how it stands now, then its
// story, the newest first, what came since the last visit set apart.
func (a *App) activityDetail(it activityItem, width int) string {
	var b strings.Builder
	label := func(name string) string { return tag(role("activity.label")) + fmt.Sprintf("%-6s", name) + tagEnd }
	line := func(s string) { b.WriteString(s + "\n") }
	if it.kind == activityWatch {
		w := it.watch
		st := a.watchSnap.States[w.Key()]
		line(tag(colMuted) + esc(w.Project) + " · watched " + humanAge(w.Since) + tagEnd)
		line("")
		glyph, colour := ciMark(st.Status)
		ci := tag(colour) + glyph + tagEnd + " " + tag(stateColour2(st.Status)) + esc(watchPipelineWords(st)) + tagEnd
		if st.Pipeline > 0 {
			ci = tag(colour) + glyph + tagEnd + " " + tag(colMuted) + fmt.Sprintf("#%d ", st.Pipeline) + tagEnd + tag(stateColour2(st.Status)) + esc(watchPipelineWords(st)) + tagEnd
		}
		if st.User != "" {
			ci += tag(colMuted) + " · " + esc(st.User) + tagEnd
		}
		line(label("CI") + ci)
		if w.IID > 0 && st.Known {
			var parts []string
			approved := tag(role("approvals.missing")) + "no approvals" + tagEnd
			if len(st.Approvers) > 0 {
				approved = tag(role("approvals.done")) + glyphCheck + " " + esc(strings.Join(st.Approvers, ", ")) + tagEnd
			}
			parts = append(parts, approved, tag(role("comments.all"))+fmt.Sprintf("%d %s", st.Comments, plural(st.Comments, "comment", "comments"))+tagEnd)
			line(label("MR") + strings.Join(parts, tag(colMuted)+" · "+tagEnd))
			if st.Head != "" {
				head := tag(role("column.mr")) + shortSHA(st.Head) + tagEnd
				if st.HeadBy != "" {
					head += " " + tag(colMuted) + esc(st.HeadBy) + tagEnd
				}
				if st.HeadTitle != "" {
					head += " " + esc(trunc(st.HeadTitle, max(10, width-30)))
				}
				line(label("HEAD") + head)
			}
		}
		if st.Base != "" {
			behind := tag(role("state.good")) + "up to date with " + esc(st.Base) + tagEnd
			if st.Behind > 0 {
				behind = tag(colWarn) + fmt.Sprintf("%d behind %s", st.Behind, esc(st.Base)) + tagEnd
			}
			line(label("BASE") + behind)
		}
		if st.Error != "" {
			line(label("READ") + tag(role("watched.error")) + esc(st.Error) + tagEnd)
		}
	} else {
		r := it.agent
		glyph, word, colour := agentState(r.Status, a.spinFrame)
		state := strings.TrimSpace(glyph + " " + word)
		if state == glyphRing {
			state += " running"
		}
		line(tag(colMuted) + esc(tildePath(r.Dir)) + tagEnd)
		line("")
		line(label("STATE") + tag(role(colour)) + esc(state) + tagEnd)
		if what := a.agentWhat(r); what != "" {
			line(label("ON") + esc(what) + tag(colMuted) + " · " + esc(r.Branch) + tagEnd)
		}
		line(label("WHERE") + esc(r.Where))
		if r.Title != "" {
			line(label("TITLE") + esc(r.Title))
		}
		if r.Record != nil {
			line(label("SINCE") + tag(colMuted) + humanAge(r.Record.Since) + tagEnd)
		}
	}
	line("")
	var story []watch.Event
	for _, e := range a.activityLog {
		if e.Key == it.key() {
			story = append(story, e)
		}
	}
	b.WriteString(a.storyText(story, width, false))
	return b.String()
}

// storyIndent is where an event's sentence starts under its heading: past
// the time and the mark.
const storyIndent = 12

// glyphNew marks a row or an event that came since the last visit: a bar
// at the edge, not a dot, which a pipeline's state already is.
const glyphNew = "▍"

// storyText is events as the detail tells them, newest first: the time,
// the mark of the level, the heading, and the sentence under it; the line
// of the last visit between the new and the rest. withWhat names each
// event's thing, for events of several.
func (a *App) storyText(events []watch.Event, width int, withWhat bool) string {
	if len(events) == 0 {
		return tag(colMuted) + "Nothing has happened yet." + tagEnd
	}
	var b strings.Builder
	newOnes := 0
	for _, e := range events {
		if a.activityUnseen(e) {
			newOnes++
		}
	}
	if newOnes > 0 {
		b.WriteString(a.visitLine(width) + "\n")
	} else {
		b.WriteString(tag(role("activity.label")) + "HISTORY" + tagEnd + "\n")
	}
	for i, e := range events {
		if newOnes > 0 && i == newOnes {
			b.WriteString(tag(role("activity.rule")) + strings.Repeat(glyphBar, max(0, width)) + tagEnd + "\n")
		}
		glyph, colour := levelGlyph(e)
		when := fmt.Sprintf("%-10s", eventTime(e.At))
		head := tag(role("activity.when")) + esc(when) + tagEnd + tag(role(colour)) + esc(glyph) + tagEnd + " "
		heading := "[::b]" + esc(eventHeading(e)) + "[::-]"
		if withWhat {
			heading = tag(role("activity.what")) + esc(e.What) + tagEnd + " " + heading
		}
		b.WriteString(head + heading + "\n")
		if e.Heading != "" {
			for _, l := range tview.WordWrap(e.Line, max(10, width-storyIndent)) {
				b.WriteString(strings.Repeat(" ", storyIndent) + tag(role("activity.about")) + esc(l) + tagEnd + "\n")
			}
		}
	}
	return b.String()
}

// activityItemAt is the row of a data index.
func (a *App) activityItemAt(idx int) (activityItem, bool) {
	if idx < 0 || idx >= len(a.activityRows) {
		return activityItem{}, false
	}
	return a.activityRows[idx], true
}

// selectActivityKey puts the list's cursor on the row of a key.
func (a *App) selectActivityKey(key string) {
	p := a.activityPane
	p.selectWhere(func(i int) bool {
		it, ok := a.activityItemAt(i)
		return ok && it.key() == key
	})
}

// activityWatchesOf are the watches among items.
func activityWatchesOf(items []activityItem) []watch.Watch {
	var out []watch.Watch
	for _, it := range items {
		if it.kind == activityWatch {
			out = append(out, it.watch)
		}
	}
	return out
}
