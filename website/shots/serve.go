//go:build shots

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// serve answers as the world's GitLab servers: a merge request's detail,
// its commits, threads and approvals, made up from the cached list; any
// other list is empty. Both servers are this one, by different names.
func serve(dir string) {
	cfg := config.Default()
	cfg.SetDir(filepath.Join(dir, "config"))
	mrs, err := index.Load[index.MergeRequests](cfg.IndexPath("mrs"))
	check(err)
	byKey := map[string]forge.MergeRequest{}
	for _, m := range mrs.Items {
		byKey[fmt.Sprintf("%d/%d", m.ProjectID, m.IID)] = m
	}
	mrPath := regexp.MustCompile(`^/api/v4/projects/(\d+)/merge_requests/(\d+)(/[a-z_]+)?$`)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		m := mrPath.FindStringSubmatch(r.URL.Path)
		if m == nil {
			fmt.Fprint(w, "[]")
			return
		}
		mr, ok := byKey[m[1]+"/"+m[2]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch m[3] {
		case "":
			reply(w, detail(mr))
		case "/commits":
			reply(w, commits(mr))
		case "/discussions":
			reply(w, discussions(mr))
		case "/approvals":
			var by []map[string]any
			for _, u := range mr.ApprovedBy {
				by = append(by, map[string]any{"user": map[string]string{"username": u, "name": u}})
			}
			reply(w, map[string]any{"approved_by": by, "approvals_required": mr.ApprovalsRequired,
				"approvals_left": max(0, mr.ApprovalsRequired-len(mr.ApprovedBy))})
		default:
			fmt.Fprint(w, "[]")
		}
	})
	fmt.Fprintln(os.Stderr, "mock: serving on 127.0.0.1:"+port)
	check(http.ListenAndServe("127.0.0.1:"+port, nil))
}

func reply(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

func detail(mr forge.MergeRequest) map[string]any {
	labels := []map[string]string{}
	for _, l := range mr.Labels {
		labels = append(labels, map[string]string{"name": l.Name, "color": l.Color})
	}
	status := mr.Pipeline
	if status == "" {
		status = "success"
	}
	return map[string]any{
		"id": mr.ID, "iid": mr.IID, "project_id": mr.ProjectID, "title": mr.Title, "state": "opened",
		"draft": mr.Draft, "source_branch": mr.SourceBranch, "target_branch": mr.TargetBranch,
		"web_url": mr.WebURL, "updated_at": mr.UpdatedAt, "created_at": mr.UpdatedAt.Add(-52 * time.Hour).Format(time.RFC3339),
		"author": mr.Author, "reviewers": mr.Reviewers, "assignees": []forge.User{mr.Author}, "labels": labels,
		"description": "Sign-in gets a token bucket per client: a burst of attempts, then one a second.\n\n" +
			"- [x] the limiter and its tests\n- [x] wired into the router\n- [ ] the numbers agreed with the security team",
		"detailed_merge_status": "mergeable", "user_notes_count": mr.Comments, "changes_count": "4",
		"upvotes": 2, "head_pipeline": map[string]any{"id": 9000 + mr.ID, "status": status, "ref": mr.SourceBranch},
		"task_completion_status": map[string]int{"count": 3, "completed_count": 2},
		"diff_refs":              map[string]string{"base_sha": fmt.Sprintf("%040x", mr.ID), "head_sha": mr.SHA, "start_sha": fmt.Sprintf("%040x", mr.ID)},
		"sha":                    mr.SHA, "diverged_commits_count": 1,
	}
}

func commits(mr forge.MergeRequest) []map[string]any {
	titles := []string{"Wire the limiter into the router", "Test the bucket's refill", "Add a token bucket per client"}
	var out []map[string]any
	for i, t := range titles {
		at := mr.UpdatedAt.Add(-time.Duration(i*3) * time.Hour)
		out = append(out, map[string]any{"id": fmt.Sprintf("%040x", mr.ID*100+i), "short_id": fmt.Sprintf("%08x", mr.ID*100+i),
			"title": t, "message": t, "author_name": mr.Author.Name, "authored_date": at, "committed_date": at, "created_at": at})
	}
	return out
}

func discussions(mr forge.MergeRequest) []map[string]any {
	note := func(id int, who forge.User, body string, at time.Duration, resolvable, resolved bool) map[string]any {
		return map[string]any{"id": id, "body": body, "created_at": mr.UpdatedAt.Add(-at), "author": who,
			"resolvable": resolvable, "resolved": resolved}
	}
	reviewer := forge.User{Username: "tomas", Name: "Tomáš Král"}
	return []map[string]any{
		{"id": "d1", "notes": []any{
			note(1, reviewer, "Should the burst be per client or per account? A shared NAT puts a whole office behind one client.", 5*time.Hour, true, false),
			note(2, mr.Author, "Per client for now - per account needs the session first. I'll note it in the ADR.", 4*time.Hour, true, false)}},
		{"id": "d2", "notes": []any{
			note(3, forge.User{Username: "demo", Name: "Demo User"}, "The refill rounds down, so 999ms after a sign-in still counts as none. Intended?", 2*time.Hour, true, true)}},
	}
}
