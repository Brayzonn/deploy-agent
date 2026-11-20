package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	cmdrun "github.com/Brayzonn/deploy-agent/internal/run"
)

// ============================================================
// Signal handling
// ============================================================

/*
interruption records the first stop signal the agent received, so
run() can tell an interrupted deploy from an ordinary failure.
*/
type interruption struct {
	mu  sync.Mutex
	sig os.Signal
}

/* signal returns the first signal received, or nil if none arrived. */
func (in *interruption) signal() os.Signal {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.sig
}

/* signalName prints SIGTERM/SIGINT by name; Go's String() gives "terminated"/"interrupt". */
func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGINT:
		return "SIGINT"
	}
	return sig.String()
}

func (in *interruption) set(sig os.Signal) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.sig = sig
}

/*
handleSignals makes a stop signal end the deploy cleanly instead of
killing it mid-step with no rollback and no alert.

The first SIGTERM or SIGINT cancels the work context: the running
command's process group is killed and the deploy stops at its current
step, but rollback code still runs (see run.EnterRollback). A second
signal also cancels the rollback.

SIGPIPE is ignored: the agent's stdout is a pipe to the webhook
server, and if that server restarts mid-deploy, the next log line
would otherwise kill the agent. With SIGPIPE ignored the write just
fails, and the log file still gets every line.
*/
func handleSignals(log *logger.Logger) *interruption {
	work, cancelWork := context.WithCancel(context.Background())
	rollback, cancelRollback := context.WithCancel(context.Background())
	cmdrun.SetContexts(work, rollback)

	signal.Ignore(syscall.SIGPIPE)

	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)

	in := &interruption{}
	go func() {
		sig := <-ch
		in.set(sig)
		log.Errorf("Received %s — stopping the current step and rolling it back (send it again to stop the rollback too)", signalName(sig))
		cancelWork()

		sig = <-ch
		log.Errorf("Received %s again — stopping the rollback", signalName(sig))
		cancelRollback()
	}()
	return in
}
