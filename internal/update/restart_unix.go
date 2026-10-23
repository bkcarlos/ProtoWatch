//go:build linux || darwin

package update

import (
	"os"
	"syscall"
)

// Restart preserves the PID and the supervisor's process identity.
func restartProcess(executable string) error {
	return syscall.Exec(executable, os.Args, os.Environ())
}
