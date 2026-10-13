package workspace

import "golang.org/x/sys/unix"

// Installing a moved repository must never replace a concurrently created path.
func renameUnused(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
