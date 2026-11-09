package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// The refs a local watch last saw are a file of their own, local/<key>.json,
// not part of state.json: a clone can have thousands of tags, and every
// instance reads state.json again whenever it moves. Only the poller
// writes them.

func localName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join("local", hex.EncodeToString(sum[:12])+".json")
}

// LocalRefs are the branches and tags a local watch last saw, by their
// full names, and whether it saw any yet.
func (s *Store) LocalRefs(key string) (map[string]string, bool) {
	var refs map[string]string
	if err := readJSON(s.path(localName(key)), &refs); err != nil || refs == nil {
		return nil, false
	}
	return refs, true
}

// WriteLocalRefs keeps what a local watch saw.
func (s *Store) WriteLocalRefs(key string, refs map[string]string) error {
	if err := s.secureDirectory(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.path("local"), 0o700); err != nil {
		return err
	}
	if refs == nil {
		refs = map[string]string{}
	}
	return s.writeJSON(localName(key), refs)
}

// ForgetLocalRefs lets a local watch's refs go with the watch.
func (s *Store) ForgetLocalRefs(key string) { os.Remove(s.path(localName(key))) }
