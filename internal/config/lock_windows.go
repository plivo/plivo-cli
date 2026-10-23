package config

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile blocks until f holds an exclusive lock over its whole range, which
// unlockFile, closing f or the process exiting releases.
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
}
