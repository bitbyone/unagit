//go:build !darwin

package keychain

// Available reports whether secrets can be kept in a keychain here. Outside
// macOS there is no keychain that limits an item to the program that stored
// it - the Secret Service lets any program of the user's read it - so none is
// used.
func Available() bool { return false }

func Get(service, account string) ([]byte, error)      { return nil, ErrUnsupported }
func Set(service, account string, secret []byte) error { return ErrUnsupported }
func Delete(service, account string) error             { return nil }
