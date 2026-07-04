package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// Timeouts per operation class: quick docker/compose queries, image
// builds, stack up/down, and migration runs.
const (
	dockerQueryTimeout = 1 * time.Minute
	dockerBuildTimeout = 30 * time.Minute
	stackTimeout       = 5 * time.Minute
	migrationTimeout   = 10 * time.Minute
)

type DockerBuilder struct {
	workDir     string
	composeFile string
	envFile     string
	log         *logger.Logger
	prevImages  []previousImage
}

/*
previousImage records everything needed to restore one service during
rollback: the exact image reference compose resolves for the service
(imageName, taken from the running container rather than guessed from
naming conventions), the image ID that was running before this deploy,
and the safety tag keeping that ID alive.
*/
type previousImage struct {
	service   string
	imageName string // reference compose resolves for this service, e.g. "example_api:latest"
	imageID   string // image ID running before this deploy
	prevTag   string // safety tag holding imageID, e.g. "example-api:previous"
}

// ============================================================
// Compose binary detection
// ============================================================

/*
composeBase resolves which Compose binary this host has, exactly
once per process: the v2 plugin ("docker compose") is preferred,
falling back to the EOL v1 "docker-compose" binary. Every compose
invocation in this package goes through compose()/composeOut(), so
the whole deploy speaks to a single, consistent Compose version —
v1 and v2 differ in image naming and ps output.
*/
var (
	composeOnce sync.Once
	composeCmd  []string
)

func composeBase() []string {
	composeOnce.Do(func() {
		if _, err := run.Cmd(dockerQueryTimeout, "", "docker", "compose", "version"); err == nil {
			composeCmd = []string{"docker", "compose"}
			return
		}
		composeCmd = []string{"docker-compose"}
	})
	return composeCmd
}

/*
compose runs a Compose subcommand against this builder's compose
file in its work dir, returning combined stdout+stderr.
*/
func (d *DockerBuilder) compose(timeout time.Duration, args ...string) (string, error) {
	base := composeBase()
	full := append(append([]string{}, base[1:]...), "-f", d.composeFile)
	full = append(full, args...)
	return run.Cmd(timeout, d.workDir, base[0], full...)
}

/* composeOut is compose but returns stdout only, for output that gets parsed. */
func (d *DockerBuilder) composeOut(timeout time.Duration, args ...string) (string, error) {
	base := composeBase()
	full := append(append([]string{}, base[1:]...), "-f", d.composeFile)
	full = append(full, args...)
	return run.Out(timeout, d.workDir, base[0], full...)
}

// ============================================================
// Construction
// ============================================================

/*
NewDockerBuilder creates a DockerBuilder for workDir, defaulting the
compose and env file names when the caller doesn't override them.
*/
func NewDockerBuilder(workDir, composeFile, envFile string, log *logger.Logger) *DockerBuilder {
	if composeFile == "" {
		composeFile = "docker-compose.prod.yml"
	}
	if envFile == "" {
		envFile = ".env.production"
	}

	log.Infof("Using Compose command: %s", strings.Join(composeBase(), " "))

	return &DockerBuilder{
		workDir:     workDir,
		composeFile: composeFile,
		envFile:     envFile,
		log:         log,
	}
}

// ============================================================
// Image tagging
// ============================================================

/*
tagPreviousImages tags each currently running service's image as
"<project>-<service>:previous" so Rollback() has something to
restore if the upcoming build or deploy fails. Alongside the tag it
records the image reference the container was actually created from
({{.Config.Image}}), so rollback can restore onto the exact name
compose resolves instead of guessing a naming convention.
*/
func (d *DockerBuilder) tagPreviousImages() {
	d.prevImages = nil

	out, err := d.composeOut(dockerQueryTimeout, "config", "--services")
	if err != nil {
		d.log.Warningf("Could not list compose services for pre-tagging: %v", err)
		return
	}

	for svc := range strings.FieldsSeq(out) {
		containerID, err := d.composeOut(dockerQueryTimeout, "ps", "-q", svc)
		if err != nil || strings.TrimSpace(containerID) == "" {
			continue
		}

		info, err := run.Out(dockerQueryTimeout, d.workDir, "docker", "inspect",
			"--format", "{{.Image}}|{{.Config.Image}}",
			strings.TrimSpace(containerID),
		)
		if err != nil {
			continue
		}

		imageID, imageName, ok := strings.Cut(strings.TrimSpace(info), "|")
		if !ok || imageID == "" || imageName == "" {
			continue
		}

		prevTag := fmt.Sprintf("%s-%s:previous", d.projectName(), svc)
		if _, err := run.Cmd(dockerQueryTimeout, d.workDir, "docker", "tag", imageID, prevTag); err != nil {
			d.log.Warningf("Could not tag previous image for %s: %v", svc, err)
			continue
		}

		d.prevImages = append(d.prevImages, previousImage{
			service:   svc,
			imageName: imageName,
			imageID:   imageID,
			prevTag:   prevTag,
		})
		d.log.Infof("Tagged previous image for %s (%s) as %s", svc, imageName, prevTag)
	}
}

