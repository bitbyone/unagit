// Package keychain keeps one secret in the macOS login keychain, where only
// the program that stored it may read it without asking.
//
// It exists for one thing: remembering the vault passphrase, when the user
// has chosen that, so unagit can open without the dialog. The item is created
// by the unagit binary through the keychain API, and the keychain trusts the
// application that creates an item - and nothing else. Any other program,
// the security command line included, gets a macOS dialog the user has to
// answer. A rebuilt unagit is another program to the keychain too, so it
// asks once, and "Always Allow" trusts the new binary.
//
// Elsewhere there is no keychain with that guarantee, and Available says so.
package keychain

import "errors"

// ErrNotFound means nothing is stored under the name.
var ErrNotFound = errors.New("nothing stored in the keychain")

// ErrUnsupported means this system has no keychain unagit will use.
var ErrUnsupported = errors.New("the keychain is only used on macOS")
