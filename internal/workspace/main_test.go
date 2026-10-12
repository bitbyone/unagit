package workspace

import (
	"testing"

	"github.com/tobola/unagit/internal/testenv"
)

// TestMain runs in the tests' own environment, which also gives git the
// identity a rebase needs to write commits.
func TestMain(m *testing.M) { testenv.Main(m) }
