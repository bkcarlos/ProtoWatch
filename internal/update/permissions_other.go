//go:build !linux

package update

import "os"

func preservePermissions(old, next string) error {
	info, err := os.Stat(old)
	if err != nil {
		return err
	}
	return os.Chmod(next, info.Mode().Perm())
}