/* cleanPreviousTags removes the ":previous" tags after a successful deployment. */
func (d *DockerBuilder) cleanPreviousTags() {
	for _, prev := range d.prevImages {
		run.Cmd(dockerQueryTimeout, d.workDir, "docker", "rmi", prev.prevTag)
	}
	d.prevImages = nil
}

/* projectName derives a stable image-tag prefix from the working directory name. */
func (d *DockerBuilder) projectName() string {
	return strings.ToLower(filepath.Base(d.workDir))
}

// ============================================================
// Build
// ============================================================

/*
Build compiles the Docker images for all services defined in the
compose file. It tags currently-running images as ":previous"
first so Rollback() has something to restore if the build fails.
*/
func (d *DockerBuilder) Build() (*types.BuildOutput, error) {
	d.tagPreviousImages()

	d.log.Info("Building Docker containers...")
	startTime := time.Now()

	composePath := filepath.Join(d.workDir, d.composeFile)
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("docker-compose file not found: %s", d.composeFile)
	}
	envPath := filepath.Join(d.workDir, d.envFile)
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		d.log.Warningf("Env file not found: %s", d.envFile)
	}

	output, err := d.compose(dockerBuildTimeout, "build")
	duration := time.Since(startTime)

	if err != nil {
		d.log.Errorf("Docker build failed after %v", duration)
		d.log.Error(output)
		return nil, fmt.Errorf("compose build failed after %v: %w\nOutput: %s", duration, err, output)
	}

	d.log.Successf("Docker build completed in %v", duration)

	return &types.BuildOutput{
		OutputDir: d.workDir,
		Duration:  duration,
	}, nil
}

// ============================================================
// Deployment
// ============================================================

/*
Deploy brings the compose stack up in detached mode, waits for the
containers to settle, then verifies they're healthy.
*/
func (d *DockerBuilder) Deploy() error {
	d.log.Info("Deploying Docker containers...")

	d.log.Info("Starting containers...")
	output, err := d.compose(stackTimeout, "up", "-d", "--remove-orphans")
	if err != nil {
		return fmt.Errorf("failed to start containers: %w\nOutput: %s", err, output)
	}

	d.log.Info("Waiting for containers to be healthy...")
	if err := d.CheckHealth(); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	d.log.Success("Docker containers deployed successfully")
	return nil
}

// ============================================================
// Infrastructure
// ============================================================

/*
StartInfra brings up dependency services (e.g. postgres, redis)
ahead of migrations, so migrations have something to connect to.
No-ops if no services are configured.
*/
func (d *DockerBuilder) StartInfra(services []string) error {
	if len(services) == 0 {
		return nil
	}

	d.log.Infof("Starting infra services: %s", strings.Join(services, ", "))
	args := append([]string{"up", "-d"}, services...)

	output, err := d.compose(stackTimeout, args...)
	if err != nil {
		return fmt.Errorf("failed to start infra services: %w\nOutput: %s", err, output)
	}

	d.log.Info("Waiting for infra services to be ready...")
	return d.waitForServices(services, 60*time.Second)
}

/*
RunMigrations runs the migration command inside a throwaway container
for the configured migration service (default "api") against the
started infra, defaulting to a Prisma deploy command if none is
configured.

Migrations run before the new containers replace the old ones, and a
later rollback restores containers but never the schema — so by
policy, migrations must be backward-compatible with the previous
release (additive changes; no drops or renames in the same deploy).
*/
func (d *DockerBuilder) RunMigrations(service, migrationCmd string) error {
	if service == "" {
		service = "api"
	}
	if migrationCmd == "" {
		migrationCmd = "npx prisma migrate deploy"
	}

	d.log.Info("Running database migrations...")

	args := append([]string{"run", "--rm", service}, strings.Fields(migrationCmd)...)

	output, err := d.compose(migrationTimeout, args...)
	if err != nil {
		return fmt.Errorf("migrations failed: %w\nOutput: %s", err, output)
	}

	d.log.Success("Migrations completed successfully")
	d.log.Info(output)
	return nil
}

// ============================================================
// Health
// ============================================================

/*
CheckHealth waits until every compose service's containers are
running — and "healthy" where the container defines a healthcheck —
polling actual container state instead of substring-matching ps
output.
*/
func (d *DockerBuilder) CheckHealth() error {
	out, err := d.composeOut(dockerQueryTimeout, "config", "--services")
	if err != nil {
		return fmt.Errorf("failed to list compose services: %w", err)
	}

	return d.waitForServices(strings.Fields(out), 60*time.Second)
}

/*
waitForServices polls the given services until each has a running
container ("healthy" when a healthcheck is defined) or the deadline
passes. Exited, dead, or unhealthy containers fail immediately —
waiting won't fix them.
*/
func (d *DockerBuilder) waitForServices(services []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		pending, failed := d.serviceStates(services)

		if len(failed) > 0 {
			return fmt.Errorf("containers are not healthy: %s", strings.Join(failed, ", "))
		}
		if len(pending) == 0 {
			d.log.Success("All containers are running")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("containers not ready after %s: %s", timeout, strings.Join(pending, ", "))
		}

		time.Sleep(2 * time.Second)
	}
}

