package ui

import (
	"strings"
	"testing"
)

// TestIntegrationsAreTilesUnderTheirKind: the cards stand under headings,
// three across on a large screen, two on an ordinary one, one on a narrow
// one, and h, j, k and l move through them as they are drawn.
func TestIntegrationsAreTilesUnderTheirKind(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	for _, size := range []struct{ w, h, columns int }{{220, 50, 3}, {140, 40, 2}, {90, 30, 1}} {
		resizeApp(a, sc, size.w, size.h)
		openSection(t, a, sc, sectionIntegrations)
		waitFor(t, a, sc, "╭ Neovim")
		if got := onLoop(a, func() int { return a.settings.integrations.columns }); got != size.columns {
			t.Fatalf("%d columns at %d wide, want %d", got, size.w, size.columns)
		}
		text := a.screenText(sc)
		t.Logf("Integrations at %dx%d:\n%s", size.w, size.h, text)
		// Side by side: the first row holds that many card tops.
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "╭ Neovim") {
				if got := strings.Count(line, "╭"); got != size.columns {
					t.Fatalf("%d cards in the first row at %d wide:\n%s", got, size.w, line)
				}
			}
		}
		assertLegible(t, a, sc, "integration tiles")
	}

	// Two across: l goes right, j down a row - into the next category when
	// the row is the last of its own - k back up, h left and then out to
	// the list of sections.
	resizeApp(a, sc, 140, 40)
	openSection(t, a, sc, sectionIntegrations)
	current := func() string {
		return onLoop(a, func() string { v := a.settings.integrations; return v.cards[v.current].name })
	}
	for _, step := range []struct{ key, want string }{
		{"l", "IntelliJ IDEA"}, {"l", "IntelliJ IDEA"}, {"j", "Zed"}, {"j", "Custom"}, {"j", "Incomm"}, {"l", "Hunk"},
		{"j", "Herdr"}, {"h", "Zellij"}, {"j", "Ghostty"}, {"j", "Claude Code"}, {"k", "Ghostty"}, {"k", "Zellij"}, {"h", "Zellij"},
	} {
		typeRunes(sc, step.key)
		// The condition runs on the event loop already.
		waitEditorState(t, a, func() bool {
			v := a.settings.integrations
			return v.cards[v.current].name == step.want || step.key == "h"
		})
		if step.key != "h" {
			if got := current(); got != step.want {
				t.Fatalf("after %s: on %s, want %s", step.key, got, step.want)
			}
		}
	}
	waitEditorState(t, a, func() bool { return !a.settings.contentFocused })
	// Scrolled down, the card with the cursor is whole on screen.
	focusCard(t, a, sc, "Chezmoi")
	waitFor(t, a, sc, "Files & Navigation")
	waitFor(t, a, sc, "╭ Chezmoi")
}

// TestACardAtTheEdgeSlidesUnderIt: scrolled, a card cut by the panel's
// edge shows what is inside the panel and nothing over its frame, which
// stays whole on every row.
func TestACardAtTheEdgeSlidesUnderIt(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	resizeApp(a, sc, 140, 30)
	openSection(t, a, sc, sectionIntegrations)
	waitFor(t, a, sc, "╭ Neovim")
	focusCard(t, a, sc, "Ghostty")
	waitFor(t, a, sc, "╭ Ghostty")
	r := onLoop(a, func() [4]int { x, y, w, h := a.settings.integrations.GetRect(); return [4]int{x, y, w, h} })
	px, py, pw, ph := r[0], r[1], r[2], r[3]
	lines := strings.Split(a.screenText(sc), "\n")
	cut := false
	for y := py; y < py+ph; y++ {
		row := []rune(lines[y])
		left, right := row[px], row[px+pw-1]
		edge := y == py || y == py+ph-1
		if !edge && (left != '│' || right != '│') {
			t.Fatalf("the panel's frame is broken on row %d:\n%s", y, a.screenText(sc))
		}
		// A card's side with no top above it in the panel: cut by the edge.
		if y == py+2 && strings.HasPrefix(strings.TrimSpace(string(row[px+1:px+pw-1])), "│") {
			cut = true
		}
	}
	if !cut {
		t.Fatalf("no card slid under the top edge:\n%s", a.screenText(sc))
	}
}

// TestACardStandsOutAndSaysWhetherItIsOn: a card's background is a step off
// the page's, and its top edge ends with its state - a cell in the state's
// colour, then the word.
func TestACardStandsOutAndSaysWhetherItIsOn(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionIntegrations)
	focusCard(t, a, sc, "Incomm")
	waitFor(t, a, sc, "╭ Incomm")
	lines := strings.Split(a.screenText(sc), "\n")
	for y, line := range lines {
		if !strings.Contains(line, "╭ Incomm") {
			continue
		}
		state := onLoop(a, func() string { _, r := cardState(a.settings.integrations.card("Incomm")); return r })
		// Incomm's own edge, not the card beside it.
		if next := strings.Index(line[strings.Index(line, "╭ Incomm")+1:], "╭"); next >= 0 {
			line = line[:strings.Index(line, "╭ Incomm")+1+next]
		}
		word := -1
		for _, w := range []string{" not installed ", " disabled ", " enabled "} {
			if i := strings.LastIndex(line, w); i > word {
				word = i + 1
			}
		}
		if word < 0 {
			t.Fatalf("no state on the top edge: %q", line)
		}
		col := len([]rune(line[:word]))
		_, cell := cellAt(a, sc, col-2, y)
		if _, bg, _ := cell.Decompose(); bg != role(state) {
			t.Fatalf("the state's cell is %v, want the state's colour", bg)
		}
		_, inside := cellAt(a, sc, len([]rune(line[:strings.Index(line, "╭ Incomm")]))+3, y+2)
		if _, bg, _ := inside.Decompose(); bg != colCard || bg == colBackground {
			t.Fatalf("the card's background is %v, want %v off the page's %v", bg, colCard, colBackground)
		}
		return
	}
	t.Fatalf("no Incomm card:\n%s", a.screenText(sc))
}
