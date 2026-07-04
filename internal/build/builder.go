package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

const (
	installTimeout = 10 * time.Minute
	buildTimeout   = 15 * time.Minute
)

type Builder struct {
	workDir string
	log     *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/* New creates a Builder rooted at workDir. */
func New(workDir string, log *logger.Logger) *Builder {
	return &Builder{
		workDir: workDir,
		log:     log,
	}
}

// ============================================================
// Dependencies
// ============================================================

/*
InstallDependencies installs npm dependencies, using `npm ci` when a
lock file is present for a reproducible install and falling back to
`npm install` otherwise.
*/
func (b *Builder) InstallDependencies() error {
	b.log.Info("Installing dependencies...")

	lockFile := filepath.Join(b.workDir, "package-lock.json")
	var args []string

	if _, err := os.Stat(lockFile); err == nil {
		b.log.Info("Using npm ci (lock file found)...")
		args = []string{"ci", "--prefer-offline", "--no-audit"}
	} else {
		b.log.Info("Using npm install (no lock file)...")
		args = []string{"install"}
	}

	output, err := run.Cmd(installTimeout, b.workDir, "npm", args...)
	if err != nil {
		return fmt.Errorf("failed to install dependencies: %w\nOutput: %s", err, output)
	}

	b.log.Success("Dependencies installed successfully")
	return nil
}

/* ScriptExists checks if a package.json script exists. */
func (b *Builder) ScriptExists(scriptName string) (bool, error) {
	packageJSON := filepath.Join(b.workDir, "package.json")

	data, err := os.ReadFile(packageJSON)
	if err != nil {
		return false, fmt.Errorf("failed to read package.json: %w", err)
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return false, fmt.Errorf("failed to parse package.json: %w", err)
	}

	_, exists := pkg.Scripts[scriptName]
	return exists, nil
}

// ============================================================
// Build
// ============================================================

/*
RunBuild removes stale build output, runs the npm "build" script,
and locates the fresh output directory, checking "dist" then "build"
since projects vary in which one they emit to. Cleaning first means
a build that silently produces nothing fails here instead of
redeploying last deploy's artifacts.
*/
func (b *Builder) RunBuild() (*types.BuildOutput, error) {
	b.log.Info("Building application...")
	startTime := time.Now()

	exists, err := b.ScriptExists("build")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("no 'build' script found in package.json")
	}

	distPath := filepath.Join(b.workDir, "dist")
	buildPath := filepath.Join(b.workDir, "build")
	for _, stale := range []string{distPath, buildPath} {
		if err := os.RemoveAll(stale); err != nil {
			return nil, fmt.Errorf("failed to clean stale build output %s: %w", stale, err)
		}
	}

	output, err := run.Cmd(buildTimeout, b.workDir, "npm", "run", "build")
	duration := time.Since(startTime)

	if err != nil {
		b.log.Errorf("Build failed after %v", duration)
		return nil, fmt.Errorf("build failed: %w\nOutput: %s", err, output)
	}

	outputDir := ""
	if _, err := os.Stat(distPath); err == nil {
		outputDir = distPath
	} else if _, err := os.Stat(buildPath); err == nil {
		outputDir = buildPath
	} else {
		return nil, fmt.Errorf("build produced no output directory (dist/build missing)")
	}

	b.log.Successf("Build completed in %v", duration)
	b.log.Infof("Build output: %s", outputDir)

	return &types.BuildOutput{
		OutputDir: outputDir,
		Duration:  duration,
	}, nil
}

/* ValidateBuildOutput checks that the build output directory exists and is not empty. */
func (b *Builder) ValidateBuildOutput(outputDir string) error {
	info, err := os.Stat(outputDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("build output directory does not exist: %s", outputDir)
	}
	if err != nil {
		return fmt.Errorf("failed to check build output: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("build output path is not a directory: %s", outputDir)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read build output directory: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("build output directory is empty: %s", outputDir)
	}

	b.log.Successf("Build output validated: %d files/directories found", len(entries))
	return nil
}
