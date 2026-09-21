package secret

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	token := []byte("glpat-not-a-real-token")
	blob, err := Encrypt(token, []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(blob, []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(token) {
		t.Fatalf("got %q, want %q", got, token)
	}
}

func TestWrongPassphrase(t *testing.T) {
	blob, err := Encrypt([]byte("secret"), []byte("right"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(blob, []byte("wrong")); err != ErrWrongPassphrase {
		t.Fatalf("got %v, want ErrWrongPassphrase", err)
	}
}

func TestCiphertextDiffersPerEncryption(t *testing.T) {
	a, _ := Encrypt([]byte("secret"), []byte("pass"))
	b, _ := Encrypt([]byte("secret"), []byte("pass"))
	if a.Data == b.Data || a.Salt == b.Salt || a.Nonce == b.Nonce {
		t.Fatal("encryption is not randomised")
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.enc")
	blob, _ := Encrypt([]byte("secret"), []byte("pass"))
	if err := Save(path, blob); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(loaded, []byte("pass"))
	if err != nil || string(got) != "secret" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTamperedBlobFails(t *testing.T) {
	blob, _ := Encrypt([]byte("secret"), []byte("pass"))
	blob.Data = "AAAA" + blob.Data[4:]
	if _, err := Decrypt(blob, []byte("pass")); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
}
