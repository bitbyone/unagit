package ui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

func TestMRAgeFilterInputAndLayout(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			a.mrs[i].UpdatedAt = time.Now()
			if a.mrs[i].IID == 7 {
				a.mrs[i].UpdatedAt = time.Now().Add(-31 * 24 * time.Hour)
			}
		}
	})
	typeRunes(sc, "2vjjjjjj ")
	waitFor(t, a, sc, "Hide older merge requests")
	form := currentForm(a)
	if form == nil {
		t.Fatal("the age input did not open")
	}
	for _, size := range []struct{ w, h int }{{120, 34}, {80, 26}, {60, 20}} {
		resizeApp(a, sc, size.w, size.h)
		text := a.screenText(sc)
		for _, label := range []string{"Days", "Since the last update", "Apply", "Cancel", "Esc stop typing"} {
			if !strings.Contains(text, label) {
				t.Errorf("%dx%d: missing %q:\n%s", size.w, size.h, label, text)
			}
		}
		frame := onLoop(a, func() rect {
			x, y, w, h := form.GetRect()
			return rect{x, y, w, h}
		})
		onLoop(a, func() bool {
			x, y, w, h := form.GetInnerRect()
			inner := rect{x, y, w, h}
			for i := 0; i < form.GetFormItemCount(); i++ {
				item := form.GetFormItem(i)
				x, y, w, h := item.GetRect()
				if !((rect{x, y, w, h}).within(inner)) {
					t.Errorf("field %q exceeds its frame at %dx%d", item.GetLabel(), size.w, size.h)
				}
			}
			return true
		})
		for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
			if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
				t.Errorf("%dx%d: border overwritten at row %d:\n%s", size.w, size.h, y, text)
			}
		}
		assertLegible(t, a, sc, "the age input")
		if size.w == 80 {
			t.Logf("age input at 80x26:\n%s", text)
		}
	}
	resizeApp(a, sc, 120, 34)
	typeRunes(sc, "a-+ 3x0.é")
	if got := onLoop(a, func() string { return form.GetFormItem(0).(*tview.InputField).GetText() }); got != "30" {
		t.Fatalf("the numeric input accepted other characters: %q", got)
	}
	pressButton(t, a, sc, form, "Apply")
	waitGone(t, a, sc, "Hide older merge requests")
	waitFor(t, a, sc, "hide older than 30 days")
	loaded, err := config.LoadFrom(a.cfg.Dir())
	if err != nil || loaded.Filters.HideOlderThanDays != 30 {
		t.Fatalf("the age preference did not persist: %v, %v", loaded, err)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "View · Merge requests")
	waitGone(t, a, sc, "Rate limiting")
	waitFor(t, a, sc, "Drop the old client")
	waitFor(t, a, sc, "updated within 30 days")
	typeRunes(sc, "vjjjjjj ")
	waitFor(t, a, sc, "Hide older merge requests")
	form = currentForm(a)
	setField(t, a, form, 0, "7")
	pressButton(t, a, sc, form, "Cancel")
	waitGone(t, a, sc, "Hide older merge requests")
	waitFor(t, a, sc, "hide older than 30 days")
	// Cancel returns to the same row, ready to edit again.
	typeRunes(sc, " ")
	waitFor(t, a, sc, "Hide older merge requests")
	form = currentForm(a)
	setField(t, a, form, 0, "")
	pressButton(t, a, sc, form, "Apply")
	waitGone(t, a, sc, "Hide older merge requests")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "View · Merge requests")
	waitFor(t, a, sc, "Rate limiting")
}

