package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ============================================================
// Timeouts
// ============================================================

/*
TestTimeoutKillsGrandchildren checks a timeout kills the whole
process group: sh's child sleep would otherwise hold the output pipe
open and make Cmd wait the full 6 seconds instead of about 1.
*/
func TestTimeoutKillsGrandchildren(t *testing.T) {
	start := time.Now()
	_, err := Cmd(1*time.Second, "", "sh", "-c", "sleep 6; echo done")
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("Cmd took %v; the grandchild was not killed with the group", elapsed)
	}
}

// ============================================================
// Normal execution
// ============================================================

/* TestCmdReturnsOutput checks a quick command still returns its output with no error. */
func TestCmdReturnsOutput(t *testing.T) {
	out, err := Cmd(5*time.Second, "", "sh", "-c", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("expected output %q, got %q", "hello", out)
	}
}

// ============================================================
// Interruption
// ============================================================

/* useContexts installs work/rollback contexts for one test and restores the defaults after. */
func useContexts(t *testing.T, work, rollback context.Context) {
	t.Helper()
	SetContexts(work, rollback)
	t.Cleanup(func() { SetContexts(context.Background(), context.Background()) })
}

/*
TestCancelledWorkStopsCommands checks that once the work context is
cancelled (first signal), a running command is killed at once and
reported as interrupted, not as a timeout.
*/
func TestCancelledWorkStopsCommands(t *testing.T) {
	work, cancel := context.WithCancel(context.Background())
	useContexts(t, work, context.Background())

	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := Cmd(10*time.Second, "", "sh", "-c", "sleep 6; echo done")

	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("expected ErrInterrupted, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("command took %v after cancel; it should stop at once", elapsed)
	}
	if !Stopping() {
		t.Error("Stopping() = false after the work context was cancelled")
	}
}

/*
TestRollbackRunsAfterInterrupt checks that commands inside
EnterRollback still run after the first signal, and that leaving
rollback mode returns to the cancelled work context.
*/
func TestRollbackRunsAfterInterrupt(t *testing.T) {
	work, cancel := context.WithCancel(context.Background())
	cancel()
	useContexts(t, work, context.Background())

	leave := EnterRollback()
	if Stopping() {
		t.Error("Stopping() = true inside rollback mode with a live rollback context")
	}
	if out, err := Cmd(5*time.Second, "", "sh", "-c", "echo rolled back"); err != nil || strings.TrimSpace(out) != "rolled back" {
		t.Fatalf("rollback command = %q, %v; want it to run", out, err)
	}
	leave()
	leave()

	if _, err := Cmd(5*time.Second, "", "sh", "-c", "echo hi"); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("after leaving rollback mode, expected ErrInterrupted, got %v", err)
	}
}

/* TestEnterRollbackNests checks a nested rollback helper keeps rollback mode until the outer one leaves. */
func TestEnterRollbackNests(t *testing.T) {
	work, cancel := context.WithCancel(context.Background())
	cancel()
	useContexts(t, work, context.Background())

	outer := EnterRollback()
	inner := EnterRollback()
	inner()
	if Stopping() {
		t.Error("left rollback mode when only the inner helper returned")
	}
	outer()
	if !Stopping() {
		t.Error("still in rollback mode after the outer helper returned")
	}
}
