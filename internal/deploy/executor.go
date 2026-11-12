package deploy

import (
	"errors"
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
	git        *git.Repo
	prevCommit string // commit hash before sync, used for rollback
}

// ============================================================
// Construction
// ============================================================

/* New creates an Executor for the given deployment context. */
func New(ctx *types.DeploymentContext, cfg *config.Config, log *logger.Logger) *Executor {
	repo := git.New(ctx.Config.RepoDir, ctx.Branch, log)

	return &Executor{
		ctx: ctx,
		cfg: cfg,
		log: log,
		git: repo,
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

	if err := stageEnvOverlay(e.ctx.Config.Name, e.ctx.Config.RepoDir, e.log); err != nil {
		return fmt.Errorf("env overlay staging failed: %w", err)
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
func (e *Executor) rollbackServer(builder *build.ServerBuilder, serverDir, domain string) {
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

	if err := e.serverHealthCheck(domain); err != nil {
		e.log.Errorf("Rolled-back version failed its health check too: %v — MANUAL INTERVENTION REQUIRED", err)
		return
	}

	e.log.Success("Server rolled back successfully and passed health check")
}

/*
serverHealthCheck runs the configured health check against the given
domain — the domain is a parameter because fullstack deploys serve
the API from a different domain than the repo's primary one.
*/
func (e *Executor) serverHealthCheck(domain string) error {
	return health.New(
		domain,
		e.ctx.Config.Port,
		e.ctx.RepoName,
		e.ctx.Config.ProjectType,
		e.ctx.Config.HealthCheckURL,
		e.ctx.Config.HealthCheckTimeout,
		e.log,
	).Check()
}

// ============================================================
// Edge (nginx + SSL)
// ============================================================

/*
setupEdge configures nginx and requests an SSL certificate for a
site, holding the global edge lock for the whole step. Failures are
warnings, not deploy failures — the app is up either way, and edge
config is fixable without redeploying. No-ops when no domain is
configured, or when a proxied (non-client) site has no port to proxy
to.
*/
func (e *Executor) setupEdge(domain string, aliases []string, webRoot string, pt types.ProjectType, port int, nginxMaxUpload int, spaMetaProxyPath string) {
	if domain == "" {
		return
	}
	if pt != types.ProjectTypeClient && port <= 0 {
		return
	}

	/*
		The deploy lock is per-repo but nginx/certbot are global server
		state, so the whole edge step — nginx.Setup through certbot to the
		final restore/reload — runs under the process-wide edge lock: a
		concurrent deploy of another repo cannot reload nginx or run
		certbot mid-regeneration. Lock ordering is deadlock-safe (per-repo
		lock always first, in Execute; edge lock second, here) and edge
		stays best-effort — a lock failure warns and skips, never fails
		the deploy.
	*/
	edgeLock, err := lock.AcquireEdge(e.cfg.StateDir, e.log)
	if err != nil {
		e.log.Warningf("Skipping nginx/SSL setup — could not acquire the edge lock: %v", err)
		return
	}
	defer edgeLock.Release()

	/*
		Create-once insurance: a domain with a DNS record pointing here but
		no matching vhost gets a refused TLS handshake instead of another
		site's certificate. Warn-only — the site's own edge setup proceeds
		regardless.
	*/
	if err := nginx.EnsureDefaultServer(e.log); err != nil {
		e.log.Warningf("Default TLS catch-all setup failed: %v", err)
	}

	nginxMgr := nginx.New(domain, aliases, webRoot, pt, port, nginxMaxUpload, spaMetaProxyPath, e.cfg.BackupDir, e.log)
	res, err := nginxMgr.Setup()
	if err != nil {
		e.log.Warningf("Nginx setup failed: %v", err)
		return
	}

	sslMgr := ssl.New(domain, aliases, e.cfg.SSLEmail, e.log)
	sslErr := sslMgr.Setup()

	/*
		The regenerated backup lifecycle spans BOTH the nginx step and this
		certbot step, so it is coordinated here rather than inside Setup. On
		a regenerated config the new on-disk body has no 443 block yet and
		nginx has NOT been reloaded (deferred-reload design); certbot is what
		installs SSL and reloads. If certbot fails (including a rate limit),
		restore the pre-deploy config so a live site is never left
		HTTPS-down; on success, reload once more as an idempotent safety net
		after certbot's own reload. The edge lock held above keeps other
		repos' deploys away from nginx/certbot for this whole window.
	*/
	if res.Outcome == nginx.OutcomeRegenerated {
		if sslErr != nil {
			e.log.Warningf("SSL setup failed on a regenerated config for %s: %v — restoring the pre-deploy config so the site is not left HTTPS-down", domain, sslErr)
			if restoreErr := nginxMgr.RestoreConfig(res.BackupPath); restoreErr != nil {
				e.log.Errorf("Failed to restore nginx config from backup %s — MANUAL INTERVENTION REQUIRED: %v", res.BackupPath, restoreErr)
			}
			return
		}
		if reloadErr := nginxMgr.Reload(); reloadErr != nil {
			e.log.Warningf("Post-certbot nginx reload failed: %v", reloadErr)
		}
	}

	if sslErr != nil && !errors.Is(sslErr, ssl.ErrRateLimited) {
		e.log.Warningf("SSL setup failed: %v", sslErr)
	}
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
		return e.deployClient(e.ctx.Config.Domain, e.ctx.Config.DomainAliases, e.ctx.Config.WebRoot)
	}

	return e.deployServer(e.ctx.Config.Domain, e.ctx.Config.DomainAliases)
}

/*
deployDocker builds and deploys a Docker Compose project: build
images, start infra and run migrations if configured, bring the
stack up, set up nginx/SSL, run health checks, and roll back on any
failure along the way. If the project also has a client, it's
deployed via a temporary config override — before the stack comes up
when SpaMetaProxyPath is set (see clientDeploysFirst), after
otherwise.
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
		e.ctx.RepoName,
		workDir,
		e.ctx.Config.DockerComposeFile,
		e.ctx.Config.DockerEnvFile,
		e.log,
	)

	buildResult, err := dockerBuilder.Build()
	if err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	e.log.Successf("Docker build completed in %v", buildResult.Duration)

	migrationsRan := false
	if e.ctx.Config.RequiresMigrations {
		e.log.State(types.StateRunningMigrations)
		if err := dockerBuilder.StartInfra(e.ctx.Config.InfraServices); err != nil {
			e.log.Warning("Attempting rollback after infra startup failure...")
			dockerBuilder.Rollback()
			return fmt.Errorf("infra startup failed: %w", err)
		}
		if err := dockerBuilder.RunMigrations(e.ctx.Config.MigrationService, e.ctx.Config.MigrationCommand); err != nil {
			e.log.Warning("Attempting rollback after migration failure...")
			e.log.Warning("Database may be PARTIALLY MIGRATED — rollback restores containers, not schema; verify migration state manually")
			dockerBuilder.Rollback()
			return fmt.Errorf("migrations failed: %w", err)
		}
		migrationsRan = true
		e.log.Success("Database migrations completed")
	}

	/*
		clientDeploysFirst: a repo with SpaMetaProxyPath set expects its
		Docker service to bind-mount the client's web root (to read
		index.html for meta injection), and a bind mount locks onto
		whatever directory exists at container-start time. deployClient's
		swap replaces that directory outright, so a container already
		running when the swap happens is left with a mount pointing at
		the directory the swap just replaced — and Compose's own "up -d"
		won't fix that on its own: it only recreates a container when it
		detects an image/config diff, and a directory swap under an
		unchanged bind-mount source is invisible to that diff. Deploying
		the client first, then force-recreating the service below,
		guarantees the container's mount is always established fresh
		against the just-swapped directory. Every other Docker+client
		repo keeps today's order and Compose's normal change detection —
		this only applies to repos that opted into the coupling.
	*/
	clientDeploysFirst := e.ctx.Config.ClientWebRoot != "" && e.ctx.Config.SpaMetaProxyPath != ""

	if clientDeploysFirst {
		e.log.Info("Deploying client application...")
		if err := e.deployClient(e.ctx.Config.ClientDomain, e.ctx.Config.ClientDomainAliases, e.ctx.Config.ClientWebRoot); err != nil {
			return fmt.Errorf("client deployment failed: %w", err)
		}

		if err := dockerBuilder.ForceRecreateService(e.ctx.Config.MigrationService); err != nil {
			e.log.Warningf("Failed to recreate the API container after client deploy — it may still have the previous client build mounted: %v", err)
		}
	}

	e.log.State(types.StateDeployingDocker)
	if err := dockerBuilder.Deploy(); err != nil {
		logs, _ := dockerBuilder.GetLogs("", 50)
		e.log.Error("Container logs:")
		e.log.Error(logs)

		e.log.Warning("Attempting rollback...")
		if migrationsRan {
			e.log.Warning("Database schema was already migrated for the new version — rolled-back containers will run OLD code against the NEW schema; this only works if the migrations were backward-compatible")
		}
		if clientDeploysFirst {
			e.log.Warning("Client was already deployed before this failure — rolled-back containers will run OLD server code against the NEW client build; this only works if the two are backward-compatible")
		}
		dockerBuilder.Rollback()
		return fmt.Errorf("docker deployment failed: %w", err)
	}

	e.setupEdge(e.ctx.Config.Domain, e.ctx.Config.DomainAliases, "", e.ctx.Config.ProjectType, e.ctx.Config.Port, e.ctx.Config.NginxMaxUploadMB, "")

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
			logs, _ := dockerBuilder.GetLogs("", 50)
			e.log.Error("Container logs:")
			e.log.Error(logs)

			e.log.Warningf("HTTP health check failed: %v — rolling back", err)
			if migrationsRan {
				e.log.Warning("Database schema was already migrated for the new version — rolled-back containers will run OLD code against the NEW schema; this only works if the migrations were backward-compatible")
			}
			if clientDeploysFirst {
				e.log.Warning("Client was already deployed before this failure — rolled-back containers will run OLD server code against the NEW client build; this only works if the two are backward-compatible")
			}
			dockerBuilder.Rollback()
			return fmt.Errorf("docker deployment failed health check: %w", err)
		}
		e.log.Success("HTTP health check passed")
	}

	dockerBuilder.CleanupOnSuccess()
	e.log.Success("Docker deployment completed successfully!")

	if e.ctx.Config.ClientWebRoot != "" && !clientDeploysFirst {
		e.log.Info("Deploying client application...")

		if err := e.deployClient(e.ctx.Config.ClientDomain, e.ctx.Config.ClientDomainAliases, e.ctx.Config.ClientWebRoot); err != nil {
			return fmt.Errorf("client deployment failed: %w", err)
		}
	}

	return nil
}

