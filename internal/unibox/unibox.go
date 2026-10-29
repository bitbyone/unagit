// Package unibox drives the unibox command line: the envs a team describes
// as sbx's env files, the sandboxes sbx creates from them and the tasks in
// each. unibox is a project of its own, and unagit imports nothing of it -
// it runs the binary and reads its versioned --json, decoded here into types
// of unagit's own, written against unibox's internal/unibox/api.
//
// Nothing here reads unibox's YAML, an sbxenv file or its state: every
// answer comes from a command, and every change is a command, so what
// unagit shows is what unibox itself would say.
package unibox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// APIVersion is the version of unibox's JSON contract this unagit is written
// against. unibox raises it only when a field changes meaning or goes, so
// another one is refused rather than half understood.
const APIVersion = 2

// readTimeout keeps a unibox that waits on its runtime - a daemon not
// answering - from holding a list for ever.
var readTimeout = 30 * time.Second

// Client runs one unibox binary.
type Client struct {
	Binary string
}

// New is a client of the binary at a path.
func New(binary string) *Client { return &Client{Binary: binary} }

// Error is a failure unibox reported under --json: a code a program can
// act on, its message for people, and the exit status, which says what kind
// of failure it was (4 a refusal).
type Error struct {
	Code    string
	Message string
	Status  int
}

func (e *Error) Error() string { return e.Message }

// Refused reports whether unibox refused rather than failed: work it would
// not discard, a sandbox busy with a session, a question it needs answered.
func (e *Error) Refused() bool { return e.Status == 4 }

// VersionError is a unibox speaking a JSON contract this unagit does not
// know.
type VersionError struct{ Got int }

func (e *VersionError) Error() string {
	if e.Got > APIVersion {
		return fmt.Sprintf("unibox speaks API version %d and this unagit knows %d - update unagit", e.Got, APIVersion)
	}
	return fmt.Sprintf("unibox speaks API version %d and this unagit knows %d - update unibox", e.Got, APIVersion)
}

