package config

import (
	"slices"
	"strings"
)

// GitIdentity is who a commit says made it: git's user.name and user.email.
// Either may be empty, and is then inherited from above.
type GitIdentity struct {
	Name  string `yaml:"name,omitempty"`
	Email string `yaml:"email,omitempty"`
}

// Empty reports whether it says nothing at all.
func (id GitIdentity) Empty() bool { return id.Name == "" && id.Email == "" }

// String is the identity as a commit shows it: "Jan <jan@example.com>".
func (id GitIdentity) String() string {
	switch {
	case id.Name != "" && id.Email != "":
		return id.Name + " <" + id.Email + ">"
	case id.Email != "":
		return "<" + id.Email + ">"
	}
	return id.Name
}

// IdentitySet is the identity set for a server (Path ""), a group or a
// repository; everything below it inherits what it sets.
type IdentitySet struct {
	Instance    string `yaml:"instance"`
	Path        string `yaml:"path"`
	GitIdentity `yaml:",inline"`
}

// Identity is an identity as it comes out for one repository, and where each
// half was set: "" when nowhere, IdentityGlobal, IdentityServer, a group's
// path, or the repository's own path.
type Identity struct {
	GitIdentity
	NameFrom, EmailFrom string
}

// Where an identity can come from, besides a group's or a repository's path.
const (
	IdentityGlobal = "general"
	IdentityServer = "server"
)

// IdentityOf is the identity a repository's commits are made under, as
// unagit writes it into the clone: the one in General, then the server's,
// then each group from the top, then the repository's own, each half the
// nearest that sets it.
func (c *Config) IdentityOf(instance, path string) Identity {
	var out Identity
	lay := func(id GitIdentity, from string) {
		if id.Name != "" {
			out.Name, out.NameFrom = id.Name, from
		}
		if id.Email != "" {
			out.Email, out.EmailFrom = id.Email, from
		}
	}
	lay(c.GitIdentity, IdentityGlobal)
	if instance == "" {
		return out
	}
	if s := findIdentity(c.Identities, instance, ""); s != nil {
		lay(s.GitIdentity, IdentityServer)
	}
	if path == "" {
		return out
	}
	parts := strings.Split(path, "/")
	for i := 1; i <= len(parts); i++ {
		at := strings.Join(parts[:i], "/")
		if s := findIdentity(c.Identities, instance, at); s != nil {
			lay(s.GitIdentity, at)
		}
	}
	return out
}

// InheritedIdentity is what a server, group or repository gets from above
// it, before its own.
func (c *Config) InheritedIdentity(instance, path string) Identity {
	if instance == "" {
		return Identity{}
	}
	if path == "" {
		return c.IdentityOf("", "")
	}
	parent := ""
	if i := strings.LastIndex(path, "/"); i > 0 {
		parent = path[:i]
	}
	if parent == "" {
		return c.IdentityOf(instance, "")
	}
	return c.IdentityOf(instance, parent)
}

// OwnIdentity is what is set for a server (path ""), a group or a
// repository itself, inherited halves left out.
func (c *Config) OwnIdentity(instance, path string) GitIdentity {
	if instance == "" {
		return c.GitIdentity
	}
	if s := findIdentity(c.Identities, instance, path); s != nil {
		return s.GitIdentity
	}
	return GitIdentity{}
}

// SetIdentity sets the identity of a server (path ""), a group or a
// repository; an instance of "" is General's. An empty one inherits again.
func (c *Config) SetIdentity(instance, path string, id GitIdentity) {
	id.Name, id.Email = strings.TrimSpace(id.Name), strings.TrimSpace(id.Email)
	if instance == "" {
		c.GitIdentity = id
		return
	}
	c.Identities = slices.DeleteFunc(c.Identities, func(s IdentitySet) bool {
		return s.Instance == instance && s.Path == path
	})
	if !id.Empty() {
		c.Identities = append(c.Identities, IdentitySet{Instance: instance, Path: path, GitIdentity: id})
	}
}

func findIdentity(sets []IdentitySet, instance, path string) *IdentitySet {
	for i := range sets {
		if sets[i].Instance == instance && sets[i].Path == path {
			return &sets[i]
		}
	}
	return nil
}
