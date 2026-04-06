package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type DockerBuilder struct {
	workDir     string
	composeFile string
	envFile     string
	log         *logger.Logger
	// service names discovered from the running stack before build
	prevTagged []string
}

func NewDockerBuilder(workDir, composeFile, envFile string, log *logger.Logger) *DockerBuilder {
	if composeFile == "" {
		composeFile = "docker-compose.prod.yml"
	}
	if envFile == "" {
		envFile = ".env.production"
	}

	return &DockerBuilder{
		workDir:     workDir,
		composeFile: composeFile,
		envFile:     envFile,
		log:         log,
	}
}

// tagPreviousImages tags each running service image as <image>:previous so we
// can restore them if the new build fails.
func (d *DockerBuilder) tagPreviousImages() {
	d.prevTagged = nil

	// Get service names from the compose file
	cmd := exec.Command("docker-compose", "-f", d.composeFile, "config", "--services")
	cmd.Dir = d.workDir
	out, err := cmd.Output()
	if err != nil {
		d.log.Warningf("Could not list compose services for pre-tagging: %v", err)
		return
	}

	services := strings.Fields(string(out))
	for _, svc := range services {
		// Get the current image ID for this service
		psCmd := exec.Command("docker-compose", "-f", d.composeFile, "ps", "-q", svc)
		psCmd.Dir = d.workDir
		containerID, err := psCmd.Output()
		if err != nil || strings.TrimSpace(string(containerID)) == "" {
			continue // service not running, nothing to tag
		}

		imgCmd := exec.Command("docker", "inspect",
			"--format", "{{.Image}}",
			strings.TrimSpace(string(containerID)),
		)
		imageID, err := imgCmd.Output()
		if err != nil || strings.TrimSpace(string(imageID)) == "" {
			continue
		}

		tag := fmt.Sprintf("%s-%s:previous", d.projectName(), svc)
		tagCmd := exec.Command("docker", "tag", strings.TrimSpace(string(imageID)), tag)
		if err := tagCmd.Run(); err != nil {
			d.log.Warningf("Could not tag previous image for %s: %v", svc, err)
			continue
		}

		d.prevTagged = append(d.prevTagged, tag)
		d.log.Infof("Tagged previous image for %s as %s", svc, tag)
	}
}

// cleanPreviousTags removes the :previous tags after a successful deployment.
func (d *DockerBuilder) cleanPreviousTags() {
	for _, tag := range d.prevTagged {
		exec.Command("docker", "rmi", tag).Run()
	}
	d.prevTagged = nil
}

// projectName derives a stable prefix from the compose file directory.
func (d *DockerBuilder) projectName() string {
	return strings.ToLower(filepath.Base(d.workDir))
}

func (d *DockerBuilder) Build() (*types.BuildOutput, error) {
	// Tag currently running images before stopping them
	d.tagPreviousImages()

	d.log.Info("Stopping any existing containers before build...")
	stopCmd := exec.Command("docker-compose", "-f", d.composeFile, "down", "--remove-orphans", "--timeout", "30")
	stopCmd.Dir = d.workDir
	stopCmd.Run()

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

	cmd := exec.Command("docker-compose", "-f", d.composeFile, "build")
	cmd.Dir = d.workDir

	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	if err != nil {
		d.log.Errorf("Docker build failed after %v", duration)
		d.log.Error(string(output))
		return &types.BuildOutput{
			Success:  false,
			Duration: duration,
			Error:    fmt.Errorf("docker build failed: %w\nOutput: %s", err, string(output)),
		}, err
	}

	d.log.Successf("Docker build completed in %v", duration)

	return &types.BuildOutput{
		Success:   true,
		OutputDir: d.workDir,
		Duration:  duration,
		Error:     nil,
	}, nil
}

