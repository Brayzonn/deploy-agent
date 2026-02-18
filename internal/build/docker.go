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
    workDir       string
    composeFile   string
    envFile       string
    log           *logger.Logger
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

func (d *DockerBuilder) Build() (*types.BuildOutput, error) {
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
    cmd := exec.Command("docker-compose", "-f", d.composeFile, "build", "--no-cache")
    cmd.Dir = d.workDir

    output, err := cmd.CombinedOutput()
    duration := time.Since(startTime)

    if err != nil {
        d.log.Errorf("Docker build failed after %v", duration)
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

    d.log.Info("Stopping existing containers...")
    stopCmd := exec.Command("docker-compose", "-f", d.composeFile, "down", "--remove-orphans")
    stopCmd.Dir = d.workDir
    if err := stopCmd.Run(); err != nil {
        d.log.Warning("Failed to stop containers (may not exist yet)")
    }

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

func (d *DockerBuilder) Rollback() error {
    d.log.Warning("Rolling back Docker deployment...")

    cmd := exec.Command("docker-compose", "-f", d.composeFile, "down")
    cmd.Dir = d.workDir

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("rollback failed: %w", err)
    }

    d.log.Success("Rollback completed")
    return nil
}