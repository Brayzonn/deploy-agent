package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

type DeployLock struct {
	file *os.File
	log  *logger.Logger
}

// ============================================================
// Acquisition
// ============================================================

/*
Acquire takes an exclusive flock on <stateDir>/deploy-<repo>.lock so
only one deployment per repo runs at a time. If another deployment
holds the lock, it logs and blocks until that one finishes, so rapid
successive pushes deploy serially with the last one winning. The
kernel releases the lock when the process exits, so a crashed deploy
never leaves a stale lock behind.
*/
func Acquire(stateDir, repoName string, log *logger.Logger) (*DeployLock, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	path := filepath.Join(stateDir, fmt.Sprintf("deploy-%s.lock", repoName))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open deploy lock file: %w", err)
	}

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK {
		log.Warningf("Another deployment for %s is in progress — waiting for it to finish...", repoName)
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
	}
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to acquire deploy lock: %w", err)
	}

	file.Truncate(0)
	fmt.Fprintf(file, "%d\n", os.Getpid())

	return &DeployLock{file: file, log: log}, nil
}

// ============================================================
// Release
// ============================================================

/* Release unlocks and closes the lock file, letting any queued deployment proceed. */
func (l *DeployLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		l.log.Warningf("Failed to release deploy lock: %v", err)
	}
	l.file.Close()
	l.file = nil
}
