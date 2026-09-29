package keychain

import (
	"bytes"
	"os"
	"testing"
)

// TestRoundTrip uses the real login keychain, so it only runs when asked to:
// UNAGIT_KEYCHAIN_TEST=1 go test ./internal/keychain. It stores, reads,
// replaces and deletes an item of its own, and leaves nothing behind.
func TestRoundTrip(t *testing.T) {
	if os.Getenv("UNAGIT_KEYCHAIN_TEST") == "" || !Available() {
		t.Skip("set UNAGIT_KEYCHAIN_TEST=1 to use the real keychain")
	}
	const service, account = "unagit test", "round trip"
	t.Cleanup(func() { _ = Delete(service, account) })
	if _, err := Get(service, account); err != ErrNotFound {
		t.Fatalf("before storing: %v, want ErrNotFound", err)
	}
	for _, secret := range [][]byte{[]byte("first"), []byte("second, longer")} {
		if err := Set(service, account, secret); err != nil {
			t.Fatal(err)
		}
		got, err := Get(service, account)
		if err != nil || !bytes.Equal(got, secret) {
			t.Fatalf("read back %q, %v; want %q", got, err, secret)
		}
	}
	if err := Delete(service, account); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(service, account); err != ErrNotFound {
		t.Fatalf("after deleting: %v, want ErrNotFound", err)
	}
	if err := Delete(service, account); err != nil {
		t.Errorf("deleting nothing: %v", err)
	}
}
