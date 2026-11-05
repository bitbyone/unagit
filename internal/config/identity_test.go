package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestAnIdentityIsInherited: each half comes from the nearest that sets
// it - the repository, its groups from the bottom, the server, General -
// and the file keeps only what was set.
func TestAnIdentityIsInherited(t *testing.T) {
	t.Parallel()
	var c Config
	c.SetIdentity("", "", GitIdentity{Name: "Jan", Email: "jan@home.example"})
	c.SetIdentity("work", "", GitIdentity{Email: "jan@work.example"})
	c.SetIdentity("work", "acme/secret", GitIdentity{Email: " jan@secret.example "})
	c.SetIdentity("work", "acme/secret/vault", GitIdentity{Name: "J. T."})

	for _, want := range []struct {
		instance, path string
		id             Identity
	}{
		{"home", "me/dots", Identity{GitIdentity{"Jan", "jan@home.example"}, IdentityGlobal, IdentityGlobal}},
		{"work", "acme/api", Identity{GitIdentity{"Jan", "jan@work.example"}, IdentityGlobal, IdentityServer}},
		{"work", "acme/secret/x/y", Identity{GitIdentity{"Jan", "jan@secret.example"}, IdentityGlobal, "acme/secret"}},
		{"work", "acme/secret/vault", Identity{GitIdentity{"J. T.", "jan@secret.example"}, "acme/secret/vault", "acme/secret"}},
	} {
		if got := c.IdentityOf(want.instance, want.path); got != want.id {
			t.Errorf("%s %s: %+v, want %+v", want.instance, want.path, got, want.id)
		}
	}
	if got := c.InheritedIdentity("work", "acme/secret/vault"); got.Name != "Jan" || got.EmailFrom != "acme/secret" {
		t.Errorf("the vault inherits %+v", got)
	}

	c.SetIdentity("work", "acme/secret", GitIdentity{})
	if got := c.IdentityOf("work", "acme/secret/x"); got.Email != "jan@work.example" {
		t.Errorf("an identity taken away still counts: %+v", got)
	}
	out, err := yaml.Marshal(Config{Identities: c.Identities})
	if err != nil {
		t.Fatal(err)
	}
	if want := "email: jan@work.example"; !strings.Contains(string(out), want) || strings.Contains(string(out), "git_identity") || strings.Contains(string(out), "name: \"\"") {
		t.Errorf("the file says:\n%s", out)
	}
}
