//go:build aix || !(unix || windows)

package config

import "os"

// No file lock on the platforms the CLI does not ship for: two changes made at
// once there can still lose one.
func lockFile(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
