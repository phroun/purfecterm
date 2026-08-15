//go:build darwin && cgo

package purfecterm

// Shared-memory transmission (t=s) on macOS.
//
// Unlike Linux, macOS does not surface POSIX shared memory objects in the
// filesystem — there is no /dev/shm, and an object created with shm_open is
// reachable only through shm_open. Reading the name as a path, which is all the
// Linux implementation needs to do, fails with ENOENT there and takes every
// shared-memory image down with it.
//
// The work is done in a C shim rather than call by call from Go, for two
// reasons cgo forces: shm_open is declared variadic and cgo cannot call a
// variadic function, and MAP_FAILED is a cast constant cgo cannot express.
// Both are ordinary C on the other side of this boundary.

/*
#include <errno.h>
#include <fcntl.h>
#include <string.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>

// purfecterm_shm_read maps a POSIX shared memory object, copies it to a
// malloc'd buffer the caller frees, and unlinks the object — the protocol has
// the terminal consume it. Returns NULL on any failure, reporting WHICH step
// failed in *stage and its errno in *err, so a diagnostic can say why rather
// than flattening every cause into "not found".
//   stage 1 shm_open   2 fstat/size   3 mmap   4 malloc
static void *purfecterm_shm_read(const char *name, size_t max, size_t *out_size,
                                 int *stage, int *err) {
    *stage = 0;
    *err = 0;
    int fd = shm_open(name, O_RDONLY);
    if (fd < 0) {
        *stage = 1;
        *err = errno;
        return NULL;
    }
    struct stat st;
    if (fstat(fd, &st) != 0 || st.st_size <= 0 || (size_t)st.st_size > max) {
        *stage = 2;
        *err = errno;
        close(fd);
        return NULL;
    }
    size_t n = (size_t)st.st_size;
    void *addr = mmap(NULL, n, PROT_READ, MAP_SHARED, fd, 0);
    close(fd);
    if (addr == MAP_FAILED) {
        *stage = 3;
        *err = errno;
        return NULL;
    }
    void *buf = malloc(n);
    if (buf != NULL) {
        memcpy(buf, addr, n);
    }
    munmap(addr, n);
    if (buf == NULL) {
        *stage = 4;
        return NULL;
    }
    *out_size = n;
    shm_unlink(name);
    return buf;
}
*/
import "C"

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// readSharedMemory opens a POSIX shared memory object by name, copies its
// contents, and unlinks it.
func readSharedMemory(name string) ([]byte, error) {
	clean := "/" + strings.TrimPrefix(strings.TrimSpace(name), "/")
	if clean == "/" || strings.Contains(clean[1:], "/") {
		// A POSIX shm name is a single leading-slash component; anything else
		// is a path trying to escape, and is refused rather than opened.
		return nil, os.ErrInvalid
	}

	cname := C.CString(clean)
	defer C.free(unsafe.Pointer(cname))

	var size C.size_t
	var stage, errno C.int
	buf := C.purfecterm_shm_read(cname, C.size_t(MaxKittyPayloadBytes), &size, &stage, &errno)
	if buf == nil {
		step := map[C.int]string{1: "shm_open", 2: "fstat/size", 3: "mmap", 4: "malloc"}[stage]
		if step == "" {
			step = "unknown step"
		}
		return nil, fmt.Errorf("purfecterm: shared memory object %s: %s: %w",
			clean, step, syscall.Errno(errno))
	}
	defer C.free(buf)
	return C.GoBytes(buf, C.int(size)), nil
}
