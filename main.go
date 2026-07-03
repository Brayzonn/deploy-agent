package main

import (
	"fmt"
	"os"

	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/deploy"
	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/notify"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

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
		return 1
	}

	ctx, err := config.ValidateEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Environment validation failed: %v\n", err)
		return 1
	}

	log, err := logger.New(ctx.DeploymentID, cfg.VerboseLogDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		return 1
	}
	defer log.Close()

	log.Infof("=== Deploying %s ===", ctx.RepoName)
	log.Infof("Deployment ID: %s", ctx.DeploymentID)
	log.Infof("Repository: %s", ctx.RepoFullName)
	log.Infof("Branch: %s", ctx.Branch)
	log.Infof("Commit: %s", ctx.Commit[:7])
	log.Infof("Pushed by: %s", ctx.Pusher)
	log.Info("==================================")

	executor := deploy.New(ctx, cfg, log)

	if err := executor.Execute(); err != nil {
		log.State(types.StateFailed)
		log.Errorf("Deployment failed: %v", err)
		notify.NewSlack(cfg.SlackWebhookURL, log).NotifyFailure(ctx, err)
		return 1
	}

	return 0
}
