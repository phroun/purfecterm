package purfecterm

// Shared-memory transmission (t=s) reads a POSIX shared memory object by name.
// On Linux and the BSDs those objects appear under /dev/shm, so the name maps
// to a path; a platform without that mapping falls back to failing the read,
// which the protocol reports as ENOENT.

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
