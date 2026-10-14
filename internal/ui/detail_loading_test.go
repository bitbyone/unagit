package ui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/tobola/unagit/internal/forge"
)

type heldDetailProvider struct {
	forge.Provider
	hold <-chan struct{}
}

func (p *heldDetailProvider) ProjectDetail(ctx context.Context, pr forge.Project) (*forge.ProjectDetail, error) {
	select {
	case <-p.hold:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.Provider.ProjectDetail(ctx, pr)
}

func (p *heldDetailProvider) MergeRequestDetail(ctx context.Context, mr forge.MergeRequest) (*forge.MergeRequestDetail, error) {
	select {
	case <-p.hold:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.Provider.MergeRequestDetail(ctx, mr)
}

func (p *heldDetailProvider) MergeRequestNotes(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Note, error) {
	select {
	case <-p.hold:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.Provider.MergeRequestNotes(ctx, mr, limit)
}

func TestRemoteDetailsShowAnAnimatedLoaderAndReleaseIt(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"repository", "merge request", "conversation"} {
		t.Run(kind, func(t *testing.T) {
			hold := make(chan struct{})
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(hold) }) })
			ticks := make(chan time.Time, 8)
			t.Cleanup(func() { close(ticks) })
			a, sc, _ := newTestAppSrv(t, func(a *App) {
				id := a.cfg.Instances[0].ID
				a.clients[id] = &heldDetailProvider{Provider: a.clients[id], hold: hold}
				a.newAnimationTicker = func(time.Duration) (<-chan time.Time, func()) { return ticks, func() {} }
			})
			waitFor(t, a, sc, "acme/gateway")
			if kind != "repository" {
				typeRunes(sc, "2")
				waitFor(t, a, sc, "Rate limiting")
			}
			loading := "Loading " + kind + " details…"
			if kind == "conversation" {
				typeRunes(sc, "c")
				loading = "Loading the conversation…"
			} else {
				sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
			}
			waitFor(t, a, sc, loading)
			before := onLoop(a, func() int { return a.spinFrame })
			ticks <- time.Now()
			waitTrue(t, "the detail spinner never turned", func() bool { return onLoop(a, func() bool { return a.spinFrame > before }) })
			spinner := onLoop(a, func() string { return spinnerGlyph(a.spinFrame) })
			waitFor(t, a, sc, spinner+" "+loading)
			assertLegible(t, a, sc, "the loading "+kind)
			if strings.Contains(a.screenText(sc), "Loading from GitLab") {
				t.Fatal("a shared detail names one forge")
			}
			t.Logf("loading %s:\n%s", kind, a.screenText(sc))
			if kind == "repository" {
				// Closing a panel stops its loader even while the request runs.
				sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
				waitFor(t, a, sc, "NORMAL")
				sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
				waitTrue(t, "the closed detail kept its spinner", func() bool { return onLoop(a, func() bool { return len(a.textWaits) == 0 }) })
			}
			release.Do(func() { close(hold) })
			waitGone(t, a, sc, loading)
			waitTrue(t, "the finished detail kept a wait or job", func() bool {
				return onLoop(a, func() bool { return len(a.textWaits) == 0 && a.waits == 0 && len(a.jobs) == 0 })
			})
		})
	}
}
