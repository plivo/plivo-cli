//go:build unix && !aix

package config

import (
	"testing"

	"golang.org/x/sys/unix"
)

// NFS without its lock service answers ENOLCK, and a held lock EWOULDBLOCK.
func TestNoLocks_readsENOLCKAsNoLocks(t *testing.T) {
	if !noLocks(unix.ENOLCK) || noLocks(unix.EWOULDBLOCK) {
		t.Error("want ENOLCK read as a file system without locks, and EWOULDBLOCK as a held lock")
	}
}
