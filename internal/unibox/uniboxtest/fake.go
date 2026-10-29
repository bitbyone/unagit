// Package uniboxtest supplies a unibox stand-in for tests: a shell script
// that answers the commands it is given from fixtures and writes down every
// call, so a test can say what unagit asked unibox to do without a runtime,
// a VM or a token anywhere near it.
package uniboxtest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/testenv"
)

// Answer is what the stand-in prints for a command, and how it exits.
type Answer struct {
	Out    string
	Status int
}

// Fake is an installed stand-in.
type Fake struct {
	// Path is the stand-in, for an app's findExecutable.
	Path string
	log  string
}

// Install writes a stand-in answering each command line - its arguments
// joined by spaces, --json included, a * matching the rest as a shell case
// does - with its answer. A command it has no answer for succeeds saying
// nothing, as an agent's session that ended would.
func Install(t *testing.T, answers map[string]Answer) *Fake {
	t.Helper()
	// Not t.TempDir: a read the app started in the background may still be
	// writing its call down as the test ends, and a directory that is not
	// empty then fails the test.
	dir, err := os.MkdirTemp("", "unibox-fake-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for range 50 {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	f := &Fake{Path: filepath.Join(dir, "unibox"), log: filepath.Join(dir, "calls")}
	patterns := make([]string, 0, len(answers))
	for p := range answers {
		patterns = append(patterns, p)
	}
	// The more a pattern says, the earlier it is tried: an exact line
	// before one ending in *.
	sort.Slice(patterns, func(i, j int) bool {
		wi, wj := strings.Contains(patterns[i], "*"), strings.Contains(patterns[j], "*")
		if wi != wj {
			return !wi
		}
		if len(patterns[i]) != len(patterns[j]) {
			return len(patterns[i]) > len(patterns[j])
		}
		return patterns[i] < patterns[j]
	})
	var script strings.Builder
	fmt.Fprintf(&script, "#!/bin/sh\nprintf '%%s\\n' \"$*\" >> '%s'\ncase \"$*\" in\n", f.log)
	for i, p := range patterns {
		out := filepath.Join(dir, fmt.Sprintf("answer-%d", i))
		if err := os.WriteFile(out, []byte(answers[p].Out), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&script, "  %s) cat '%s'; exit %d;;\n", casePattern(p), out, answers[p].Status)
	}
	script.WriteString("  *) ;;\nesac\n")
	if err := testenv.WriteExecutable(f.Path, []byte(script.String())); err != nil {
		t.Fatal(err)
	}
	return f
}

// casePattern quotes a command line for a shell case, leaving its * a
// wildcard.
func casePattern(p string) string {
	parts := strings.Split(p, "*")
	for i, part := range parts {
		parts[i] = "'" + strings.ReplaceAll(part, "'", `'\''`) + "'"
	}
	return strings.Join(parts, "*")
}

// Calls are the command lines run so far, in order.
func (f *Fake) Calls() []string {
	data, _ := os.ReadFile(f.log)
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Called reports whether a command line was run.
func (f *Fake) Called(line string) bool {
	for _, c := range f.Calls() {
		if c == line {
			return true
		}
	}
	return false
}

// JSON answers a command with a document and success.
func JSON(doc string) Answer { return Answer{Out: doc} }

// Failure answers a command the way unibox fails under --json.
func Failure(code, message string, status int) Answer {
	return Answer{Out: fmt.Sprintf(`{"api_version":2,"code":%q,"message":%q}`, code, message), Status: status}
}

// OK answers a command that changes something.
func OK(message string) Answer {
	return Answer{Out: fmt.Sprintf(`{"api_version":2,"ok":true,"message":%q}`, message)}
}

// Overview are the answers of two envs - web, with claude and copilot, and
// api, whose sandbox is not made yet and which lacks a value - and the
// sandbox of web, running, with two tasks, one of them with its agent at
// work. The context is web's sandbox; two secrets are kept.
func Overview() map[string]Answer {
	return map[string]Answer{
		"version --json":               JSON(`{"api_version":2,"version":"0.5"}`),
		"env ls --json":                JSON(`{"api_version":2,"default":"web","envs":{"web":{"files":["/team/sandboxes/web.sbxenv.yaml"],"repo":"/team"},"api":{"files":["/team/sandboxes/api.sbxenv.yaml"],"repo":"/team"}}}`),
		"env show web --json":          JSON(`{"api_version":2,"env":"web","sandbox":"web","sbxenv":{"files":["/team/sandboxes/web.sbxenv.yaml"],"agent":"claude","args":{"npm_scope":{"default":"acme"}},"kits":[{"source":"docker/claude"}]},"values":{},"missing":[]}`),
		"env show api --json":          JSON(`{"api_version":2,"env":"api","sandbox":"api-box","sbxenv":{"files":["/team/sandboxes/api.sbxenv.yaml"],"agent":"codex","args":{"jira_user":{"required":true,"description":"your Jira login"}}},"values":{},"missing":["jira_user"]}`),
		"context --json":               JSON(`{"api_version":2,"context":{"sandbox":"web"},"sandbox":"web","env":"web"}`),
		"sandbox ls --json":            JSON(overviewSandboxes),
		"config get --json":            JSON(`{"api_version":2,"value":{"default_env":"web","stop_idle":false,"context":{"sandbox":"web"}}}`),
		"secret ls --all --json":       JSON(`{"api_version":2,"secrets":[{"service":"anthropic","scope":"global","kept":"stored","used_by":["web"],"needed":true},{"service":"github","scope":"global","kept":"stored","used_by":["web","api-box"],"needed":true}],"skipped_envs":[]}`),
		"secret ls -e web --json":      JSON(`{"api_version":2,"env":"web","sandbox":"web","secrets":[{"service":"anthropic","kits":["docker/claude"],"stored":true,"scope":"global"},{"service":"artifactory","description":"the team's packages","stored":false,"page":"https://artifactory.example.com/ui/user_profile"}]}`),
		"secret ls -e api --json":      JSON(`{"api_version":2,"env":"api","sandbox":"api-box","secrets":[{"service":"openai","stored":true}]}`),
		"repo ls -e api --json":        JSON(`{"api_version":2,"env":"api","repos":{}}`),
		"var ls -e web --json":         JSON(`{"api_version":2,"env":"web","file":"/home/.config/unibox/vars.toml","args":[{"name":"npm_scope","value":"acme","from":"default"}]}`),
		"var ls -e api --json":         JSON(`{"api_version":2,"env":"api","file":"/home/.config/unibox/vars.toml","args":[{"name":"jira_user","value":"","from":"missing","description":"your Jira login"}]}`),
		"repo ls -e web --json":        JSON(`{"api_version":2,"env":"web","repos":{"adrs":{"url":"https://gl.test/arch/adrs.git","default":"always","from":"repo","description":"the decisions"}}}`),
		"port ls -s web --json":        JSON(`{"api_version":2,"sandbox":"web","running":true,"ports":[{"name":"fe","port":"3000:3000","team":true,"description":"the dev server","default":true,"named":true,"on":true,"published":[{"host_ip":"127.0.0.1","host_port":3000,"sandbox_port":3000,"protocol":"tcp"}]},{"name":"devtools","port":"9333:9222","named":true,"default":false,"on":false,"published":[]}]}`),
		"network ls -s web --json":     JSON(`{"api_version":2,"sandbox":"web","rules":[{"decision":"allow","host":"jitpack.io","protocol":"tcp","from":"yours","sandbox":"web","applied":true}],"blocked":[{"host":"repo.example.com","reason":"not allowed","count":3,"last_seen":"2026-10-28T10:00:00Z"}]}`),
		"sandbox resources web --json": JSON(`{"api_version":2,"sandbox":"web","env":"web","own":{},"resources":{"cpus":"4","memory":"8g"}}`),
	}
}

const overviewSandboxes = `{"api_version":2,"sandboxes":[
{"name":"web","env":"web","files":["/team/sandboxes/web.sbxenv.yaml"],"agent":"claude","agents":{"claude":["claude"],"copilot":["copilot"]},"state":"running",
 "tasks":[
  {"id":"gateway-1a2b3c4d","alias":"rate","sandbox":"web","directory":"/work/gateway-1a2b3c4d","agent":"claude","state":"ready",
   "repositories":[{"name":"gateway","url":"https://gl.test/acme/gateway.git","branch":"feat/rate","host_path":"/src/gateway","mounted":true,"author":"Dev <dev@example.com>"}]},
  {"id":"billing-5e6f7a8b","sandbox":"web","directory":"/work/billing-5e6f7a8b","agent":"copilot","state":"ready",
   "repositories":[{"name":"billing","url":"https://gl.test/acme/billing.git","branch":"fix/round","author":"Dev <dev@example.com>"},
                   {"name":"gateway","url":"https://gl.test/acme/gateway.git","branch":"fix/round","author":"Dev <dev@example.com>"}]}],
 "runs":[{"id":"r1","sandbox":"web","task":"gateway-1a2b3c4d","agent":"claude","pid":1,"since":"2026-10-19T08:00:00Z"}]}]}`