func TestMRAgeFilterKeepsUnknownAndFutureUpdates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		days int
		age  time.Duration
		zero bool
		want bool
	}{
		{"disabled", 0, 1000 * 24 * time.Hour, false, true},
		{"recent", 30, 29 * 24 * time.Hour, false, true},
		{"old", 30, 31 * 24 * time.Hour, false, false},
		{"future", 30, -24 * time.Hour, false, true},
		{"unknown", 30, 0, true, true},
		{"large limit", int(^uint(0) >> 1), 1000 * 24 * time.Hour, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{cfg: &config.Config{Filters: config.Filters{HideOlderThanDays: tc.days}}}
			mr := forge.MergeRequest{UpdatedAt: time.Now().Add(-tc.age)}
			if tc.zero {
				mr.UpdatedAt = time.Time{}
			}
			if got := a.passesMRFilters(mr); got != tc.want {
				t.Errorf("visible = %v, want %v", got, tc.want)
			}
		})
	}
}

type ageFilterProvider struct {
	forge.Provider
	active atomic.Bool
	listed atomic.Int32
	extras atomic.Int32
	names  atomic.Int32
}

func (p *ageFilterProvider) GroupMergeRequests(ctx context.Context, g forge.Group, subgroups bool) ([]forge.MergeRequest, error) {
	mrs, err := p.Provider.GroupMergeRequests(ctx, g, subgroups)
	p.listed.Add(1)
	for i := range mrs {
		mrs[i].UpdatedAt = time.Now()
		if mrs[i].IID == 7 {
			mrs[i].Author = forge.User{Username: "old-only"}
			if !p.active.Load() {
				mrs[i].UpdatedAt = time.Now().Add(-31 * 24 * time.Hour)
			}
		}
	}
	return mrs, err
}

func (p *ageFilterProvider) MergeRequestPipeline(ctx context.Context, mr forge.MergeRequest) (*forge.Pipeline, error) {
	if mr.IID == 7 {
		p.extras.Add(1)
	}
	return p.Provider.MergeRequestPipeline(ctx, mr)
}

func (p *ageFilterProvider) MergeRequestApprovals(ctx context.Context, mr forge.MergeRequest) (*forge.Approvals, error) {
	if mr.IID == 7 {
		p.extras.Add(1)
	}
	return p.Provider.MergeRequestApprovals(ctx, mr)
}

func (p *ageFilterProvider) Threads(ctx context.Context, mr forge.MergeRequest) (int, int, bool, error) {
	if mr.IID == 7 {
		p.extras.Add(1)
	}
	return p.Provider.Threads(ctx, mr)
}

func (p *ageFilterProvider) UserName(ctx context.Context, username string) (string, error) {
	if username == "old-only" {
		p.names.Add(1)
		return "Old Author", nil
	}
	return p.Provider.UserName(ctx, username)
}

func TestMRAgeFilterSkipsExtrasAndFindsNewActivity(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := onLoop(a, func() *ageFilterProvider {
		id := a.cfg.Instances[0].ID
		p := &ageFilterProvider{Provider: a.clients[id]}
		a.clients[id] = p
		a.cfg.Filters.HideOlderThanDays = 30
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].Pipeline = "success"
			}
		}
		return p
	})
	typeRunes(sc, "2")
	refresh := func(count int32) {
		t.Helper()
		typeRunes(sc, "R")
		waitFor(t, a, sc, fmt.Sprintf("%d open merge requests", count))
		waitGone(t, a, sc, "refreshing merge requests")
	}
	refresh(2)
	if p.listed.Load() != 1 || p.extras.Load() != 0 || p.names.Load() != 0 {
		t.Fatalf("hidden MR: listings=%d extras=%d names=%d", p.listed.Load(), p.extras.Load(), p.names.Load())
	}
	got := onLoop(a, func() string {
		for _, mr := range a.mrs {
			if mr.IID == 7 {
				return mr.Pipeline
			}
		}
		return "missing"
	})
	if got != "success" {
		t.Fatalf("the hidden MR lost its cached pipeline: %q", got)
	}
	p.active.Store(true)
	refresh(3)
	waitFor(t, a, sc, "Rate limiting")
	if p.listed.Load() != 2 || p.extras.Load() != 3 || p.names.Load() != 1 {
		t.Errorf("active MR: listings=%d extras=%d names=%d", p.listed.Load(), p.extras.Load(), p.names.Load())
	}
}
