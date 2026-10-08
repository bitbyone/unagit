// Package zoxide records visits and reads their scores without making
// navigation depend on another tool being installed or responding.
package zoxide

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// timeout bounds every call: a visit not recorded is no reason to keep an
// editor from opening.
const timeout = 2 * time.Second

// Client keeps the executable found for this instance of unagit.
type Client struct {
	binary string
	wait   time.Duration // timeout unless a test needs a slower machine's
}

func New() *Client { return Find(exec.LookPath) }

// Find lets a caller use its own executable lookup, as the integration
// cards do, without changing the environment of other applications.
func Find(lookup func(string) (string, error)) *Client {
	bin, err := lookup("zoxide")
	if err != nil {
		bin = ""
	}
	return &Client{binary: bin}
}

func (c *Client) Binary() string { return c.binary }

// Enabled follows the user's choice; an unset choice follows installation.
func (c *Client) Enabled(choice *bool) bool {
	return c.binary != "" && (choice == nil || *choice)
}

func (c *Client) run(args ...string) ([]byte, error) {
	if c.binary == "" {
		return nil, exec.ErrNotFound
	}
	wait := c.wait
	if wait == 0 {
		wait = timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, args...)
	// A child holding an inherited output pipe must not outlive the timeout.
	cmd.WaitDelay = 50 * time.Millisecond
	return cmd.Output()
}

func (c *Client) Add(dir string) error {
	_, err := c.run("add", "--", dir)
	return err
}

func (c *Client) Remove(dir string) error {
	_, err := c.run("remove", "--", Path(dir))
	return err
}

func (c *Client) Scores() (map[string]float64, error) {
	out, err := c.run("query", "--list", "--score")
	if err != nil {
		return nil, err
	}
	return parseScores(string(out))
}

func parseScores(out string) (map[string]float64, error) {
	scores := map[string]float64{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimLeftFunc(scanner.Text(), unicode.IsSpace)
		if line == "" {
			continue
		}
		sep := strings.IndexFunc(line, unicode.IsSpace)
		if sep < 0 {
			return nil, fmt.Errorf("invalid zoxide score line")
		}
		score, err := strconv.ParseFloat(line[:sep], 64)
		path := strings.TrimLeftFunc(line[sep:], unicode.IsSpace)
		if err != nil || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("invalid zoxide score line")
		}
		scores[filepath.Clean(path)] = score
	}
	return scores, scanner.Err()
}

// Path follows zoxide's optional symlink resolution. After deletion, the
// nearest surviving parent still tells us the spelling its database used.
func Path(dir string) string {
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	dir = filepath.Clean(dir)
	if os.Getenv("_ZO_RESOLVE_SYMLINKS") != "1" {
		return dir
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return dir
	}
	return filepath.Join(Path(parent), filepath.Base(dir))
}
