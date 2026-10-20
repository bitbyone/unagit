package credential

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shortDir is a directory whose socket path fits the 104 bytes macOS
// allows; a test's own temporary directory there is longer.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "uc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestATokenIsHandedOutWhileServedAndNotAfter: the client gets the token
// the handler gives for the name, the handler learns who asked, a refusal
// comes back as its sentence, and once the socket is closed the client
// says unagit is not running.
func TestATokenIsHandedOutWhileServedAndNotAfter(t *testing.T) {
	t.Parallel()
	path := SocketPath(shortDir(t))
	var asked []Request
	var peers []Peer
	s, err := Serve(path, func(r Request, p Peer) (string, error) {
		asked, peers = append(asked, r), append(peers, p)
		if r.Name == "claude" {
			return "sk-ant-oat01-x", nil
		}
		return "", errors.New("no credential " + r.Name + " is kept in unagit")
	})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode: %v %v", info.Mode(), err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode: %v %v", info.Mode(), err)
	}
	token, err := Get(path, "claude")
	if err != nil || token != "sk-ant-oat01-x" {
		t.Fatalf("got %q, %v", token, err)
	}
	if _, err := Get(path, "gitlab"); err == nil || err.Error() != "no credential gitlab is kept in unagit" {
		t.Fatalf("refusal: %v", err)
	}
	if len(peers) != 2 || peers[0].PID != os.Getpid() {
		t.Fatalf("peers %+v", peers)
	}
	// A second unagit does not take the socket over.
	if _, err := Serve(path, nil); !errors.Is(err, ErrServed) {
		t.Fatalf("a live socket was taken over: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(path, "claude"); err == nil || !strings.Contains(err.Error(), "start it and unlock it") {
		t.Fatalf("after close: %v", err)
	}
}

// TestAStaleSocketIsTakenOver: a socket file a crashed run left, which
// nothing answers, is removed and served again.
func TestAStaleSocketIsTakenOver(t *testing.T) {
	t.Parallel()
	path := SocketPath(shortDir(t))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Serve(path, func(Request, Peer) (string, error) { return "t", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if token, err := Get(path, "x"); err != nil || token != "t" {
		t.Fatalf("got %q, %v", token, err)
	}
}
