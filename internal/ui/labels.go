package ui

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/forge"
)

// A merge request's labels are the forge's - GitLab's, GitHub's - not the
// user's tags, but they are drawn the same way: as pills. A label's colour
// is the server's, made for a white page; a pill takes only its hue and how
// colourful it is, and works the ink and the fill out of the theme the way
// a tag's are (pillMaker), so labels sit in the theme as tags do and a red
// label is still a red pill.

// labelPillMaker is the theme's pillMaker, and labelColours what it made of
// each colour so far; setTheme starts both again.
var (
	labelPillMaker pillMaker
	labelColours   = map[string]tagColour{}
)

// labelColour is the pill colours of a label: from its colour's hue, or by
// its name when the forge gave it none.
func labelColour(l forge.Label) tagColour {
	c := tcell.GetColor(l.Color)
	if l.Color == "" || !c.Valid() || c == tcell.ColorDefault {
		sum := 0
		for _, r := range l.Name {
			sum += int(r)
		}
		return tagPalette[sum%len(tagPalette)]
	}
	if kept, ok := labelColours[l.Color]; ok {
		return kept
	}
	lab := toOklab(c)
	// A grey label stays nearly grey; a colourful one takes the theme's
	// full colourfulness, however loud the forge made it.
	chroma := clamp(math.Hypot(lab.a, lab.b)/0.1, 0.25, 1)
	ink, fill := labelPillMaker.pill(math.Atan2(lab.b, lab.a)*180/math.Pi, chroma)
	out := tagColour{name: l.Color, ink: quieter(ink).String(), fill: quieter(fill).String()}
	labelColours[l.Color] = out
	return out
}

// labelPills draws labels side by side, as many as fit in room; the rest
// are counted, +2, rather than cut in half.
func (a *App) labelPills(labels []forge.Label, room int, behind string) (string, int) {
	var parts []string
	width := 0
	for i, l := range labels {
		markup, w := pillOf(l.Name, labelColour(l), a.cfg.Ends(), behind)
		more := ""
		if left := len(labels) - i - 1; left > 0 {
			more = fmt.Sprintf(" +%d", left)
		}
		need := width + w + len(more)
		if width > 0 {
			need++
		}
		if need > room {
			rest := fmt.Sprintf("+%d", len(labels)-i)
			if width > 0 {
				rest = " " + rest
			}
			if width+len(rest) <= room {
				parts = append(parts, tag(colDim)+rest+tagEnd)
				width += len(rest)
			}
			break
		}
		if width > 0 {
			parts = append(parts, " ")
			width++
		}
		parts = append(parts, markup)
		width += w
	}
	return strings.Join(parts, ""), width
}

// labelsField is a merge request's labels as pills, in exactly width cells,
// and the pills again to be drawn over the row's band (keptMarkup), as the
// tags of a repository are.
func (a *App) labelsField(labels []forge.Label, width int, row rowBand) (string, keptMarkup) {
	behind, band := behindList, behindBand()
	switch row {
	case bandMarked:
		_, bg, _ := styleMarkedSelected.Decompose()
		behind, band = colMarked.String(), bg.String()
	case bandOpen:
		behind = colOpen.String()
	}
	markup, w := a.labelPills(labels, width, behind)
	var kept keptMarkup
	if w > 0 {
		kept.markup, kept.width = a.labelPills(labels, width, band)
		if row != bandNone {
			kept.banded = markup
		}
	}
	return markup + strings.Repeat(" ", max(0, width-w)), kept
}

// editLabels lists the labels that can be put on a merge request, those it
// wears marked; space puts one on or takes it off, and the choice goes to
// the server when the list closes.
func (a *App) editLabels(mr forge.MergeRequest) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	var choices []forge.Label
	a.loadThen(fmt.Sprintf("Labels of %s", path), func(step func(string)) (string, error) {
		step("reading the labels")
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		choices, err = client.LabelChoices(ctx, mr)
		return "", err
	}, func(string) {
		a.showLabelToggles(client, mr, path, choices)
	})
}

func (a *App) showLabelToggles(client forge.Provider, mr forge.MergeRequest, path string, choices []forge.Label) {
	on := map[string]bool{}
	for _, l := range mr.Labels {
		on[l.Name] = true
	}
	// A label the merge request wears stays on the list even when the
	// choices do not name it - one of a group it was moved out of - or it
	// could not be taken off.
	known := map[string]bool{}
	for _, l := range choices {
		known[l.Name] = true
	}
	for _, l := range mr.Labels {
		if !known[l.Name] {
			choices = append(choices, l)
		}
	}
	// What it wears now comes first, to be taken off or replaced at once.
	sort.SliceStable(choices, func(i, j int) bool {
		if on[choices[i].Name] != on[choices[j].Name] {
			return on[choices[i].Name]
		}
		return strings.ToLower(choices[i].Name) < strings.ToLower(choices[j].Name)
	})
	if len(choices) == 0 {
		a.flash(path + " has no labels to put on - make them on the forge first")
		return
	}
	before := forge.LabelNames(mr.Labels)
	a.showToggles(toggles{
		title: fmt.Sprintf("Labels · %s !%d", path, mr.IID),
		verb:  "on/off",
		items: func() []toggleItem {
			items := make([]toggleItem, 0, len(choices))
			for _, l := range choices {
				markup, _ := pillOf(l.Name, labelColour(l), a.cfg.Ends(), behindList)
				label := tagMark(on[l.Name]) + " " + markup
				if l.Description != "" {
					label += "  " + tag(colDim) + esc(firstLine(strings.TrimSpace(l.Description))) + tagEnd
				}
				items = append(items, toggleItem{Label: label, Search: l.Name + " " + l.Description, Data: l.Name})
			}
			return items
		},
		toggle: func(it toggleItem) {
			name := it.Data.(string)
			on[name] = !on[name]
		},
		keys: []toggleKey{{key: 'x', hint: "remove", onItem: func(it toggleItem) { on[it.Data.(string)] = false }}},
		status: func() string {
			n := 0
			for _, v := range on {
				if v {
					n++
				}
			}
			return fmt.Sprintf("%d on", n)
		},
		escSays: "save",
		closed: func() {
			// The order the merge request had is kept; what is new follows.
			var after []string
			for _, name := range before {
				if on[name] {
					after = append(after, name)
				}
			}
			for _, l := range choices {
				if on[l.Name] && !slices.Contains(after, l.Name) {
					after = append(after, l.Name)
				}
			}
			if slices.Equal(before, after) {
				return
			}
			var labels []forge.Label
			for _, name := range after {
				for _, l := range choices {
					if l.Name == name {
						labels = append(labels, l)
						break
					}
				}
			}
			a.saveLabels(client, mr, path, after, labels)
		},
	})
}

// saveLabels sends the labels to the server, puts them on the row at once
// and reads the merge request again after.
func (a *App) saveLabels(client forge.Provider, mr forge.MergeRequest, path string, names []string, labels []forge.Label) {
	a.note(fmt.Sprintf("Setting the labels of %s !%d …", path, mr.IID))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := client.SetLabels(ctx, mr, names)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%v", err)
				return
			}
			fresh := mr
			fresh.Labels = labels
			a.applyMRUpdate(fresh, true, false)
			what := "none"
			if len(names) > 0 {
				what = strings.Join(names, ", ")
			}
			a.done(fmt.Sprintf("Labels of %s !%d: %s", path, mr.IID, what))
			a.refetchMR(client, mr)
		})
	}()
}
