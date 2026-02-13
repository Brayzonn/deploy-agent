package deploy

import (
	"fmt"
	"path/filepath"

	"github.com/Brayzonn/deploy-agent/internal/build"
	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/git"
	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type Executor struct {
	ctx    *types.DeploymentContext
	cfg    *config.Config
	log    *logger.Logger
	git    *git.GitManager
	stashed bool
}

func New(ctx *types.DeploymentContext, cfg *config.Config, log *logger.Logger) *Executor {
	gitManager := git.New(ctx.Config.RepoDir, ctx.Branch, log)
	
	return &Executor{
		ctx:     ctx,
		cfg:     cfg,
		log:     log,
		git:     gitManager,
		stashed: false,
	}
}

func (e *Executor) Execute() error {
	e.log.State(types.StateStarting)
	e.log.Infof("Starting deployment for %s", e.ctx.RepoName)
	e.log.Infof("Branch: %s | Type: %s | Fullstack: %t", e.ctx.Branch, e.ctx.Config.ProjectType, e.ctx.Config.FullStack)

	// Validate git repository
	if err := e.git.Validate(); err != nil {
		return fmt.Errorf("git validation failed: %w", err)
	}

	// Handle uncommitted changes
	if err := e.handleUncommittedChanges(); err != nil {
		return err
	}

	// Fetch and check for updates
	e.log.State(types.StateFetching)
	if err := e.git.Fetch(); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	hasUpdates, err := e.git.CheckForUpdates()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if !hasUpdates {
		e.log.Success("No changes to deploy. Your site is up to date!")
		if e.stashed {
			e.git.PopStash()
		}
		return nil
	}

	// Pull latest changes
	e.log.State(types.StatePulling)
	if err := e.git.Pull(); err != nil {
		if e.stashed {
			e.log.Warning("Pull failed. Attempting to restore stash and retry...")
			e.git.PopStash()
			e.stashed = false
		}
		return fmt.Errorf("git pull failed: %w", err)
	}

	// Restore stashed changes
	if e.stashed {
		e.git.PopStash()
	}

	if err := e.deploy(); err != nil {
		return err
	}

	e.log.State(types.StateSuccess)
	e.log.Success("Deployment completed successfully!")
	e.log.Successf("Deployed %s (%s) to %s branch", e.ctx.RepoName, e.ctx.Commit[:7], e.ctx.Branch)

	return nil
}

// stash any uncommitted changes
func (e *Executor) handleUncommittedChanges() error {
	hasChanges, err := e.git.HasUncommittedChanges()
	if err != nil {
		return fmt.Errorf("failed to check for uncommitted changes: %w", err)
	}

	if hasChanges {
		stashName := fmt.Sprintf("deployment-auto-stash-%s", e.ctx.DeploymentID)
		if err := e.git.StashChanges(stashName); err != nil {
			return fmt.Errorf("failed to stash changes: %w", err)
		}
		e.stashed = true
	}

	return nil
}

//  handle the actual deployment based on project type
func (e *Executor) deploy() error {
	if e.ctx.Config.FullStack {
		return e.deployFullstack()
	}

	if e.ctx.Config.ProjectType == types.ProjectTypeClient {
		return e.deployClient()
	}

	return e.deployServer()
}

//  deploy a frontend-only project
func (e *Executor) deployClient() error {
	e.log.State(types.StateDeployingClient)
	e.log.Info("Deploying client application...")

	// Determine client directory
	clientDir := e.ctx.Config.RepoDir
	if e.ctx.Config.ClientDir != "" && e.ctx.Config.ClientDir != "." {
		clientDir = filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ClientDir)
	}

	e.log.Infof("Client directory: %s", clientDir)

	// Build client
	clientBuilder := build.NewClientBuilder(clientDir, e.ctx.Config.WebRoot, e.log)
	buildResult, err := clientBuilder.Build()
	if err != nil {
		return fmt.Errorf("client build failed: %w", err)
	}

	// Deploy to web root
	if err := clientBuilder.Deploy(buildResult.OutputDir); err != nil {
		return fmt.Errorf("client deployment failed: %w", err)
	}

	// Restart Nginx
	clientBuilder.RestartNginx()

	return nil
}

// deployServer deploys a backend-only project
func (e *Executor) deployServer() error {
	e.log.State(types.StateDeployingServer)
	e.log.Info("Deploying server API...")

	// Determine server directory
	serverDir := filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ServerDir)
	e.log.Infof("Server directory: %s", serverDir)

	// Build server
	serverBuilder := build.NewServerBuilder(
		serverDir,
		e.ctx.Config.ProjectType,
		e.ctx.RepoName,
		e.ctx.Config.ServerEntry,
		e.ctx.Config.PM2Ecosystem,
		e.log,
	)

	buildResult, err := serverBuilder.Build()
	if err != nil {
		return fmt.Errorf("server build failed: %w", err)
	}

	// Deploy with PM2
	if err := serverBuilder.Deploy(serverDir); err != nil {
		return fmt.Errorf("server deployment failed: %w", err)
	}

	e.log.Infof("Server build completed in %v", buildResult.Duration)
	return nil
}

// deployFullstack deploys both frontend and backend
func (e *Executor) deployFullstack() error {
	e.log.State(types.StateDeployingFull)
	e.log.Info("Deploying fullstack application...")

	// Deploy server
	e.log.Info("Step 1/2: Deploying server...")
	serverDir := filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ServerDir)
	
	serverBuilder := build.NewServerBuilder(
		serverDir,
		e.ctx.Config.ProjectType,
		e.ctx.RepoName,
		e.ctx.Config.ServerEntry,
		e.ctx.Config.PM2Ecosystem,
		e.log,
	)

	serverBuildResult, err := serverBuilder.Build()
	if err != nil {
		return fmt.Errorf("server build failed: %w", err)
	}

	if err := serverBuilder.Deploy(serverDir); err != nil {
		return fmt.Errorf("server deployment failed: %w", err)
	}

	e.log.Successf("Server deployed in %v", serverBuildResult.Duration)

	// Deploy client
	e.log.Info("Step 2/2: Deploying client...")
	clientDir := filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ClientDir)

	clientBuilder := build.NewClientBuilder(clientDir, e.ctx.Config.WebRoot, e.log)
	clientBuildResult, err := clientBuilder.Build()
	if err != nil {
		return fmt.Errorf("client build failed: %w", err)
	}

	if err := clientBuilder.Deploy(clientBuildResult.OutputDir); err != nil {
		return fmt.Errorf("client deployment failed: %w", err)
	}

	clientBuilder.RestartNginx()

	e.log.Successf("Client deployed in %v", clientBuildResult.Duration)
	return nil
}