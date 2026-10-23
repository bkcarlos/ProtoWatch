package update

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func preservePermissions(old, next string) error {
	info, err := os.Stat(old)
	if err != nil {
		return err
	}
	// Preserve file capabilities used for unprivileged live capture. Refuse an
	// update if this process cannot restore them; never silently disable capture.
	n, err := unix.Getxattr(old, "security.capability", nil)
	if err != nil && !errors.Is(err, unix.ENODATA) && !errors.Is(err, unix.ENOTSUP) {
		return err
	}
	if err = os.Chmod(next, info.Mode().Perm()); err != nil {
		return err
	}
	if n > 0 {
		data := make([]byte, n)
		n, err = unix.Getxattr(old, "security.capability", data)
		if err != nil {
			return err
		}
		if err = unix.Setxattr(next, "security.capability", data[:n], 0); err != nil {
			return fmt.Errorf("cannot preserve CAP_NET_RAW; update using an administrator or systemd AmbientCapabilities: %w", err)
		}
	}
	return nil
}
