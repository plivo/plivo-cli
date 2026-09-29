//go:build unix

package cmd

import (
	"os"

	"golang.org/x/sys/unix"
)

// inForeground reports whether reading terminal f won't stop this process
// with SIGTTIN, as it would for `plivo login &` or `timeout 300 plivo login`.
// The ioctl fails when f isn't our controlling terminal, and job control
// never stops a read from any other terminal.
func inForeground(f *os.File) bool {
	fg, err := unix.IoctlGetInt(int(f.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return true
	}
	own, err := unix.Getpgid(0)
	return err != nil || fg == own
}
