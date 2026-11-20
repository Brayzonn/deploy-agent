package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// waitDelay caps how long a cancelled command may keep its output
// pipes open (a surviving grandchild, or a root process under sudo
// that cannot be killed) before the pipes are closed and the call returns.
const waitDelay = 10 * time.Second

// ============================================================
// Test stubbing
// ============================================================

/*
stub, when set, intercepts every command instead of executing it.
It exists so packages built on run can unit-test their command
sequencing and output parsing without real git/docker/pm2 binaries
on the machine; production code never sets it.
*/
var stub func(dir, name string, args ...string) (string, error)

/*
Stub installs f as the command interceptor and returns a restore
func for the test to defer. Not safe for parallel tests — the stub
is process-global, like the commands it replaces.
*/
func Stub(f func(dir, name string, args ...string) (string, error)) (restore func()) {
	stub = f
	return func() { stub = nil }
}

// ============================================================
// Interruption
// ============================================================

/*
Every command runs under one of two parent contexts, which main
cancels when a signal arrives:

  - work: cancelled on the first SIGTERM/SIGINT. The running command's
    process group is killed (see killGroupOnCancel) and every later
    command fails at once, so the deploy stops at the current step.
  - rollback: cancelled only on a second signal. Code between
    EnterRollback and its leave func runs under this one, so the
    rollback for the interrupted step can still run.

Both default to never-cancelled, which is what tests and any caller
that never calls SetContexts get.
*/
var (
	ctxMu         sync.Mutex
	workCtx       = context.Background()
	rollbackCtx   = context.Background()
	rollbackDepth int
)

/* ErrInterrupted marks a command that was stopped because the deploy was interrupted by a signal. */
var ErrInterrupted = errors.New("deploy interrupted by signal")

/* SetContexts installs the work and rollback parent contexts; main calls it once at startup. */
func SetContexts(work, rollback context.Context) {
	ctxMu.Lock()
	defer ctxMu.Unlock()
	workCtx, rollbackCtx = work, rollback
}

/*
EnterRollback makes commands started from now on use the rollback
context, until the returned leave func is called. Calls nest, so a
rollback helper that calls another one stays in rollback mode.
Usage: defer run.EnterRollback()()
*/
func EnterRollback() (leave func()) {
	ctxMu.Lock()
	rollbackDepth++
	ctxMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			ctxMu.Lock()
			rollbackDepth--
			ctxMu.Unlock()
		})
	}
}

/*
Stopping reports whether the context that new commands would run
under is cancelled. Wait loops (health checks, container readiness)
check it so they end at once instead of polling until their timeout.
*/
func Stopping() bool {
	return parentContext().Err() != nil
}

/* parentContext returns the context new commands run under right now. */
func parentContext() context.Context {
	ctxMu.Lock()
	defer ctxMu.Unlock()
	if rollbackDepth > 0 {
		return rollbackCtx
	}
	return workCtx
}

// ============================================================
// Command execution
// ============================================================

/*
Cmd runs name with args in dir, killing the process once timeout
passes so a hung command (npm, docker, sudo prompt) can never block
a deployment forever. Returns combined stdout+stderr — what error
messages want.
*/
func Cmd(timeout time.Duration, dir, name string, args ...string) (string, error) {
	if stub != nil {
		return stub(dir, name, args...)
	}

	ctx, cancel := context.WithTimeout(parentContext(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	killGroupOnCancel(cmd)

	out, err := cmd.CombinedOutput()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

/* Out is Cmd but returns stdout only, for command output that gets parsed. */
func Out(timeout time.Duration, dir, name string, args ...string) (string, error) {
	if stub != nil {
		return stub(dir, name, args...)
	}

	ctx, cancel := context.WithTimeout(parentContext(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	killGroupOnCancel(cmd)

	out, err := cmd.Output()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

/* Stdin is Cmd with the given reader wired to the command's stdin. */
func Stdin(timeout time.Duration, dir string, stdin io.Reader, name string, args ...string) (string, error) {
	if stub != nil {
		return stub(dir, name, args...)
	}

	ctx, cancel := context.WithTimeout(parentContext(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	killGroupOnCancel(cmd)
	cmd.Stdin = stdin

	out, err := cmd.CombinedOutput()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

// ============================================================
// Helpers
// ============================================================

/*
killGroupOnCancel runs the command in its own process group and, on
timeout, kills the whole group instead of just the direct child, so
grandchildren (node under npm, builds under docker compose) die too
and stop holding the output pipe open. WaitDelay is the backstop for
anything that survives the kill, such as root processes under sudo.
*/
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay
}

/*
wrapTimeout turns a deadline-exceeded kill into an explicit timeout
error, and a signal cancellation into one that wraps ErrInterrupted.
*/
func wrapTimeout(ctx context.Context, timeout time.Duration, name string, err error) error {
	if err == nil {
		return nil
	}
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return fmt.Errorf("%s timed out after %s", name, timeout)
	case context.Canceled:
		return fmt.Errorf("%s stopped: %w", name, ErrInterrupted)
	}
	return err
}
