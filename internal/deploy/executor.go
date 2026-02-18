package deploy

import (
	"fmt"
	"path/filepath"

	"github.com/Brayzonn/deploy-agent/internal/build"
	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/git"
	"github.com/Brayzonn/deploy-agent/internal/health"
	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/nginx"
	"github.com/Brayzonn/deploy-agent/internal/ssl"
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
	e.log.Infof("Branch: %s | Type: %s | Docker: %t | Fullstack: %t", 
		e.ctx.Branch, e.ctx.Config.ProjectType, e.ctx.Config.UseDocker, e.ctx.Config.FullStack)

	if err := e.git.CloneIfMissing(e.ctx.RepoFullName); err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}
	
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
	// Check if Docker deployment
	if e.ctx.Config.UseDocker {
		return e.deployDocker()
	}

	// Traditional deployments
	if e.ctx.Config.FullStack {
		return e.deployFullstack()
	}

	if e.ctx.Config.ProjectType == types.ProjectTypeClient {
		return e.deployClient()
	}

	return e.deployServer()
}

//  deploy using Docker
func (e *Executor) deployDocker() error {
	e.log.State(types.StateBuildingDocker)
	e.log.Info("Deploying with Docker...")

	workDir := e.ctx.Config.RepoDir
	if e.ctx.Config.ServerDir != "" && e.ctx.Config.ServerDir != "." {
		workDir = filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ServerDir)
	}

	e.log.Infof("Docker directory: %s", workDir)
	e.log.Infof("Compose file: %s", e.ctx.Config.DockerComposeFile)
	e.log.Infof("Env file: %s", e.ctx.Config.DockerEnvFile)

	dockerBuilder := build.NewDockerBuilder(
		workDir,
		e.ctx.Config.DockerComposeFile,
		e.ctx.Config.DockerEnvFile,
		e.log,
	)

	buildResult, err := dockerBuilder.Build()
	if err != nil {
		e.log.Errorf("Docker build failed: %v", err)
		return fmt.Errorf("docker build failed: %w", err)
	}

	e.log.Successf("Docker build completed in %v", buildResult.Duration)

	e.log.State(types.StateDeployingDocker)
	if err := dockerBuilder.Deploy(); err != nil {
		e.log.Errorf("Docker deployment failed: %v", err)
		
		logs, _ := dockerBuilder.GetLogs("", 50)
		e.log.Error("Container logs:")
		e.log.Error(logs)
		
		e.log.Warning("Attempting rollback...")
		dockerBuilder.Rollback()
		return fmt.Errorf("docker deployment failed: %w", err)
	}

	if e.ctx.Config.RequiresMigrations {
		e.log.State(types.StateRunningMigrations)
		if err := dockerBuilder.RunMigrations(e.ctx.Config.MigrationCommand); err != nil {
			e.log.Errorf("Migrations failed: %v", err)
			
			logs, _ := dockerBuilder.GetLogs("api", 100)
			e.log.Error("API container logs:")
			e.log.Error(logs)
			
			return fmt.Errorf("migrations failed: %w", err)
		}
		e.log.Success("Database migrations completed")
	}

	if e.ctx.Config.Domain != "" && e.ctx.Config.Port > 0 {
		nginxMgr := nginx.New(
			e.ctx.Config.Domain,
			e.ctx.Config.DomainAliases,
			"",
			e.ctx.Config.ProjectType,
			e.ctx.Config.Port,
			e.log,
		)
		
		if err := nginxMgr.Setup(); err != nil {
			e.log.Warningf("Nginx setup failed: %v", err)
		}

		sslMgr := ssl.New(
			e.ctx.Config.Domain,
			e.ctx.Config.DomainAliases,
			e.cfg.SSLEmail,
			e.log,
		)
		
		if err := sslMgr.Setup(); err != nil {
			e.log.Warningf("SSL setup failed: %v", err)
		}
	}

	if err := dockerBuilder.CheckHealth(); err != nil {
		e.log.Errorf("Container health check failed: %v", err)
		return fmt.Errorf("health check failed: %w", err)
	}

	if e.ctx.Config.HealthCheckURL != "" {
		healthChecker := health.New(
			e.ctx.Config.Domain,
			e.ctx.Config.Port,
			e.ctx.RepoName,
			e.ctx.Config.ProjectType,
			e.log,
		)

		if err := healthChecker.Check(); err != nil {
			e.log.Warningf("HTTP health check failed: %v", err)
		} else {
			e.log.Success("HTTP health check passed")
		}
	}

	e.log.Success("Docker deployment completed successfully!")
	return nil
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

	if e.ctx.Config.Domain != "" {
		nginxMgr := nginx.New(
			e.ctx.Config.Domain,
			e.ctx.Config.DomainAliases,
			e.ctx.Config.WebRoot,
			e.ctx.Config.ProjectType,
			e.ctx.Config.Port,
			e.log,
		)
		
		if err := nginxMgr.Setup(); err != nil {
			e.log.Warningf("Nginx setup failed: %v", err)
		}

		sslMgr := ssl.New(
			e.ctx.Config.Domain,
			e.ctx.Config.DomainAliases,
			e.cfg.SSLEmail,
			e.log,
		)
		
		if err := sslMgr.Setup(); err != nil {
			e.log.Warningf("SSL setup failed: %v", err)
		}
	}

	// Deploy to web root (get backup path)
	backupDir, err := clientBuilder.Deploy(buildResult.OutputDir)
	if err != nil {
		return fmt.Errorf("client deployment failed: %w", err)
	}

	healthChecker := health.New(
		e.ctx.Config.Domain,
		0, 
		"", 
		e.ctx.Config.ProjectType,
		e.log,
	)

	if err := healthChecker.Check(); err != nil {
		e.log.Errorf("Health check failed: %v", err)
		if backupDir != "" {
			e.log.Warning("Attempting automatic rollback...")
			if rollbackErr := clientBuilder.RestoreFromBackup(backupDir); rollbackErr != nil {
				e.log.Errorf("Rollback failed: %v", rollbackErr)
				return fmt.Errorf("deployment failed and rollback failed: health check error: %w, rollback error: %v", err, rollbackErr)
			}
			e.log.Success("Rollback completed - previous deployment restored")
		}
		return fmt.Errorf("deployment health check failed: %w", err)
	}

	return nil
}

