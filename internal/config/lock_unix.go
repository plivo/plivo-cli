//go:build unix && !aix

package config

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile blocks until f holds an exclusive lock, which unlockFile, closing f
// or the process exiting releases. A signal can cut the wait short with EINTR.
func lockFile(f *os.File) error {
	for {
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != unix.EINTR {
			return err
		}
	}
}

func unlockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
