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
	return Answer{Out: fmt.Sprintf(`{"api_version":1,"code":%q,"message":%q}`, code, message), Status: status}
}

// OK answers a command that changes something.
func OK(message string) Answer {
	return Answer{Out: fmt.Sprintf(`{"api_version":1,"ok":true,"message":%q}`, message)}
}

// Overview are the answers of an environments repository with one profile,
// web, two runtimes, and two sandboxes of it: web-1 running with two tasks,
// one of them with its agent at work, and web-2 stopped and empty.
func Overview() map[string]Answer {
	return map[string]Answer{
		"version --json":                  JSON(`{"api_version":1,"version":"0.3","profile_format":6}`),
		"config get environments --json":  JSON(`{"api_version":1,"value":"/envs"}`),
		"context --json":                  JSON(`{"api_version":1,"context":{"sandbox":"web-1"},"sandbox":"web-1","profile":"web"}`),
		"runtime ls --json":               JSON(`{"api_version":1,"runtimes":[{"name":"sbx","available":true,"enabled":true,"default":true},{"name":"msb","available":true,"enabled":true,"experimental":true}]}`),
		"profile ls --json":               JSON(`{"api_version":1,"profiles":[{"name":"web","agents":["claude","copilot"],"images":{"sbx":"built","msb":"outdated"},"last_commit":"2026-10-12"},{"name":"broken","agents":[],"images":{},"error":"profile format 4 - run unibox init broken"}]}`),
		"sandbox ls --json":               JSON(overviewSandboxes),
		"agent ls -p web --json":          JSON(`{"api_version":1,"profile":"web","agents":[{"agent":"claude","auth":"token secret:claude","co_author":"Claude <noreply@anthropic.com>","args":["--dangerously-skip-permissions"]},{"agent":"copilot","auth":"token secret:copilot","co_author":"Copilot <Copilot@users.noreply.github.com>","args":["--allow-all"]}]}`),
		"service ls -p web --json":        JSON(`{"api_version":1,"profile":"web","services":[{"name":"github","git":true,"hosts":["github.com"],"token":"stored"},{"name":"artifactory","git":false,"hosts":["artifactory.example.com"],"token":"missing"}]}`),
		"network ls -p web --json":        JSON(`{"api_version":1,"profile":"web","allow":["jitpack.io"],"deny":["gist.github.com"],"services":{"github.com":"github"}}`),
		"network port ls -s web-1 --json": JSON(`{"api_version":1,"sandbox":"web-1","running":true,"ports":[{"name":"fe","port":"3000:3000","default":true,"named":true,"on":true,"published":[{"host_ip":"127.0.0.1","host_port":3000,"sandbox_port":3000,"protocol":"tcp"}]},{"name":"devtools","port":"9333:9222","named":true,"on":false,"published":[]}]}`),
		"network port ls -s web-2 --json": JSON(`{"api_version":1,"sandbox":"web-2","running":false,"ports":[]}`),
		"script ls -s web-1 --json":       JSON(`{"api_version":1,"sandbox":"web-1","profile":"web","scripts":{"reset-db":"reset-db.sh","update-skills":"update-skills.sh"}}`),
		"script ls -s web-2 --json":       JSON(`{"api_version":1,"sandbox":"web-2","profile":"web","scripts":{"reset-db":"reset-db.sh","update-skills":"update-skills.sh"}}`),
	}
}

const overviewSandboxes = `{"api_version":1,"sandboxes":[
{"name":"web-1","runtime":"sbx","runtime_name":"unibox-web-1","profile":"web","state":"running","memory":"8GiB",
 "settings":{"dockerfile":"sandbox.dockerfile","cpus":"4","memory":"8GiB"},
 "network_allow":["jitpack.io"],
 "tasks":[
  {"id":"gateway-1a2b3c4d","alias":"rate","sandbox":"web-1","directory":"/work/gateway-1a2b3c4d","agent":"claude","state":"ready",
   "repositories":[{"name":"gateway","url":"https://gl.test/acme/gateway.git","branch":"feat/rate","author":"Dev <dev@example.com>"}]},
  {"id":"billing-5e6f7a8b","sandbox":"web-1","directory":"/work/billing-5e6f7a8b","agent":"copilot","state":"ready",
   "repositories":[{"name":"billing","url":"https://gl.test/acme/billing.git","branch":"fix/round","author":"Dev <dev@example.com>"},
                   {"name":"gateway","url":"https://gl.test/acme/gateway.git","branch":"fix/round","author":"Dev <dev@example.com>"}]}],
 "runs":[{"id":"r1","sandbox":"web-1","task":"gateway-1a2b3c4d","agent":"claude","pid":1,"since":"2026-10-19T08:00:00Z"}]},
{"name":"web-2","runtime":"msb","runtime_name":"unibox-web-2","profile":"web","state":"stopped","tasks":[],"runs":[]}]}`
