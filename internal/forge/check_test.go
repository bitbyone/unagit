// Package forge_test pins the contract: everything unagit can talk to has to
// answer the same questions.
package forge_test

import (
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/github"
	"github.com/tobola/unagit/internal/gitlab"
)

// Both providers satisfy the interface, checked at compile time.
var (
	_ forge.Provider = (*gitlab.Client)(nil)
	_ forge.Provider = (*github.Client)(nil)
)
