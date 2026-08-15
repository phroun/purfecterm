//go:build (darwin && !cgo) || windows

package purfecterm

// Shared-memory transmission (t=s) where it cannot be served: macOS without
// cgo, which needs shm_open, and Windows, which has no POSIX shared memory to
// open at all. Both report the transfer as unavailable rather than silently
// drawing nothing — the protocol turns this into an ENOENT the client can see
// and fall back from.

import "errors"

// ErrSharedMemoryUnsupported is returned when this build cannot reach POSIX
// shared memory.
var ErrSharedMemoryUnsupported = errors.New(
	"purfecterm: shared-memory image transfer requires cgo on this platform")

func readSharedMemory(string) ([]byte, error) { return nil, ErrSharedMemoryUnsupported }
