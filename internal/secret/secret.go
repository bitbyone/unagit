// Package secret stores the GitLab token encrypted at rest.
//
// The token is sealed with AES-256-GCM using a key derived from a passphrase
// via Argon2id. The passphrase is asked for on every start and the plaintext
// token only ever lives in process memory - it is never written to disk, never
// passed as a command line argument and never logged.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

const (
	kdfArgon2id = "argon2id"
	saltLen     = 16
	keyLen      = 32
)

// params are the Argon2id cost parameters used for new blobs.
var defaultParams = params{Time: 3, Memory: 64 * 1024, Threads: 4}

type params struct {
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

// Blob is the serialised, encrypted token.
type Blob struct {
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	Params  params `json:"params"`
	Salt    string `json:"salt"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

// ErrWrongPassphrase is returned when decryption fails, which in practice
// always means the passphrase was wrong (or the file was tampered with).
var ErrWrongPassphrase = errors.New("wrong passphrase")

// Encrypt seals plaintext with a key derived from passphrase.
func Encrypt(plaintext, passphrase []byte) (*Blob, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := derive(passphrase, salt, defaultParams)
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return &Blob{
		Version: 1,
		KDF:     kdfArgon2id,
		Params:  defaultParams,
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Nonce:   base64.StdEncoding.EncodeToString(nonce),
		Data:    base64.StdEncoding.EncodeToString(ct),
	}, nil
}

// Decrypt opens the blob with the given passphrase.
func Decrypt(b *Blob, passphrase []byte) ([]byte, error) {
	if b.KDF != kdfArgon2id {
		return nil, fmt.Errorf("unsupported kdf %q", b.KDF)
	}
	salt, err := base64.StdEncoding.DecodeString(b.Salt)
	if err != nil {
		return nil, fmt.Errorf("corrupt salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(b.Nonce)
	if err != nil {
		return nil, fmt.Errorf("corrupt nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(b.Data)
	if err != nil {
		return nil, fmt.Errorf("corrupt data: %w", err)
	}
	gcm, err := newGCM(derive(passphrase, salt, b.Params))
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return pt, nil
}

func derive(passphrase, salt []byte, p params) []byte {
	return argon2.IDKey(passphrase, salt, p.Time, p.Memory, p.Threads, keyLen)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Save writes the blob to path with 0600 permissions.
func Save(path string, b *Blob) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Load reads a blob from path.
func Load(path string) (*Blob, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Blob
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &b, nil
}
