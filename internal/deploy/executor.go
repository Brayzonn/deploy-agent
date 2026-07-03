package deploy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Brayzonn/deploy-agent/internal/build"
	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/git"
	"github.com/Brayzonn/deploy-agent/internal/health"
	"github.com/Brayzonn/deploy-agent/internal/lock"
	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/nginx"
	"github.com/Brayzonn/deploy-agent/internal/ssl"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type Executor struct {
	ctx        *types.DeploymentContext
	cfg        *config.Config
	log        *logger.Logger
	git        *git.GitManager
	prevCommit string // commit hash before sync, used for rollback
}

// ============================================================
// Construction
// ============================================================

/* New creates an Executor for the given deployment context. */
func New(ctx *types.DeploymentContext, cfg *config.Config, log *logger.Logger) *Executor {
	gitManager := git.New(ctx.Config.RepoDir, ctx.Branch, log)

	return &Executor{
		ctx: ctx,
		cfg: cfg,
		log: log,
		git: gitManager,
	}
}

// ============================================================
// Orchestration
// ============================================================

/*
Execute runs a full deployment: check the push is for the configured
deploy branch, take the per-repo deploy lock, clone-or-validate the
repo, force the checkout to exactly the webhook commit, then hand
off to deploy(). Concurrent pushes serialize behind the lock and
deploy in order. Pushes to other branches and redeliveries of the
already-deployed commit exit successfully without deploying.
*/
func (e *Executor) Execute() error {
	e.log.State(types.StateStarting)
	e.log.Infof("Starting deployment for %s", e.ctx.RepoName)
	e.log.Infof("Branch: %s | Type: %s | Docker: %t | Fullstack: %t",
		e.ctx.Branch, e.ctx.Config.ProjectType, e.ctx.Config.UseDocker, e.ctx.Config.FullStack)

	if e.ctx.Branch != e.ctx.Config.DeployBranch {
		e.log.Infof("Ignoring push to branch %q — %s deploys from %q only",
			e.ctx.Branch, e.ctx.RepoName, e.ctx.Config.DeployBranch)
		return nil
	}

	deployLock, err := lock.Acquire(e.cfg.StateDir, e.ctx.RepoName, e.log)
	if err != nil {
		return fmt.Errorf("failed to acquire deploy lock: %w", err)
	}
	defer deployLock.Release()

	freshClone, err := e.git.CloneIfMissing(e.ctx.RepoFullName)
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	if err := e.git.Validate(); err != nil {
		return fmt.Errorf("git validation failed: %w", err)
	}

	if !freshClone {
		if commit, err := e.git.GetCurrentCommit(); err == nil {
			if commit == e.ctx.Commit {
				e.log.Success("Already at the requested commit. Nothing to deploy!")
				return nil
			}
			e.prevCommit = commit
		} else {
			e.log.Warningf("Could not capture pre-sync commit for rollback: %v", err)
		}

		if changes, err := e.git.LocalChanges(); err != nil {
			e.log.Warningf("Could not check for local changes: %v", err)
		} else if len(changes) > 0 {
			e.log.Warningf("Discarding local changes to %d tracked file(s) on the server:\n%s",
				len(changes), strings.Join(changes, "\n"))
		}
	}

	e.log.State(types.StateSyncing)
	if err := e.git.SyncToCommit(e.ctx.Commit); err != nil {
		return fmt.Errorf("git sync failed: %w", err)
	}

	if err := e.deploy(); err != nil {
		return err
	}

	e.log.State(types.StateSuccess)
	e.log.Success("Deployment completed successfully!")
	e.log.Successf("Deployed %s (%s) to %s branch", e.ctx.RepoName, e.ctx.Commit[:7], e.ctx.Branch)

	return nil
}

// ============================================================
// Rollback
// ============================================================

