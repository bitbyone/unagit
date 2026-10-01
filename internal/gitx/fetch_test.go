package gitx

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// TestFetchesIntoOneRepositoryTakeTurns: two fetches at once - one from the
// clone, one from a worktree of it - both get through, though git alone would
// fail one of them for moving a ref under the other.
func TestFetchesIntoOneRepositoryTakeTurns(t *testing.T) {
	origin, clone := repos(t)
	pusher := filepath.Join(t.TempDir(), "pusher")
	sh(t, filepath.Dir(pusher), "clone", "-q", origin, pusher)
	wt := filepath.Join(t.TempDir(), "wt")
	sh(t, clone, "worktree", "add", "-q", "-b", "side", wt)
	g := New("", nil)
	for i := range 8 {
		commit(t, pusher, "f.txt", fmt.Sprintf("change %d", i))
		sh(t, pusher, "push", "-q", "origin", "main", fmt.Sprintf("HEAD:refs/heads/b%d", i))
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for j, dir := range []string{clone, wt} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[j] = g.Fetch(dir)
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", i, err)
			}
		}
	}
}
