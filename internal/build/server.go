package build

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/pm2"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type ServerBuilder struct {
	builder      *Builder
	projectType  types.ProjectType
	appName      string
	serverEntry  string
	pm2Ecosystem string
	log          *logger.Logger
}

func NewServerBuilder(workDir string, projectType types.ProjectType, appName, serverEntry, pm2Ecosystem string, log *logger.Logger) *ServerBuilder {
	return &ServerBuilder{
		builder:      New(workDir, log),
		projectType:  projectType,
		appName:      appName,
		serverEntry:  serverEntry,
		pm2Ecosystem: pm2Ecosystem,
		log:          log,
	}
}

func (s *ServerBuilder) Build() (*types.BuildOutput, error) {
	s.log.Info("Building server application...")

	if err := s.builder.InstallDependencies(); err != nil {
		return nil, err
	}

	// For JavaScript projects, no build needed
	if s.projectType == types.ProjectTypeAPIJS {
		s.log.Info("JavaScript project - no build step required")
		return &types.BuildOutput{
			Success:   true,
			OutputDir: s.builder.workDir,
			Duration:  0,
			Error:     nil,
		}, nil
	}

	// For TypeScript projects, run build
	if s.projectType == types.ProjectTypeAPITS {
		result, err := s.builder.RunBuild()
		if err != nil {
			return result, err
		}

	
		s.log.Info("Waiting for build output to stabilize...")
		outputDir, err := s.builder.WaitForBuildCompletion(120 * time.Second)
		if err != nil {
			return nil, fmt.Errorf("build timeout: %w", err)
		}

		time.Sleep(3 * time.Second)

		// Validate main entry file exists
		mainFile := filepath.Join(outputDir, "main.js")
		if _, err := os.Stat(mainFile); os.IsNotExist(err) {
			s.log.Warning("main.js not found, checking for alternative entry points...")
			altFiles := []string{"app.js", "index.js", "server.js"}
			found := false
			for _, altFile := range altFiles {
				altPath := filepath.Join(outputDir, altFile)
				if _, err := os.Stat(altPath); err == nil {
					s.log.Infof("Found alternative entry point: %s", altFile)
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("build completed but no entry file found in %s", outputDir)
			}
		}

		result.OutputDir = outputDir
		return result, nil
	}

	return nil, fmt.Errorf("unsupported project type: %s", s.projectType)
}

// Deploy deploys the server using PM2
func (s *ServerBuilder) Deploy(workDir string) error {
	s.log.Info("Deploying server with PM2...")

	if !pm2.IsInstalled() {
		return fmt.Errorf("PM2 is not installed")
	}

	pm2Manager := pm2.New(s.appName, workDir, s.log)

	// Check if app exists
	exists, err := pm2Manager.AppExists()
	if err != nil {
		return fmt.Errorf("failed to check PM2 app: %w", err)
	}

	if exists {
		s.log.Info("Restarting existing PM2 app...")
		if err := pm2Manager.Restart(s.pm2Ecosystem); err != nil {
			return fmt.Errorf("failed to restart PM2 app: %w", err)
		}
	} else {
		s.log.Info("Starting new PM2 app...")
		if err := pm2Manager.Start(s.pm2Ecosystem); err != nil {
			return fmt.Errorf("failed to start PM2 app: %w", err)
		}
	}

	// Wait a bit for PM2 to start
	time.Sleep(4 * time.Second)

	if err := pm2Manager.EnsureRunning(s.pm2Ecosystem, 5); err != nil {
		return fmt.Errorf("PM2 app not running: %w", err)
	}

	// Save PM2 configuration
	if err := pm2Manager.Save(); err != nil {
		s.log.Warning("Failed to save PM2 configuration")
	}

	s.log.Success("Server deployed successfully with PM2")
	return nil
}