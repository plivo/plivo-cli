//go:build !linux && !darwin

package tunnel

import "errors"

// processArgs cannot read another process's command line here, so
// StopLeftover leaves every pid alone.
func processArgs(int) ([]string, error) {
	return nil, errors.ErrUnsupported
}