// envelope is what every --json answer carries besides its own fields.
type envelope struct {
	APIVersion int    `json:"api_version"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	OK         bool   `json:"ok"`
}

// JSON runs a command with --json and decodes its answer into out, which may
// be nil for a command whose answer is only that it worked.
func (c *Client) JSON(ctx context.Context, out any, args ...string) error {
	_, err := c.jsonAnswer(ctx, out, args...)
	return err
}

// Do runs a command that changes something and returns what unibox said it
// did.
func (c *Client) Do(ctx context.Context, args ...string) (string, error) {
	env, err := c.jsonAnswer(ctx, nil, args...)
	return env.Message, err
}

func (c *Client) jsonAnswer(ctx context.Context, out any, args ...string) (envelope, error) {
	cmd := exec.CommandContext(ctx, c.Binary, append(append([]string{}, args...), "--json")...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		if runErr != nil {
			return env, c.failed(args, runErr, stderr.String())
		}
		return env, fmt.Errorf("unibox %s answered what unagit cannot read - update unagit or unibox", strings.Join(args, " "))
	}
	if env.APIVersion != APIVersion {
		return env, &VersionError{Got: env.APIVersion}
	}
	if runErr != nil {
		status := 1
		var exit *exec.ExitError
		if errors.As(runErr, &exit) {
			status = exit.ExitCode()
		}
		message := env.Message
		if message == "" {
			message = firstLine(stderr.String(), runErr.Error())
		}
		return env, &Error{Code: env.Code, Message: message, Status: status}
	}
	if out != nil {
		if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
			return env, fmt.Errorf("unibox %s answered what unagit cannot read - update unagit or unibox: %v", strings.Join(args, " "), err)
		}
	}
	return env, nil
}

func (c *Client) failed(args []string, err error, stderr string) error {
	var notFound *exec.Error
	if errors.As(err, &notFound) || errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("unibox is not installed at %s - install it, see Settings › Integrations", c.Binary)
	}
	return fmt.Errorf("unibox %s: %s", strings.Join(args, " "), firstLine(stderr, err.Error()))
}

// firstLine is the first line of what a command said, without unibox's own
// "unibox:" before it, or the fallback when it said nothing.
func firstLine(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "unibox:")); line != "" {
			return line
		}
	}
	return fallback
}

// Stream runs a command whose progress is worth reading - an update of the
// team's repository, a prune - and hands its output to say a line at a time, colours and
// all: its output goes to a terminal of its own, so sbx and docker colour
// it as they do on the user's. Its input stays none, so nothing it would
// ask on a terminal waits for an answer, and docker's progress is plain,
// line after line, rather than redrawn in place.
func (c *Client) Stream(ctx context.Context, say func(string), args ...string) error {
	cmd := exec.CommandContext(ctx, c.Binary, append(append([]string{}, args...), "--yes")...)
	cmd.Env = append(os.Environ(), "BUILDKIT_PROGRESS=plain", "TERM=xterm-256color")
	var reader io.Reader
	var writer io.WriteCloser
	if master, tty, err := pty.Open(); err == nil {
		_ = pty.Setsize(master, &pty.Winsize{Rows: 50, Cols: 160})
		defer master.Close()
		// The child holds its own copy of the terminal; this one is closed
		// once it started, so the master reads its end when it exits.
		cmd.Stdout, cmd.Stderr = tty, tty
		reader, writer = ptyReader{master}, tty
	} else {
		pr, pw := io.Pipe()
		cmd.Stdout, cmd.Stderr = pw, pw
		reader, writer = pr, pw
	}
	var last string
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		scan := bufio.NewScanner(reader)
		scan.Buffer(make([]byte, 64*1024), 1024*1024)
		for scan.Scan() {
			line := strings.TrimRight(scan.Text(), "\r")
			if plain := strings.TrimSpace(StripANSI(line)); plain != "" {
				mu.Lock()
				last = plain
				mu.Unlock()
			}
			say(line)
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	err := cmd.Start()
	if _, isTTY := writer.(*os.File); isTTY {
		_ = writer.Close()
	}
	if err == nil {
		err = cmd.Wait()
	}
	_ = writer.Close()
	<-done
	if err != nil {
		mu.Lock()
		defer mu.Unlock()
		return c.failed(args, err, last)
	}
	return nil
}

// ptyReader reads a terminal's master end, taking the error Linux gives
// once the other end is closed for the end of the output it is.
type ptyReader struct{ f *os.File }

func (r ptyReader) Read(b []byte) (int, error) {
	n, err := r.f.Read(b)
	if err != nil && errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return n, err
}

// ansiSequence is an escape sequence a terminal reads: a colour, a move
// of the cursor, a title.
var ansiSequence = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

// StripANSI is text as it reads without the terminal's escape sequences.
func StripANSI(s string) string { return ansiSequence.ReplaceAllString(s, "") }

// CommandHolding is Command for what runs in a terminal of its own - an
// agent, a shell: when unibox stops with an error it waits for Enter, so
// what it said on the terminal is read before the terminal is unagit's
// again, or its pane closes.
func (c *Client) CommandHolding(args ...string) *exec.Cmd {
	const hold = `"$@"; s=$?; if [ $s -ne 0 ] && [ $s -ne 130 ]; then printf '\nunibox stopped (exit %s) - press Enter to go back\n' "$s"; read _; fi; exit $s`
	return exec.Command("/bin/sh", append([]string{"-c", hold, "unibox", c.Binary}, args...)...)
}

// Command is a command run on a terminal: an agent, a shell, a sign-in that
// asks for a token where unagit never sees it.
func (c *Client) Command(args ...string) *exec.Cmd {
	return exec.Command(c.Binary, args...)
}

// read is JSON with the timeout a list is given.
func (c *Client) read(out any, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	return c.JSON(ctx, out, args...)
}
