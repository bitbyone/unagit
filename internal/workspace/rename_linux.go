package workspace

import "golang.org/x/sys/unix"

// Installing a moved repository must never replace a concurrently created path.
func renameUnused(source, target string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
}
