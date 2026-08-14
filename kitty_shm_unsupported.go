//go:build darwin && !cgo

package purfecterm

// Shared-memory transmission (t=s) needs shm_open on macOS, which needs cgo.
// A pure-Go build there reports the transfer as unavailable rather than
// silently drawing nothing: the protocol turns this into an ENOENT the client
// can see and fall back from.

import "errors"

// ErrSharedMemoryUnsupported is returned when this build cannot reach POSIX
// shared memory.
var ErrSharedMemoryUnsupported = errors.New(
	"purfecterm: shared-memory image transfer requires cgo on this platform")

func readSharedMemory(string) ([]byte, error) { return nil, ErrSharedMemoryUnsupported }
