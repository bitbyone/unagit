package ui

import (
	"sort"

	"github.com/tobola/unagit/internal/secret"
)

// Most interface tests only need the tokens of an already unlocked vault.
// Those that save or rekey it still use the real encryption, with its usual
// cost and a passphrase known only to the test.
type memoryVault struct {
	tokens map[string]string
	sealed *secret.Vault
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