//  deploys a backend-only project
func (e *Executor) deployServer() error {
	e.log.State(types.StateDeployingServer)
	e.log.Info("Deploying server API...")

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

	if e.ctx.Config.Domain != "" && e.ctx.Config.Port > 0 {
		nginxMgr := nginx.New(
			e.ctx.Config.Domain,
			e.ctx.Config.DomainAliases,
			"", 
			e.ctx.Config.ProjectType,
			e.ctx.Config.Port,
			e.log,
		)
		
		if err := nginxMgr.Setup(); err != nil {
			e.log.Warningf("Nginx setup failed: %v", err)
		}

		sslMgr := ssl.New(
			e.ctx.Config.Domain,
			e.ctx.Config.DomainAliases,
			e.cfg.SSLEmail,
			e.log,
		)
		
		if err := sslMgr.Setup(); err != nil {
			e.log.Warningf("SSL setup failed: %v", err)
		}
	}

	// Deploy with PM2
	if err := serverBuilder.Deploy(serverDir); err != nil {
		return fmt.Errorf("server deployment failed: %w", err)
	}

	e.log.Infof("Server build completed in %v", buildResult.Duration)

	healthChecker := health.New(
		e.ctx.Config.Domain,
		e.ctx.Config.Port,
		e.ctx.RepoName, 
		e.ctx.Config.ProjectType,
		e.log,
	)

	if err := healthChecker.Check(); err != nil {
		e.log.Errorf("Health check failed: %v", err)
		return fmt.Errorf("deployment health check failed: %w", err)
	}

	return nil
}

// deploy fullstack app
func (e *Executor) deployFullstack() error {
    e.log.State(types.StateDeployingFull)
    e.log.Info("Deploying fullstack application...")

    e.log.Info("Step 1/2: Deploying server...")
    
    originalDomain := e.ctx.Config.Domain
    originalAliases := e.ctx.Config.DomainAliases  
    
    if e.ctx.Config.Domain != "" {
        e.ctx.Config.Domain = "api." + e.ctx.Config.Domain
        e.ctx.Config.DomainAliases = []string{} 
    }

    if err := e.deployServer(); err != nil {
        e.ctx.Config.Domain = originalDomain 
        e.ctx.Config.DomainAliases = originalAliases  
        return err
    }

    e.ctx.Config.Domain = originalDomain
    e.ctx.Config.DomainAliases = originalAliases  

    e.log.Info("Step 2/2: Deploying client...")
    if err := e.deployClient(); err != nil {
        return err
    }

    e.log.Success("Fullstack deployment completed successfully!")
    return nil
}