/*
rollbackGit resets the repo to prevCommit. Reports whether the
checkout actually holds the previous commit afterward, so callers
don't rebuild the failed code thinking it's the old version.
*/
func (e *Executor) rollbackGit() bool {
	if e.prevCommit == "" {
		e.log.Warning("No pre-sync commit recorded — cannot rollback git")
		return false
	}
	if err := e.git.ResetHard(e.prevCommit); err != nil {
		e.log.Errorf("Git rollback failed: %v", err)
		return false
	}
	return true
}

/*
rollbackServer resets git to the pre-sync commit, rebuilds, restarts
PM2, and re-runs the health check to confirm the previous version is
actually serving. If there is no commit to roll back to (fresh clone
or failed capture), it stops instead of rebuilding the code that
just failed, and says so.
*/
func (e *Executor) rollbackServer(builder *build.ServerBuilder, serverDir string) {
	e.log.Warning("Rolling back server deployment...")

	if !e.rollbackGit() {
		e.log.Error("Cannot roll back: no previous commit to return to — MANUAL INTERVENTION REQUIRED; whatever PM2 is running is still the failed deploy")
		return
	}

	e.log.Info("Rebuilding previous version...")
	if _, err := builder.Build(); err != nil {
		e.log.Errorf("Rollback build failed: %v — MANUAL INTERVENTION REQUIRED", err)
		return
	}

	e.log.Info("Restarting PM2 with previous version...")
	if err := builder.Deploy(serverDir); err != nil {
		e.log.Errorf("Rollback PM2 restart failed: %v — MANUAL INTERVENTION REQUIRED", err)
		return
	}

	healthChecker := health.New(
		e.ctx.Config.Domain,
		e.ctx.Config.Port,
		e.ctx.RepoName,
		e.ctx.Config.ProjectType,
		e.ctx.Config.HealthCheckURL,
		e.ctx.Config.HealthCheckTimeout,
		e.log,
	)

	if err := healthChecker.Check(); err != nil {
		e.log.Errorf("Rolled-back version failed its health check too: %v — MANUAL INTERVENTION REQUIRED", err)
		return
	}

	e.log.Success("Server rolled back successfully and passed health check")
}

// ============================================================
// Deployment strategies
// ============================================================

/* deploy routes to the right deployment strategy based on the repo's project config. */
func (e *Executor) deploy() error {
	if e.ctx.Config.UseDocker {
		return e.deployDocker()
	}

	if e.ctx.Config.FullStack {
		return e.deployFullstack()
	}

	if e.ctx.Config.ProjectType == types.ProjectTypeClient {
		return e.deployClient()
	}

	return e.deployServer()
}

/*
deployDocker builds and deploys a Docker Compose project: build
images, start infra and run migrations if configured, bring the
stack up, set up nginx/SSL, run health checks, and roll back on any
failure along the way. If the project also has a client, it's
deployed afterward via a temporary config override.
*/
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

	if e.ctx.Config.RequiresMigrations {
		e.log.State(types.StateRunningMigrations)
		if err := dockerBuilder.StartInfra(e.ctx.Config.InfraServices); err != nil {
			e.log.Errorf("Failed to start infra services: %v", err)
			dockerBuilder.Rollback()
			return fmt.Errorf("infra startup failed: %w", err)
		}
		if err := dockerBuilder.RunMigrations(e.ctx.Config.MigrationCommand); err != nil {
			e.log.Errorf("Migrations failed: %v", err)
			e.log.Warning("Attempting rollback after migration failure...")
			dockerBuilder.Rollback()
			return fmt.Errorf("migrations failed: %w", err)
		}
		e.log.Success("Database migrations completed")
	}

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
		e.log.Warning("Attempting rollback after health check failure...")
		dockerBuilder.Rollback()
		return fmt.Errorf("health check failed: %w", err)
	}

	if e.ctx.Config.HealthCheckURL != "" {
		healthChecker := health.New(
			e.ctx.Config.Domain,
			e.ctx.Config.Port,
			e.ctx.RepoName,
			e.ctx.Config.ProjectType,
			e.ctx.Config.HealthCheckURL,
			e.ctx.Config.HealthCheckTimeout,
			e.log,
		)

		if err := healthChecker.Check(); err != nil {
			e.log.Warningf("HTTP health check failed: %v", err)
		} else {
			e.log.Success("HTTP health check passed")
		}
	}

	dockerBuilder.CleanupOnSuccess()
	e.log.Success("Docker deployment completed successfully!")

	if e.ctx.Config.ClientWebRoot != "" {
		e.log.Info("Deploying client application...")

		origDomain := e.ctx.Config.Domain
		origAliases := e.ctx.Config.DomainAliases
		origWebRoot := e.ctx.Config.WebRoot
		origType := e.ctx.Config.ProjectType

		e.ctx.Config.Domain = e.ctx.Config.ClientDomain
		e.ctx.Config.DomainAliases = e.ctx.Config.ClientDomainAliases
		e.ctx.Config.WebRoot = e.ctx.Config.ClientWebRoot
		e.ctx.Config.ProjectType = types.ProjectTypeClient

		err := e.deployClient()

		e.ctx.Config.Domain = origDomain
		e.ctx.Config.DomainAliases = origAliases
		e.ctx.Config.WebRoot = origWebRoot
		e.ctx.Config.ProjectType = origType

		if err != nil {
			return fmt.Errorf("client deployment failed: %w", err)
		}
	}

	return nil
}

