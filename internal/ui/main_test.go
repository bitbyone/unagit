package ui

import (
	"flag"
	"fmt"
	"os"
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
	limitParallel()
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// parallelTests is how many tests run at once unless -parallel says
// otherwise. Under the race detector, which make test always uses, its
// runtime takes a lock for much of what the applications do, and past four
// at once the run gets slower rather than faster: measured on 18 cores, 4
// took 127 s, 8 took 170 s and one per core 215 s, against 263 s serial.
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
