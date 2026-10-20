// Package credential hands the tokens unagit keeps for unibox's sandboxes
// to the runtime that asks for them, over a socket of the running,
// unlocked unagit. sbx stores a command for each such token and runs it
// on the host whenever it wants the value; that command is unagit
// credential get, a client of this socket with no vault of its own. So a
// token is handed out only while the user's unagit is up and unlocked -
// nothing outlives the process, and nothing keeps the passphrase.
//
// Any process of the same user can connect while the socket exists; the
// peer check refuses other users only. What bounds it is that only the
// tokens stored for sandboxes are ever handed out, never the forge tokens,
// and that every handout is written down where the user sees it.
package credential

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Version is the protocol's: one JSON line asked, one answered.
const Version = 1

// Request asks for the token stored under a name.
type Request struct {
	V    int    `json:"v"`
	Name string `json:"name"`
}

// Answer is the token, or why there is none.
type Answer struct {
	V     int    `json:"v"`
	Token string `json:"token,omitempty"`
	Error string `json:"error,omitempty"`
}

// Peer is the process that asked, as far as the platform says: the
// client's own pid, and the program that ran it - sbx's daemon, or
// whatever else did.
type Peer struct {
	PID    int
	Parent string
}

// Handler answers a request with a token, or an error the client prints.
type Handler func(Request, Peer) (string, error)

// ErrServed means another unagit serves the socket already.
var ErrServed = errors.New("another unagit hands out the sandbox credentials already")

// timeout bounds a connection on both sides: the runtime waits on the
// client, and a stuck peer must not hold a goroutine for long.
const timeout = 5 * time.Second

// SocketPath is the socket under a configuration directory.
func SocketPath(dir string) string { return filepath.Join(dir, "run", "credential.sock") }

// Server is the listening socket.
type Server struct {
	ln   net.Listener
	path string
	once sync.Once
	wg   sync.WaitGroup
}

// Serve listens at path and answers each connection with h. A socket left
// by a run that crashed is taken over; a live one is another unagit's.
func Serve(path string, h Handler) (*Server, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, ErrServed
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{ln: ln, path: path}
	s.wg.Add(1)
	go s.accept(h)
	return s, nil
}

func (s *Server) accept(h Handler) {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.answer(c.(*net.UnixConn), h)
	}
}

func (s *Server) answer(c *net.UnixConn, h Handler) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	reply := func(a Answer) {
		a.V = Version
		b, _ := json.Marshal(a)
		_, _ = c.Write(append(b, '\n'))
	}
	uid, pid, err := peerOf(c)
	if err != nil || uid != os.Getuid() {
		reply(Answer{Error: "refused: the credential socket answers its own user alone"})
		return
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil || req.V != Version || req.Name == "" {
		reply(Answer{Error: "unagit does not understand this request; use the unagit credential get of the same unagit"})
		return
	}
	token, err := h(req, Peer{PID: pid, Parent: parentOf(pid)})
	if err != nil {
		reply(Answer{Error: err.Error()})
		return
	}
	reply(Answer{Token: token})
}

// Close stops listening and removes the socket, so a client says unagit
// is not running rather than waiting.
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		err = s.ln.Close()
		s.wg.Wait()
		_ = os.Remove(s.path)
	})
	return err
}

// Get asks the socket at path for the token stored under name.
func Get(path, name string) (string, error) {
	c, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
			return "", errors.New("unagit is not running, or is locked; start it and unlock it")
		}
		return "", fmt.Errorf("cannot reach unagit at %s: %w", path, err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	b, _ := json.Marshal(Request{V: Version, Name: name})
	if _, err := c.Write(append(b, '\n')); err != nil {
		return "", fmt.Errorf("unagit did not take the request: %w", err)
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return "", fmt.Errorf("unagit did not answer: %w", err)
	}
	var a Answer
	if err := json.Unmarshal(line, &a); err != nil {
		return "", errors.New("unagit answered in a form this one does not know; use the same unagit for both")
	}
	if a.Error != "" {
		return "", errors.New(a.Error)
	}
	if a.Token == "" {
		return "", errors.New("unagit answered with no token")
	}
	return a.Token, nil
}
