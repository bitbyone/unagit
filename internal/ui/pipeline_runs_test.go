package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// TestWorkflowRunsListAsPipelines: GitHub's workflow runs are listed as
// GitLab's pipelines are - each by its workflow and its number, with what
// started it, when and who - and the list fits its frame.
func TestWorkflowRunsListAsPipelines(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{120, 34}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resizeApp(a, sc, size.w, size.h)
			now := time.Now()
			pipes := []forge.Pipeline{
				{ID: 41616057419, Name: "test", Number: 213, Status: "success", Ref: "main", Source: "push",
					StartedAt: now.Add(-time.Hour), User: &forge.User{Username: "jane"}},
				{ID: 41616057400, Name: "website", Number: 40, Status: "running", Ref: "main", Source: "workflow_dispatch",
					StartedAt: now.Add(-2 * time.Hour)},
				{ID: 41500000000, Name: "test", Number: 212, Status: "failed", Ref: "main", Source: "pull_request",
					StartedAt: now.Add(-50 * time.Hour), User: &forge.User{Username: "john"}},
			}
			changeOnLoop(a, func() {
				target, err := a.branchCI(a.projects[0].Instance, "acme/gateway", "main")
				if err != nil {
					t.Error(err)
					return
				}
				a.listPipelines(target, pipes, 0, nil)
			})
			for _, want := range []string{"test #213", "website #40", "test #212", "workflow_dispatch", "pull_request"} {
				waitFor(t, a, sc, want)
			}
			assertLegible(t, a, sc, "the workflow runs")
		})
	}
}
