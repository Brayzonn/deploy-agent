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

// ============================================================
// Construction
// ============================================================

/* NewServerBuilder creates a ServerBuilder for the given project type and PM2 app. */
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

// ============================================================
// Build
// ============================================================

/*
Build installs dependencies then, for TypeScript projects, compiles
and validates the output has a recognizable entry file. JavaScript
projects run directly from source and skip the compile step.
*/
func (s *ServerBuilder) Build() (*types.BuildOutput, error) {
	s.log.Info("Building server application...")

	if err := s.builder.InstallDependencies(); err != nil {
		return nil, err
	}

	if s.projectType == types.ProjectTypeAPIJS {
		s.log.Info("JavaScript project - no build step required")
		return &types.BuildOutput{OutputDir: s.builder.workDir}, nil
	}

	if s.projectType == types.ProjectTypeAPITS {
		result, err := s.builder.RunBuild()
		if err != nil {
			return nil, err
		}

		if err := s.validateEntryPoint(result.OutputDir); err != nil {
			return nil, err
		}

		return result, nil
	}

	return nil, fmt.Errorf("unsupported project type: %s", s.projectType)
}

/*
validateEntryPoint confirms the build output contains a runnable
entry file: the configured ServerEntry when set (resolved relative
to the output dir, so nest-style dist/src/main.js works), otherwise
one of the conventional names at the output root.
*/
func (s *ServerBuilder) validateEntryPoint(outputDir string) error {
	if s.serverEntry != "" {
		entry := filepath.Join(outputDir, s.serverEntry)
		if _, err := os.Stat(entry); err != nil {
			return fmt.Errorf("configured server entry not found in build output: %s", entry)
		}
		s.log.Infof("Server entry validated: %s", entry)
		return nil
	}

	for _, name := range []string{"main.js", "app.js", "index.js", "server.js"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err == nil {
			s.log.Infof("Found entry point: %s", name)
			return nil
		}
	}

	return fmt.Errorf("build completed but no entry file found in %s", outputDir)
}

// ============================================================
// Deployment
// ============================================================

/*
Deploy starts or restarts the app under PM2, waits for it to settle,
confirms it reaches the online state, and persists the PM2 process
list so it survives a server reboot.
*/
func (s *ServerBuilder) Deploy(workDir string) error {
	s.log.Info("Deploying server with PM2...")

	if !pm2.IsInstalled() {
		return fmt.Errorf("PM2 is not installed")
	}

	pm2Manager := pm2.New(s.appName, workDir, s.log)

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
		if err := pm2Manager.Start(s.pm2Ecosystem, s.serverEntry); err != nil {
			return fmt.Errorf("failed to start PM2 app: %w", err)
		}
	}

	time.Sleep(4 * time.Second)

	if err := pm2Manager.EnsureRunning(s.pm2Ecosystem, s.serverEntry, 5); err != nil {
		return fmt.Errorf("PM2 app not running: %w", err)
	}

	if err := pm2Manager.Save(); err != nil {
		s.log.Warning("Failed to save PM2 configuration")
	}

	s.log.Success("Server deployed successfully with PM2")
	return nil
}
