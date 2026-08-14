//go:build !darwin

package purfecterm

// Shared-memory transmission (t=s) on the platforms where a POSIX shared memory
// object is reachable through the filesystem: Linux and the BSDs surface them
// under /dev/shm, so the object name maps to a path and an ordinary read works.
// macOS does not, and has its own file — see kitty_shm_darwin.go.

import (
	"os"
	"strings"
)

// readSharedMemory opens a POSIX shared memory object by name and returns its
// contents. The protocol has the terminal unlink the object after reading.
func readSharedMemory(name string) ([]byte, error) {
	clean := "/" + strings.TrimPrefix(strings.TrimSpace(name), "/")
	if strings.Contains(clean[1:], "/") {
		// A POSIX shm name is a single component; anything else is a path
		// trying to escape, and is refused rather than opened.
		return nil, os.ErrInvalid
	}
	path := "/dev/shm" + clean
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_ = os.Remove(path) // the terminal consumes the object
	return data, nil
}