/*
deployClient builds a frontend-only project, sets up nginx/SSL if a
domain is configured, then deploys the build to the web root. On
deploy failure it attempts to restore the pre-deploy backup.
*/
func (e *Executor) deployClient() error {
	e.log.State(types.StateDeployingClient)
	e.log.Info("Deploying client application...")

	clientDir := e.ctx.Config.RepoDir
	if e.ctx.Config.ClientDir != "" && e.ctx.Config.ClientDir != "." {
		clientDir = filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ClientDir)
	}

	e.log.Infof("Client directory: %s", clientDir)

	clientBuilder := build.NewClientBuilder(clientDir, e.ctx.Config.WebRoot, e.cfg.BackupDir, e.log)
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

	backupDir, err := clientBuilder.Deploy(buildResult.OutputDir)
	if err != nil {
		if backupDir != "" {
			e.log.Warning("Client deployment failed, attempting to restore backup...")
			if restoreErr := clientBuilder.RestoreFromBackup(backupDir); restoreErr != nil {
				e.log.Errorf("Backup restore failed: %v", restoreErr)
			}
		}
		return fmt.Errorf("client deployment failed: %w", err)
	}

	return nil
}

/*
deployServer builds a backend-only project, sets up nginx/SSL if
configured, deploys via PM2, and runs a health check. Build
failures reset git; deploy or health-check failures trigger a full
server rollback.
*/
func (e *Executor) deployServer() error {
	e.log.State(types.StateDeployingServer)
	e.log.Info("Deploying server API...")

	serverDir := filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ServerDir)
	e.log.Infof("Server directory: %s", serverDir)

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
		e.rollbackGit()
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

	if err := serverBuilder.Deploy(serverDir); err != nil {
		e.rollbackServer(serverBuilder, serverDir)
		return fmt.Errorf("server deployment failed: %w", err)
	}

	e.log.Infof("Server build completed in %v", buildResult.Duration)

	healthChecker := health.New(
		e.ctx.Config.Domain,
		e.ctx.Config.Port,
		e.ctx.RepoName,
		e.ctx.Config.ProjectType,
		e.ctx.Config.HealthCheckURL,
		e.ctx.Config.HealthCheckTimeout,
		e.log,
	)

	if err := healthChecker.Check(); err != nil {
		e.log.Errorf("Health check failed: %v", err)
		e.rollbackServer(serverBuilder, serverDir)
		return fmt.Errorf("deployment health check failed: %w", err)
	}

	return nil
}

/*
deployFullstack deploys the server first, temporarily pointing its
domain at the "api." subdomain, then restores the original domain
and deploys the client.
*/
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
