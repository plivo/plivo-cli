//go:build unix && !aix

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes an exclusive lock on f unless another open file holds one, and
// reports whether it did. unlockFile, closing f or the process exiting
// releases it.
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }

// noLocks reports whether err says the file system takes no locks: ENOLCK,
// ENOTSUP or ENOSYS.
func noLocks(err error) bool {
	return errors.Is(err, unix.ENOLCK) || errors.Is(err, errors.ErrUnsupported)
}
