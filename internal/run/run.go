package run

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

/* Out is Cmd but returns stdout only, for command output that gets parsed. */
func Out(timeout time.Duration, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	return string(out), wrapTimeout(ctx, timeout, name, err)
}

/* Stdin is Cmd with the given reader wired to the command's stdin. */
func Stdin(timeout time.Duration, dir string, stdin io.Reader, name string, args ...string) (string, error) {
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
