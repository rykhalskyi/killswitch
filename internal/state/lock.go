package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var (
	// ErrLocked indicates that another killswitch process is currently holding the lock.
	ErrLocked = errors.New("killswitch is locked by another running process")
)

// Lock represents an exclusive file lock held on the state directory.
type Lock struct {
	file *os.File
	path string
}

// AcquireLock attempts to acquire an exclusive, non-blocking lock on <stateDir>/lock.
// Returns ErrLocked if another process holds the lock.
func AcquireLock(stateDir string) (*Lock, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("creating state directory %s: %w", stateDir, err)
	}

	lockPath := filepath.Join(stateDir, "lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file %s: %w", lockPath, err)
	}

	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("acquiring flock on %s: %w", lockPath, err)
	}

	return &Lock{
		file: f,
		path: lockPath,
	}, nil
}

// Release releases the lock and closes the lock file.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer func() {
		l.file.Close()
		l.file = nil
	}()

	return unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
}