/*
deployClient builds a frontend-only project, sets up nginx/SSL if a
domain is configured, then deploys the build to the given web root.
On deploy failure it attempts to restore the pre-deploy backup.
Domain, aliases, and web root are explicit parameters because the
Docker and fullstack paths deploy the client to a different site
than the repo's primary config.
*/
func (e *Executor) deployClient(domain string, aliases []string, webRoot string) error {
	e.log.State(types.StateDeployingClient)
	e.log.Info("Deploying client application...")

	clientDir := e.ctx.Config.RepoDir
	if e.ctx.Config.ClientDir != "" && e.ctx.Config.ClientDir != "." {
		clientDir = filepath.Join(e.ctx.Config.RepoDir, e.ctx.Config.ClientDir)
	}

	e.log.Infof("Client directory: %s", clientDir)

	clientBuilder := build.NewClientBuilder(clientDir, webRoot, e.cfg.BackupDir, e.log)
	buildResult, err := clientBuilder.Build()
	if err != nil {
		return fmt.Errorf("client build failed: %w", err)
	}

	e.setupEdge(domain, aliases, webRoot, types.ProjectTypeClient, e.ctx.Config.Port, 0, e.ctx.Config.SpaMetaProxyPath)

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
server rollback. Domain and aliases are explicit parameters because
the fullstack path serves the API from its own subdomain.
*/
func (e *Executor) deployServer(domain string, aliases []string) error {
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

	e.setupEdge(domain, aliases, "", e.ctx.Config.ProjectType, e.ctx.Config.Port, e.ctx.Config.NginxMaxUploadMB, "")

	if err := serverBuilder.Deploy(serverDir); err != nil {
		e.rollbackServer(serverBuilder, serverDir, domain)
		return fmt.Errorf("server deployment failed: %w", err)
	}

	e.log.Infof("Server build completed in %v", buildResult.Duration)

	if err := e.serverHealthCheck(domain); err != nil {
		e.rollbackServer(serverBuilder, serverDir, domain)
		return fmt.Errorf("deployment health check failed: %w", err)
	}

	return nil
}

/*
deployFullstack deploys the server on its API domain (ServerDomain,
defaulting to "api." + Domain), then deploys the client on the
primary domain.
*/
func (e *Executor) deployFullstack() error {
	e.log.State(types.StateDeployingFull)
	e.log.Info("Deploying fullstack application...")

	serverDomain := e.ctx.Config.ServerDomain
	if serverDomain == "" && e.ctx.Config.Domain != "" {
		serverDomain = "api." + e.ctx.Config.Domain
	}

	e.log.Info("Step 1/2: Deploying server...")
	if err := e.deployServer(serverDomain, nil); err != nil {
		return err
	}

	e.log.Info("Step 2/2: Deploying client...")
	if err := e.deployClient(e.ctx.Config.Domain, e.ctx.Config.DomainAliases, e.ctx.Config.WebRoot); err != nil {
		return err
	}

	e.log.Success("Fullstack deployment completed successfully!")
	return nil
}
