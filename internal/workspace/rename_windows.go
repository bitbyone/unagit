package workspace

import "golang.org/x/sys/windows"

// Installing a moved repository must never replace a concurrently created
// path: MoveFileEx without MOVEFILE_REPLACE_EXISTING refuses an existing
// target, as RENAME_NOREPLACE does on Linux.
func renameUnused(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, 0)
}
