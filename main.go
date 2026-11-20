package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/deploy"
	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/notify"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

/*
Exit codes are a contract with the webhook server, which alerts on
anything that is not 0 or 1:

	0  deployed, or skipped on purpose
	1  deploy failed — the agent already sent the Slack alert
	3  bootstrap failed before a deploy could start (config, env,
	   directories, logger) — no Slack alert from the agent; the error
	   is on stderr for the webhook server to forward

Go itself exits 2 on a panic, which the webhook server also alerts on.
*/
const (
	exitOK            = 0
	exitDeployFailed  = 1
	exitBootstrapFail = 3
)

// logMaxAge is how long deployment logs are kept before cleanup.
const logMaxAge = 30 * 24 * time.Hour

func main() {
	os.Exit(run())
}

/*
run holds the real main logic and returns the process exit code, so
deferred calls (log file close) still execute on the failure path —
os.Exit in main would skip them.
*/
func run() int {
	cfg := config.LoadConfig()

	if err := cfg.EnsureDirectories(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create directories: %v\n", err)
		return exitBootstrapFail
	}

	ctx, err := config.ValidateEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Environment validation failed: %v\n", err)
		return exitBootstrapFail
	}

	log, err := logger.New(ctx.DeploymentID, cfg.VerboseLogDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		return exitBootstrapFail
	}
	defer log.Close()

	interrupted := handleSignals(log)

	if n := logger.PruneOld(cfg.VerboseLogDir, logMaxAge); n > 0 {
		log.Infof("Deleted %d deployment log(s) older than 30 days", n)
	}

	log.Infof("=== Deploying %s ===", ctx.RepoName)
	log.Infof("Deployment ID: %s", ctx.DeploymentID)
	log.Infof("Repository: %s", ctx.RepoFullName)
	log.Infof("Branch: %s", ctx.Branch)
	log.Infof("Commit: %s", ctx.Commit[:7])
	log.Infof("Pushed by: %s", ctx.Pusher)
	log.Info("==================================")

	executor := deploy.New(ctx, cfg, log)

	slack := notify.NewSlack(cfg.SlackWebhookURL, log)
	slack.NotifyStart(ctx)

	err = executor.Execute()

	if actions := executor.ManualActions(); len(actions) > 0 {
		for _, a := range actions {
			log.Warningf("Needs attention: %s", a)
		}
		slack.NotifyWarning(ctx, actions)
	}

	if sig := interrupted.signal(); sig != nil {
		if err == nil {
			log.Warningf("Received %s, but the deploy had already finished", signalName(sig))
			return exitOK
		}
		err = fmt.Errorf("deploy interrupted by %s part-way through; the interrupted step was rolled back unless a rollback failure is listed below — redeploy when ready: %w", signalName(sig), err)
	}

	if err != nil {
		log.State(types.StateFailed)
		log.Errorf("Deployment failed: %v", err)
		slack.NotifyFailure(ctx, err)
		return exitDeployFailed
	}

	return exitOK
}
