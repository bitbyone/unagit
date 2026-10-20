package secret

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// Vault holds the GitLab tokens, one per instance.
//
// The file is a single AES-256-GCM blob whose key comes from one passphrase.
// Once opened, the derived key stays in memory so tokens can be added or
// changed from the interface without asking for the passphrase again; the
// passphrase itself is wiped by the caller.
//
// Beside the forge tokens it keeps the tokens handed to unibox's sandboxes
// (Credential): narrow ones, stored for that alone, which the credential
// socket reads from its own goroutine - hence the lock.
type Vault struct {
	mu     sync.RWMutex
	key    []byte
	salt   []byte
	params params
	tokens map[string]string
	creds  map[string]Credential
}

type vaultData struct {
	Tokens      map[string]string     `json:"tokens"`
	Credentials map[string]Credential `json:"credentials,omitempty"`
}

// Credential is a token kept for unibox's sandboxes and handed out, by its
// name, to unagit credential get while unagit runs unlocked. Kind says what
// signs in with it - an agent or a service - and Hosts where a service's is
// used; both are for the user to read, the runtime keeps the hosts itself.
type Credential struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Hosts []string `json:"hosts,omitempty"`
	Token string   `json:"token"`
}

const (
	CredentialAgent   = "agent"
	CredentialService = "service"
)

// NewVault creates an empty vault protected by passphrase.
func NewVault(passphrase []byte) (*Vault, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return &Vault{
		key:    derive(passphrase, salt, defaultParams),
		salt:   salt,
		params: defaultParams,
		tokens: map[string]string{},
		creds:  map[string]Credential{},
	}, nil
}

// OpenVault decrypts a vault file.
func OpenVault(path string, passphrase []byte) (*Vault, error) {
	blob, err := Load(path)
	if err != nil {
		return nil, err
	}
	return openBlob(blob, passphrase)
}

func openBlob(b *Blob, passphrase []byte) (*Vault, error) {
	if b.KDF != kdfArgon2id {
		return nil, fmt.Errorf("unsupported kdf %q", b.KDF)
	}
	salt, err := base64.StdEncoding.DecodeString(b.Salt)
	if err != nil {
		return nil, fmt.Errorf("corrupt salt: %w", err)
	}
	key := derive(passphrase, salt, b.Params)
	plain, err := openWithKey(b, key)
	if err != nil {
		return nil, err
	}
	var data vaultData
	if err := json.Unmarshal(plain, &data); err != nil {
		return nil, fmt.Errorf("corrupt vault: %w", err)
	}
	if data.Tokens == nil {
		data.Tokens = map[string]string{}
	}
	if data.Credentials == nil {
		data.Credentials = map[string]Credential{}
	}
	return &Vault{key: key, salt: salt, params: b.Params, tokens: data.Tokens, creds: data.Credentials}, nil
}

// OpenOrCreate opens the vault at path, or starts an empty one when the file
// does not exist yet. It reports whether the vault is new.
func OpenOrCreate(path string, passphrase []byte) (*Vault, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, false, err
		}
		v, err := NewVault(passphrase)
		return v, true, err
	}
	v, err := OpenVault(path, passphrase)
	return v, false, err
}

// ImportSingleToken folds a pre-vault token.enc into the vault under id. The
// old file has its own salt, so it needs the passphrase rather than the
// vault's key. It is a no-op when the old file is absent or already imported.
func (v *Vault) ImportSingleToken(path, id string, passphrase []byte) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.tokens[id]; ok {
		return false, nil
	}
	blob, err := Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	token, err := Decrypt(blob, passphrase)
	if err != nil {
		return false, err
	}
	v.tokens[id] = string(token)
	return true, nil
}

// Token returns the token stored for an instance, or "".
func (v *Vault) Token(id string) string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.tokens[id]
}

// Has reports whether a token is stored for an instance.
func (v *Vault) Has(id string) bool { return v.Token(id) != "" }

// Set stores a token. An empty token removes the entry.
func (v *Vault) Set(id, token string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if token == "" {
		delete(v.tokens, id)
		return
	}
	v.tokens[id] = token
}

// Remove drops an instance's token.
func (v *Vault) Remove(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.tokens, id)
}

// IDs lists the instances that have a token, sorted.
func (v *Vault) IDs() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	ids := make([]string, 0, len(v.tokens))
	for id := range v.tokens {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Credential is the sandbox credential stored under name.
func (v *Vault) Credential(name string) (Credential, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	c, ok := v.creds[name]
	return c, ok && c.Token != ""
}

// SetCredential stores a sandbox credential under its name. One without a
// token keeps the token stored before, so its hosts can change alone.
func (v *Vault) SetCredential(c Credential) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if c.Token == "" {
		c.Token = v.creds[c.Name].Token
	}
	v.creds[c.Name] = c
}

// RemoveCredential drops a sandbox credential.
func (v *Vault) RemoveCredential(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.creds, name)
}

// Credentials lists the sandbox credentials by name, their tokens left
// out: what is listed is never what is handed out.
func (v *Vault) Credentials() []Credential {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]Credential, 0, len(v.creds))
	for _, c := range v.creds {
		c.Token = ""
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Save writes the vault back to disk, sealed with a fresh nonce.
func (v *Vault) Save(path string) error {
	v.mu.RLock()
	plain, err := json.Marshal(vaultData{Tokens: v.tokens, Credentials: v.creds})
	if err != nil {
		v.mu.RUnlock()
		return err
	}
	blob, err := seal(v.key, v.salt, v.params, plain)
	v.mu.RUnlock()
	if err != nil {
		return err
	}
	return Save(path, blob)
}

// Rekey re-derives the key from a new passphrase. The caller has to Save.
func (v *Vault) Rekey(passphrase []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	v.salt = salt
	v.params = defaultParams
	v.key = derive(passphrase, salt, defaultParams)
	return nil
}
