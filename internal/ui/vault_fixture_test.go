package ui

import (
	"sort"
	"sync"

	"github.com/tobola/unagit/internal/secret"
)

// Most interface tests only need the tokens of an already unlocked vault.
// Those that save or rekey it still use the real encryption, with its usual
// cost and a passphrase known only to the test.
type memoryVault struct {
	tokens map[string]string
	sealed *secret.Vault
	// creds are read by the credential socket's goroutine, hence the lock.
	mu    sync.Mutex
	creds map[string]secret.Credential
}

func (v *memoryVault) Credential(name string) (secret.Credential, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.creds[name]
	return c, ok && c.Token != ""
}

func (v *memoryVault) SetCredential(c secret.Credential) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.creds == nil {
		v.creds = map[string]secret.Credential{}
	}
	if c.Token == "" {
		c.Token = v.creds[c.Name].Token
	}
	v.creds[c.Name] = c
}

func (v *memoryVault) RemoveCredential(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.creds, name)
}

func (v *memoryVault) Credentials() []secret.Credential {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []secret.Credential
	for _, c := range v.creds {
		c.Token = ""
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (v *memoryVault) Token(id string) string { return v.tokens[id] }
func (v *memoryVault) Has(id string) bool     { return v.Token(id) != "" }
func (v *memoryVault) Remove(id string)       { delete(v.tokens, id) }
func (v *memoryVault) Set(id, token string) {
	if token == "" {
		v.Remove(id)
	} else {
		v.tokens[id] = token
	}
}

func (v *memoryVault) IDs() []string {
	ids := make([]string, 0, len(v.tokens))
	for id := range v.tokens {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (v *memoryVault) prepare() error {
	if v.sealed == nil {
		var err error
		v.sealed, err = secret.NewVault([]byte("test-passphrase"))
		if err != nil {
			return err
		}
	}
	for _, id := range v.sealed.IDs() {
		v.sealed.Remove(id)
	}
	for id, token := range v.tokens {
		v.sealed.Set(id, token)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, c := range v.sealed.Credentials() {
		v.sealed.RemoveCredential(c.Name)
	}
	for _, c := range v.creds {
		v.sealed.SetCredential(c)
	}
	return nil
}

func (v *memoryVault) Save(path string) error {
	if err := v.prepare(); err != nil {
		return err
	}
	return v.sealed.Save(path)
}

func (v *memoryVault) Rekey(passphrase []byte) error {
	if err := v.prepare(); err != nil {
		return err
	}
	return v.sealed.Rekey(passphrase)
}
