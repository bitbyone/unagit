package ui

import (
	"math"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/forge"
)

// TestLabelsAreAColumnAndAreChosenAndSavedOnEsc: a merge request's labels
// are pills in a LABELS column, legible on every row band; t lists the
// labels that can be put on it, space puts one on, and nothing is sent
// until the list closes, the labels it had keeping their order.
func TestLabelsAreAColumnAndAreChosenAndSavedOnEsc(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].Labels = []forge.Label{{Name: "bug", Color: "#d9534f"}, {Name: "ux", Color: "#5cb85c"}}
			}
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "LABELS")
	for _, size := range []struct{ w, h int }{{120, 34}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "Rate limiting")
		assertLegible(t, a, sc, "the labels column")
	}
	resizeApp(a, sc, 120, 34)
	if line := lineAt(a.screenText(sc), "!7"); !strings.Contains(line, "bug") || !strings.Contains(line, "ux") {
		t.Fatalf("the labels are not on the row: %q", line)
	}
	typeRunes(sc, "g ") // marked: the pills on the marked row's band
	waitTrue(t, "the row was not marked", func() bool { return onLoop(a, func() int { return len(a.mrsPane.marks) }) == 1 })
	assertLegible(t, a, sc, "the labels on a marked row")
	typeRunes(sc, "g ") // and the mark taken off again
	waitTrue(t, "the mark stayed", func() bool { return onLoop(a, func() int { return len(a.mrsPane.marks) }) == 0 })

	typeRunes(sc, "gt")
	waitFor(t, a, sc, "Labels · acme/gateway !7")
	waitFor(t, a, sc, "Owned by the backend team")
	assertLegible(t, a, sc, "the labels to choose from")
	typeRunes(sc, "j ") // bug, group::backend, ux
	waitFor(t, a, sc, "3 on")
	if len(srv.written()) != 0 {
		t.Fatalf("sent before the list closed: %q", srv.written())
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Labels of acme/gateway !7: bug, ux, group::backend")
	waitWritten(t, srv, `"labels":"bug,ux,group::backend"`)
	waitRowFetched(t, a, sc)
}

// TestALabelTakesItsHueInTheTheme: a label's pill is worked out of the
// theme from its colour's hue - red stays red, blue blue - and a label
// without a colour still gets one.
func TestALabelTakesItsHueInTheTheme(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	hueOf := func(c string) float64 {
		lab := toOklab(tcell.GetColor(c))
		return mathAtan2Deg(lab.b, lab.a)
	}
	red := onLoop(a, func() tagColour { return labelColour(forge.Label{Name: "bug", Color: "#d9534f"}) })
	blue := onLoop(a, func() tagColour { return labelColour(forge.Label{Name: "docs", Color: "#0075ca"}) })
	none := onLoop(a, func() tagColour { return labelColour(forge.Label{Name: "plain"}) })
	if d := hueDistance(hueOf(red.fill), hueOf("#d9534f")); d > 30 {
		t.Errorf("a red label's fill %s is %.0f° off red", red.fill, d)
	}
	if d := hueDistance(hueOf(blue.ink), hueOf("#0075ca")); d > 30 {
		t.Errorf("a blue label's ink %s is %.0f° off blue", blue.ink, d)
	}
	if none.fill == "" || none.ink == "" {
		t.Errorf("a label without a colour has none: %+v", none)
	}
	if c := contrast(tcell.GetColor(red.ink), tcell.GetColor(red.fill)); c < 3.5 {
		t.Errorf("a red label's ink reads at %.1f:1 on its fill", c)
	}
}

func mathAtan2Deg(y, x float64) float64 { return math.Atan2(y, x) * 180 / math.Pi }

// hueDistance is how far apart two hues are on the wheel, 0 to 180.
func hueDistance(x, y float64) float64 { return math.Abs(math.Mod(x-y+540, 360) - 180) }
