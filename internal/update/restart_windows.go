package update

import (
	"os"
	"os/exec"
)

func restartProcess(executable string) error {
	cmd := exec.Command(executable, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
