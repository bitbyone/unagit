package gitx

import (
	"bufio"
	"strings"
)

// identityMark says unagit wrote the clone's user.name and user.email, so it
// may take them out again when the user no longer sets an identity; ones
// the clone had of its own are not unagit's to remove.
const identityMark = "unagit.identity"

// LocalIdentity is the user.name and user.email in a clone's own
// configuration - not the global one git falls back on - and whether unagit
// wrote them.
func (g *Git) LocalIdentity(dir string) (name, email string, unagits bool) {
	// One git for the three keys: this is read for every clone on every
	// look at the disk. Nothing set is an exit of 1, not an error worth
	// telling apart.
	out, _ := g.Run(dir, "config", "--local", "--get-regexp", `^(user\.name|user\.email|unagit\.identity)$`)
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		key, value, _ := strings.Cut(sc.Text(), " ")
		switch key {
		case "user.name":
			name = value
		case "user.email":
			email = value
		case identityMark:
			unagits = value == "true"
		}
	}
	return name, email, unagits
}

// SetIdentity makes a clone's own user.name and user.email the ones given,
// whatever it had before, and reports whether anything changed. Worktrees
// share the clone's configuration, so they commit as the same person. An
// empty half that unagit wrote before is taken out again, so git falls back
// on the global one; an empty half the clone had of its own is left.
func (g *Git) SetIdentity(dir, name, email string) (bool, error) {
	// Two writers of one configuration at once - a look at the disk and a
	// save in Settings - and git refuses the second its lock.
	defer lockRepository(dir)()
	hadName, hadEmail, unagits := g.LocalIdentity(dir)
	changed := false
	put := func(key, want, had string) error {
		switch {
		case want != "" && want != had:
			changed = true
			_, err := g.Run(dir, "config", "--local", key, want)
			return err
		case want == "" && had != "" && unagits:
			changed = true
			_, err := g.Run(dir, "config", "--local", "--unset-all", key)
			return err
		}
		return nil
	}
	if err := put("user.name", name, hadName); err != nil {
		return changed, err
	}
	if err := put("user.email", email, hadEmail); err != nil {
		return changed, err
	}
	switch {
	case (name != "" || email != "") && !unagits:
		_, err := g.Run(dir, "config", "--local", identityMark, "true")
		return changed, err
	case name == "" && email == "" && unagits:
		_, err := g.Run(dir, "config", "--local", "--unset-all", identityMark)
		return changed, err
	}
	return changed, nil
}
