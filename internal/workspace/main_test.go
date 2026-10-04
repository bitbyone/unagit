package workspace

import (
	"os"
	"testing"
)

// TestMain gives git an identity for the whole run: a rebase writes commits,
// and the user running the tests may have none configured. Set once here
// rather than by each test, so that the tests can run in parallel.
func TestMain(m *testing.M) {
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "test"}, {"GIT_AUTHOR_EMAIL", "test@example.com"},
		{"GIT_COMMITTER_NAME", "test"}, {"GIT_COMMITTER_EMAIL", "test@example.com"}} {
		os.Setenv(kv[0], kv[1])
	}
	os.Exit(m.Run())
}
