package tunnel

import (
	"os"
	"slices"
)

// StopLeftover stops the tunnel process a forward recorded (Tunnel.PID and
// Args) and could not Close, because it was killed. pid is stopped only while
// it still runs that command line, as the system may have given the number to
// another program since; where another process's command line cannot be read,
// nothing is stopped. It reports whether it stopped one.
func StopLeftover(pid int, args []string) bool {
	if pid <= 0 || pid == os.Getpid() || len(args) == 0 {
		return false
	}
	if running, err := processArgs(pid); err != nil || !slices.Equal(running, args) {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p.Kill() == nil
}
