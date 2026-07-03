package build

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
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
	var cmd *exec.Cmd

	if _, err := os.Stat(lockFile); err == nil {
		b.log.Info("Using npm ci (lock file found)...")
		cmd = exec.Command("npm", "ci", "--prefer-offline", "--no-audit")
	} else {
		b.log.Info("Using npm install (no lock file)...")
		cmd = exec.Command("npm", "install")
	}

	cmd.Dir = b.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install dependencies: %w\nOutput: %s", err, string(output))
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
RunBuild runs the npm "build" script and locates its output
directory, checking "dist" then "build" since projects vary in
which one they emit to.
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

	cmd := exec.Command("npm", "run", "build")
	cmd.Dir = b.workDir

	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	if err != nil {
		b.log.Errorf("Build failed after %v", duration)
		return &types.BuildOutput{
			Success:  false,
			Duration: duration,
			Error:    fmt.Errorf("build failed: %w\nOutput: %s", err, string(output)),
		}, err
	}

	outputDir := ""
	distPath := filepath.Join(b.workDir, "dist")
	buildPath := filepath.Join(b.workDir, "build")

	if _, err := os.Stat(distPath); err == nil {
		outputDir = distPath
	} else if _, err := os.Stat(buildPath); err == nil {
		outputDir = buildPath
	} else {
		return &types.BuildOutput{
			Success:  false,
			Duration: duration,
			Error:    fmt.Errorf("no build output directory found (dist/build missing)"),
		}, fmt.Errorf("no build output found")
	}

	b.log.Successf("Build completed in %v", duration)
	b.log.Infof("Build output: %s", outputDir)

	return &types.BuildOutput{
		Success:   true,
		OutputDir: outputDir,
		Duration:  duration,
		Error:     nil,
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

/*
WaitForBuildCompletion polls for the "dist" or "build" directory to
appear, up to timeout, on a 2s interval.
*/
func (b *Builder) WaitForBuildCompletion(timeout time.Duration) (string, error) {
	b.log.Info("Waiting for build to complete...")

	startTime := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeoutChan := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			elapsed := time.Since(startTime)

			distPath := filepath.Join(b.workDir, "dist")
			if _, err := os.Stat(distPath); err == nil {
				b.log.Successf("Build directory found after %v", elapsed)
				return distPath, nil
			}

			buildPath := filepath.Join(b.workDir, "build")
			if _, err := os.Stat(buildPath); err == nil {
				b.log.Successf("Build directory found after %v", elapsed)
				return buildPath, nil
			}

			b.log.Infof("Waiting for build to complete... (%v elapsed)", elapsed.Round(time.Second))

		case <-timeoutChan:
			return "", fmt.Errorf("build timeout: directory not created after %v", timeout)
		}
	}
}
