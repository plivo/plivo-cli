//go:build !unix

package cmd

import "os"

// inForeground is always true without Unix job control: a terminal read
// can't stop the process.
func inForeground(*os.File) bool { return true }
