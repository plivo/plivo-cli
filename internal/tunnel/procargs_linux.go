package tunnel

import (
	"os"
	"strconv"
	"strings"
)

// processArgs returns the command line pid runs.
func processArgs(pid int) ([]string, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00"), nil
}
