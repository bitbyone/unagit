package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMain points the usual configuration directory at a throwaway one for
// the whole run. Every test has a configuration of its own, but anything
// that still reaches for the usual directory - a helper written the old way,
// the keychain's account name - would otherwise land in the user's real
// ~/.config/unagit and overwrite the vault there. That has happened once.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "unagit-ui-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("UNAGIT_CONFIG_DIR", dir)
	os.Setenv("_ZO_DATA_DIR", filepath.Join(dir, "zoxide"))
	fixtureRoot = dir
	// Whether the terminal running the tests draws Nerd Font icons is
	// nothing the tests should depend on.
	nerdFontGuess = func() (bool, string) { return false, "tests" }
	// Nor whether they run under a multiplexer, which changes what the
	// Notifications card says.
	for _, name := range []string{"ZELLIJ", "HERDR_ENV", "HERDR_PANE_ID"} {
		os.Unsetenv(name)
	}
	limitParallel()
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// parallelTests is how many tests run at once unless -parallel says
// otherwise. More simulation screens together have not shown a clear gain
// under race, even after removing repeated key derivation and polling. See
// docs/testing.md for the measurements; -parallel still overrides this.
const parallelTests = 4

// limitParallel applies parallelTests when the command line did not choose.
func limitParallel() {
	flag.Parse()
	chosen := false
	flag.Visit(func(f *flag.Flag) { chosen = chosen || f.Name == "test.parallel" })
	if !chosen {
		flag.Set("test.parallel", fmt.Sprint(parallelTests))
	}
}

// patience is how long a test waits for something to appear before it gives
// up. The tests run in parallel, each with an application and often a git
// repository of its own, so on a busy machine a screen can take seconds to
// catch up; a wait ends the moment its condition holds, so only a failing
// test ever waits this long.
const patience = 30 * time.Second