func (d *DockerBuilder) Deploy() error {
	d.log.Info("Deploying Docker containers...")

	d.log.Info("Starting containers...")
	cmd := exec.Command("docker-compose", "-f", d.composeFile, "up", "-d", "--build")
	cmd.Dir = d.workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start containers: %w\nOutput: %s", err, string(output))
	}

	d.log.Info("Waiting for containers to be healthy...")
	time.Sleep(10 * time.Second)

	if err := d.CheckHealth(); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	d.log.Success("Docker containers deployed successfully")
	return nil
}

func (d *DockerBuilder) RunMigrations(migrationCmd string) error {
	if migrationCmd == "" {
		migrationCmd = "npx prisma migrate deploy"
	}

	d.log.Info("Running database migrations...")

	parts := strings.Fields(migrationCmd)

	cmd := exec.Command("docker-compose", "-f", d.composeFile, "exec", "-T", "api")
	cmd.Args = append(cmd.Args, parts...)
	cmd.Dir = d.workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("migrations failed: %w\nOutput: %s", err, string(output))
	}

	d.log.Success("Migrations completed successfully")
	d.log.Info(string(output))
	return nil
}

func (d *DockerBuilder) CheckHealth() error {
	cmd := exec.Command("docker-compose", "-f", d.composeFile, "ps")
	cmd.Dir = d.workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Exit") || strings.Contains(line, "Restarting") {
			return fmt.Errorf("some containers are not healthy:\n%s", string(output))
		}
	}

	d.log.Success("All containers are running")
	return nil
}

func (d *DockerBuilder) GetLogs(service string, tail int) (string, error) {
	args := []string{"-f", d.composeFile, "logs"}
	if service != "" {
		args = append(args, service)
	}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}

	cmd := exec.Command("docker-compose", args...)
	cmd.Dir = d.workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return string(output), nil
}

// Rollback brings up the previously tagged images. If no previous images were
// tagged (first deploy), it falls back to docker compose down so the caller
// knows the stack is stopped rather than in a broken state.
func (d *DockerBuilder) Rollback() error {
	d.log.Warning("Rolling back Docker deployment...")

	if len(d.prevTagged) == 0 {
		d.log.Warning("No previous images tagged — bringing stack down cleanly")
		cmd := exec.Command("docker-compose", "-f", d.composeFile, "down")
		cmd.Dir = d.workDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("rollback (down) failed: %w", err)
		}
		d.log.Warning("Stack is down — no previous version to restore")
		return nil
	}

	// Restore previous images by re-tagging them back onto the service image names
	// then bringing the stack back up.
	d.log.Info("Restoring previous images...")

	// Get the image names the compose file expects for each service
	cmd := exec.Command("docker-compose", "-f", d.composeFile, "config", "--services")
	cmd.Dir = d.workDir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("could not list services during rollback: %w", err)
	}

	services := strings.Fields(string(out))
	for _, svc := range services {
		prevTag := fmt.Sprintf("%s-%s:previous", d.projectName(), svc)
		// Get the compose-expected image name for this service
		imgNameCmd := exec.Command("docker-compose", "-f", d.composeFile, "config")
		imgNameCmd.Dir = d.workDir
		// Re-tag previous image as latest for this service
		retagCmd := exec.Command("docker", "tag", prevTag,
			fmt.Sprintf("%s-%s:latest", d.projectName(), svc))
		if err := retagCmd.Run(); err != nil {
			d.log.Warningf("Could not retag previous image for %s: %v", svc, err)
		}
	}

	upCmd := exec.Command("docker-compose", "-f", d.composeFile, "up", "-d", "--no-build")
	upCmd.Dir = d.workDir
	if output, err := upCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to bring up previous containers: %w\nOutput: %s", err, string(output))
	}

	d.log.Success("Previous version restored successfully")
	d.cleanPreviousTags()
	return nil
}

// CleanupOnSuccess removes the :previous tags after a successful deployment.
func (d *DockerBuilder) CleanupOnSuccess() {
	if len(d.prevTagged) > 0 {
		d.log.Info("Cleaning up previous image tags...")
		d.cleanPreviousTags()
	}
}
