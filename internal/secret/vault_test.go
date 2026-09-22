package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVaultRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.enc")

	v, err := NewVault([]byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	v.Set("work", "glpat-work")
	v.Set("personal", "glpat-personal")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}

	back, err := OpenVault(path, []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if back.Token("work") != "glpat-work" || back.Token("personal") != "glpat-personal" {
		t.Fatalf("tokens = %v", back.IDs())
	}
	if back.Token("missing") != "" || back.Has("missing") {
		t.Error("a missing instance should have no token")
	}
}

func TestVaultWrongPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.enc")
	v, _ := NewVault([]byte("right"))
	v.Set("work", "glpat")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(path, []byte("wrong")); err != ErrWrongPassphrase {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
}

// TestVaultSavesWithoutThePassphrase is what lets tokens be added from the
// interface: the derived key stays in memory, the passphrase does not.
func TestVaultSavesWithoutThePassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.enc")
	pass := []byte("hunter2")
	v, _ := NewVault(pass)
	for i := range pass { // the caller wipes it straight after opening
		pass[i] = 0
	}
	v.Set("work", "glpat-work")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	v.Set("second", "glpat-second")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}

	back, err := OpenVault(path, []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if back.Token("second") != "glpat-second" {
		t.Fatalf("second token = %q", back.Token("second"))
	}
}

func TestVaultRemove(t *testing.T) {
	v, _ := NewVault([]byte("p"))
	v.Set("a", "1")
	v.Set("b", "2")
	v.Remove("a")
	v.Set("b", "") // an empty token removes the entry too
	if ids := v.IDs(); len(ids) != 0 {
		t.Fatalf("ids = %v", ids)
	}
}

func TestOpenOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.enc")
	v, isNew, err := OpenOrCreate(path, []byte("p"))
	if err != nil || !isNew {
		t.Fatalf("new = %v, err = %v", isNew, err)
	}
	v.Set("a", "1")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	v, isNew, err = OpenOrCreate(path, []byte("p"))
	if err != nil || isNew {
		t.Fatalf("new = %v, err = %v", isNew, err)
	}
	if v.Token("a") != "1" {
		t.Error("the existing vault was not opened")
	}
}

func TestRekeyChangesThePassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.enc")
	v, _ := NewVault([]byte("old"))
	v.Set("work", "glpat")
	if err := v.Rekey([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(path, []byte("old")); err != ErrWrongPassphrase {
		t.Errorf("the old passphrase still works: %v", err)
	}
	back, err := OpenVault(path, []byte("new"))
	if err != nil || back.Token("work") != "glpat" {
		t.Fatalf("err = %v", err)
	}
}

// TestImportSingleToken folds a pre-vault token.enc in, which is how an
// existing installation keeps working.
func TestImportSingleToken(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "token.enc")
	blob, err := Encrypt([]byte("glpat-old"), []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(legacy, blob); err != nil {
		t.Fatal(err)
	}

	v, _ := NewVault([]byte("hunter2"))
	imported, err := v.ImportSingleToken(legacy, "default", []byte("hunter2"))
	if err != nil || !imported {
		t.Fatalf("imported = %v, err = %v", imported, err)
	}
	if v.Token("default") != "glpat-old" {
		t.Fatalf("token = %q", v.Token("default"))
	}
	// Doing it twice must not overwrite a token set in the meantime.
	v.Set("default", "glpat-new")
	again, err := v.ImportSingleToken(legacy, "default", []byte("hunter2"))
	if err != nil || again {
		t.Fatalf("second import: %v %v", again, err)
	}
	if v.Token("default") != "glpat-new" {
		t.Error("the import overwrote a newer token")
	}
}

func TestImportSingleTokenWithoutTheOldFile(t *testing.T) {
	v, _ := NewVault([]byte("p"))
	imported, err := v.ImportSingleToken(filepath.Join(t.TempDir(), "nope.enc"), "default", []byte("p"))
	if err != nil || imported {
		t.Fatalf("imported = %v, err = %v", imported, err)
	}
}

func TestVaultFileIsNotReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.enc")
	v, _ := NewVault([]byte("p"))
	v.Set("work", "glpat-super-secret")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if index(string(raw), "glpat-super-secret") >= 0 {
		t.Fatal("the token is in the file in the clear")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %v", fi.Mode().Perm())
	}
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
