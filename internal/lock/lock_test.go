package lock

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

// ============================================================
// Helpers
// ============================================================

/* tryFlock reports whether a non-blocking exclusive flock on path succeeds right now. */
func tryFlock(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return true
	}
	return false
}

// ============================================================
// Tests
// ============================================================

/*
TestAcquireSerializes checks that a held lock blocks a second
acquirer until released, that different repos don't contend, and
that Release lets a queued acquirer proceed.
*/
func TestAcquireSerializes(t *testing.T) {
	stateDir := t.TempDir()
	log, err := logger.New("test", filepath.Join(stateDir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	first, err := Acquire(stateDir, "myrepo", log)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	lockPath := filepath.Join(stateDir, "deploy-myrepo.lock")
	if tryFlock(t, lockPath) {
		t.Fatal("lock is not held: second flock succeeded while first is active")
	}

	otherPath := filepath.Join(stateDir, "deploy-otherrepo.lock")
	if !tryFlock(t, otherPath) {
		t.Fatal("different repo contended on the lock")
	}

	acquired := make(chan *DeployLock)
	go func() {
		second, err := Acquire(stateDir, "myrepo", log)
		if err != nil {
			t.Errorf("queued acquire failed: %v", err)
		}
		acquired <- second
	}()

	select {
	case <-acquired:
		t.Fatal("queued acquire returned while lock was still held")
	case <-time.After(100 * time.Millisecond):
	}

	first.Release()

	select {
	case second := <-acquired:
		second.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("queued acquire did not proceed after release")
	}

	if !tryFlock(t, lockPath) {
		t.Fatal("lock still held after all releases")
	}
}
