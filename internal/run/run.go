package run

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

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

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

/* Out is Cmd but returns stdout only, for command output that gets parsed. */
func Out(timeout time.Duration, dir, name string, args ...string) (string, error) {
	if stub != nil {
		return stub(dir, name, args...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

/* Stdin is Cmd with the given reader wired to the command's stdin. */
func Stdin(timeout time.Duration, dir string, stdin io.Reader, name string, args ...string) (string, error) {
	if stub != nil {
		return stub(dir, name, args...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin

	out, err := cmd.CombinedOutput()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

// ============================================================
// Helpers
// ============================================================

/* wrapTimeout turns a deadline-exceeded kill into an explicit timeout error. */
func wrapTimeout(ctx context.Context, timeout time.Duration, name string, err error) error {
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return err
}
