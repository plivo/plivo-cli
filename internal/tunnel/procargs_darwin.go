package tunnel

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

var errProcArgs = errors.New("malformed kern.procargs2")

// processArgs returns the command line pid runs, from kern.procargs2: argc,
// the executable path and its NUL padding, then argv.
func processArgs(pid int) ([]string, error) {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	if len(b) < 4 {
		return nil, errProcArgs
	}
	argc := int(binary.NativeEndian.Uint32(b))
	end := bytes.IndexByte(b[4:], 0)
	if end < 0 {
		return nil, errProcArgs
	}
	rest := bytes.TrimLeft(b[4+end:], "\x00")
	var args []string
	for len(args) < argc {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return nil, errProcArgs
		}
		args, rest = append(args, string(rest[:end])), rest[end+1:]
	}
	return args, nil
}