/*
serviceStates inspects each service's containers and buckets the
service as pending (starting up) or failed (exited, dead, or
unhealthy). A service absent from both is ready.
*/
func (d *DockerBuilder) serviceStates(services []string) (pending, failed []string) {
	for _, svc := range services {
		out, err := d.composeOut(dockerQueryTimeout, "ps", "-q", svc)
		if err != nil || strings.TrimSpace(out) == "" {
			pending = append(pending, fmt.Sprintf("%s (no container)", svc))
			continue
		}

		for containerID := range strings.FieldsSeq(out) {
			info, err := run.Out(dockerQueryTimeout, d.workDir, "docker", "inspect",
				"--format", "{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}",
				containerID,
			)
			if err != nil {
				pending = append(pending, fmt.Sprintf("%s (could not inspect)", svc))
				continue
			}

			status, healthStatus, _ := strings.Cut(strings.TrimSpace(info), "|")
			switch {
			case status == "exited" || status == "dead" || healthStatus == "unhealthy":
				failed = append(failed, fmt.Sprintf("%s (%s%s)", svc, status, healthSuffix(healthStatus)))
			case status != "running" || healthStatus == "starting":
				pending = append(pending, fmt.Sprintf("%s (%s%s)", svc, status, healthSuffix(healthStatus)))
			}
		}
	}

	return pending, failed
}

/* healthSuffix formats a container's health status for state messages, if it has one. */
func healthSuffix(healthStatus string) string {
	if healthStatus == "" {
		return ""
	}
	return ", health: " + healthStatus
}

// ============================================================
// Diagnostics
// ============================================================

/* GetLogs returns recent logs for a service (or all services if service is ""). */
func (d *DockerBuilder) GetLogs(service string, tail int) (string, error) {
	args := []string{"logs"}
	if service != "" {
		args = append(args, service)
	}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}

	output, err := d.compose(dockerQueryTimeout, args...)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return output, nil
}

// ============================================================
// Rollback
// ============================================================

/*
Rollback brings up the previously tagged images. If no previous
images were tagged (first deploy), it falls back to `docker-compose
down` so the caller knows the stack is stopped rather than left in
a broken state.
*/
func (d *DockerBuilder) Rollback() error {
	d.log.Warning("Rolling back Docker deployment...")

	if len(d.prevImages) == 0 {
		d.log.Warning("No previous images tagged — bringing stack down cleanly")
		if _, err := d.compose(stackTimeout, "down"); err != nil {
			return fmt.Errorf("rollback (down) failed: %w", err)
		}
		d.log.Warning("Stack is down — no previous version to restore")
		return nil
	}

	d.log.Info("Restoring previous images...")

	for _, prev := range d.prevImages {
		if _, err := run.Cmd(dockerQueryTimeout, d.workDir, "docker", "tag", prev.prevTag, prev.imageName); err != nil {
			d.log.Warningf("Could not retag %s onto %s for service %s: %v",
				prev.prevTag, prev.imageName, prev.service, err)
		}
	}

	if output, err := d.compose(stackTimeout, "up", "-d", "--no-build", "--remove-orphans"); err != nil {
		return fmt.Errorf("failed to bring up previous containers: %w\nOutput: %s", err, output)
	}

	// Keep the :previous tags around for manual recovery if verification fails.
	if err := d.verifyRollback(); err != nil {
		return fmt.Errorf("rollback verification failed: %w", err)
	}

	d.log.Success("Previous version restored successfully")
	d.cleanPreviousTags()
	return nil
}

/*
verifyRollback confirms each rolled-back service is actually running
the image ID recorded before the deploy, instead of trusting that the
retag + up worked.
*/
func (d *DockerBuilder) verifyRollback() error {
	var wrong []string

	for _, prev := range d.prevImages {
		containerID, err := d.composeOut(dockerQueryTimeout, "ps", "-q", prev.service)
		if err != nil || strings.TrimSpace(containerID) == "" {
			wrong = append(wrong, fmt.Sprintf("%s (not running)", prev.service))
			continue
		}

		imageID, err := run.Out(dockerQueryTimeout, d.workDir, "docker", "inspect",
			"--format", "{{.Image}}",
			strings.TrimSpace(containerID),
		)
		if err != nil {
			wrong = append(wrong, fmt.Sprintf("%s (could not inspect container)", prev.service))
			continue
		}

		if strings.TrimSpace(imageID) != prev.imageID {
			wrong = append(wrong, fmt.Sprintf("%s (still running the new image)", prev.service))
		}
	}

	if len(wrong) > 0 {
		return fmt.Errorf("services not restored to previous images: %s", strings.Join(wrong, ", "))
	}

	return nil
}

/* CleanupOnSuccess removes the ":previous" tags after a successful deployment. */
func (d *DockerBuilder) CleanupOnSuccess() {
	if len(d.prevImages) > 0 {
		d.log.Info("Cleaning up previous image tags...")
		d.cleanPreviousTags()
	}
}
