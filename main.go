package main

import (
	"fmt"
	"os"

	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/deploy"
	"github.com/Brayzonn/deploy-agent/internal/logger"
)

func main() {
	cfg := config.LoadConfig()

	if err := cfg.EnsureDirectories(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create directories: %v\n", err)
		os.Exit(1)
	}

	ctx, err := config.ValidateEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Environment validation failed: %v\n", err)
		os.Exit(1)
	}

	log, err := logger.New(ctx.DeploymentID, cfg.VerboseLogDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
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
		log.Errorf("Deployment failed: %v", err)
		os.Exit(1)
	}

	log.Success("Deployment completed successfully!")
}