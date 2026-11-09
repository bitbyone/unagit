package gitx

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tobola/unagit/internal/platform"
)

// A clone watched for local changes says what came from outside unagit - an
// agent's commit, a tag made in a terminal - and so has to know what unagit
// did itself. Every git command of unagit's that can move a branch or a tag
// in such a clone notes the refs it moved in <git common dir>/unagit-own.json,
// which every instance reads; the file being there is what marks the clone
// as watched, so a clone nobody watches costs nothing more than a stat.

// ownFile is the note's name in the common git directory.
const ownFile = "unagit-own.json"

// ownKept is how long a note of unagit's own move stands: long enough for
// any watcher to have looked, short enough that the file stays small.
const ownKept = time.Hour

// ownMove is where unagit left a ref, "" for deleted, and when.
type ownMove struct {
	To string    `json:"to"`
	At time.Time `json:"at"`
}

// commonDirs caches each directory's common git directory: it does not
// change, and asking costs a process.
var commonDirs sync.Map

// ownLock keeps an instance's writers of a note from losing each other's
// moves; two instances writing in the same instant is left to chance.
var ownLock sync.Mutex

// CommonDir is the git directory a checkout's worktrees share.
func CommonDir(dir string) (string, error) {
	if c, ok := commonDirs.Load(dir); ok {
		return c.(string), nil
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", err
	}
	c := strings.TrimSpace(string(out))
	commonDirs.Store(dir, c)
	return c, nil
}

// Follow marks a clone as watched for local changes, or no longer.
func Follow(dir string, on bool) error {
	common, err := CommonDir(dir)
	if err != nil {
		return err
	}
	path := filepath.Join(common, ownFile)
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return writeOwn(path, map[string]ownMove{})
}

// followed is the note of a watched clone, "" when nobody watches it.
func followed(dir string) string {
	common, err := CommonDir(dir)
	if err != nil {
		return ""
	}
	path := filepath.Join(common, ownFile)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// OwnRefs are the refs unagit itself last moved in a watched clone, by
// full name, each where it left it ("" deleted).
func OwnRefs(dir string) map[string]string {
	path := followed(dir)
	if path == "" {
		return nil
	}
	out := map[string]string{}
	for ref, m := range readOwn(path) {
		out[ref] = m.To
	}
	return out
}

func readOwn(path string) map[string]ownMove {
	moves := map[string]ownMove{}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &moves)
	}
	return moves
}

func writeOwn(path string, moves map[string]ownMove) error {
	b, err := json.Marshal(moves)
	if err != nil {
		return err
	}
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return platform.Replace(tmp, path)
}

// noteOwn writes down which refs moved between before and after.
func noteOwn(path string, before, after map[string]string) {
	now := time.Now()
	moved := map[string]ownMove{}
	for ref, sha := range after {
		if before[ref] != sha {
			moved[ref] = ownMove{To: sha, At: now}
		}
	}
	for ref := range before {
		if _, ok := after[ref]; !ok {
			moved[ref] = ownMove{At: now}
		}
	}
	if len(moved) == 0 {
		return
	}
	ownLock.Lock()
	defer ownLock.Unlock()
	moves := readOwn(path)
	for ref, m := range moves {
		if now.Sub(m.At) > ownKept {
			delete(moves, ref)
		}
	}
	for ref, m := range moved {
		moves[ref] = m
	}
	writeOwn(path, moves)
}

// busyKept is how long a mark of a command under way stands, should the
// process die before taking it back.
const busyKept = 10 * time.Minute

var busySeq atomic.Int64

// markBusy says a command of unagit's is moving refs until the returned
// func is called: in between, a watcher may already see a ref moved that
// the note does not name yet, and must not take it for news.
func markBusy(own string) func() {
	path := own + ".busy-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(busySeq.Add(1), 10)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return func() {}
	}
	return func() { os.Remove(path) }
}

// Busy reports whether a command of unagit's, of any instance, is moving
// refs in a watched clone right now. A watcher asks after reading the refs
// and before reading the note: a move seen then is either in the note or
// still marked.
func Busy(dir string) bool {
	path := followed(dir)
	if path == "" {
		return false
	}
	marks, _ := filepath.Glob(path + ".busy-*")
	for _, m := range marks {
		if st, err := os.Stat(m); err == nil && time.Since(st.ModTime()) < busyKept {
			return true
		}
	}
	return false
}

// movesRefs reports whether a git command can move a branch or a tag.
func movesRefs(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c" || a == "-C":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			switch a {
			case "commit", "branch", "tag", "checkout", "switch", "reset", "rebase", "merge", "pull",
				"fetch", "cherry-pick", "revert", "update-ref", "worktree", "am", "stash":
				return true
			}
			return false
		}
	}
	return false
}

// LocalRefs are a clone's branches and tags, by full name, each with the
// object it points at.
func LocalRefs(dir string) (map[string]string, error) {
	out, err := exec.Command("git", "-C", dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/tags").Output()
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if ref, sha, ok := strings.Cut(line, " "); ok {
			refs[ref] = sha
		}
	}
	return refs, nil
}

// NewCommits are the commits to that from lacks: how many, and the newest's
// author and subject.
func NewCommits(dir, from, to string) (n int, by, subject string, err error) {
	out, err := exec.Command("git", "-C", dir, "log", "--format=%an%x1f%s", from+".."+to).Output()
	if err != nil {
		return 0, "", "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if lines[0] == "" {
		return 0, "", "", nil
	}
	by, subject, _ = strings.Cut(lines[0], "\x1f")
	return len(lines), by, subject, nil
}
