//go:build unix

package history

import (
	"errors"
	"os"
	"syscall"
)

// fcntlTryLock attempts a non-blocking exclusive fcntl lock on all of f.
// busy is true only if another process holds a conflicting lock; any other
// error is returned for information and should be ignored.
func fcntlTryLock(f *os.File) (busy bool, err error) {
	fl := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	for {
		err = syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &fl)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		break
	}
	if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EACCES) {
		return true, err
	}
	return false, err
}
