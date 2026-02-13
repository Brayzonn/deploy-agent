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

// New creates a new Builder
func New(workDir string, log *logger.Logger) *Builder {
	return &Builder{
		workDir: workDir,
		log:     log,
	}
}

// Install Dependencies 
func (b *Builder) InstallDependencies() error {
	b.log.Info("Installing dependencies...")

	// Check if package-lock.json exists
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

// check if a package.json script exists
func (b *Builder) ScriptExists(scriptName string) (bool, error) {
    packageJSON := filepath.Join(b.workDir, "package.json")
    
    // Read the file
    data, err := os.ReadFile(packageJSON)
    if err != nil {
        return false, fmt.Errorf("failed to read package.json: %w", err)
    }

    // Parse JSON
    var pkg struct {
        Scripts map[string]string `json:"scripts"`
    }
    
    if err := json.Unmarshal(data, &pkg); err != nil {
        return false, fmt.Errorf("failed to parse package.json: %w", err)
    }

    _, exists := pkg.Scripts[scriptName]
    return exists, nil
}

// RunBuild runs the npm build script
func (b *Builder) RunBuild() (*types.BuildOutput, error) {
	b.log.Info("Building application...")
	startTime := time.Now()

	// Check if build script exists
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

	// Detect build output directory
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

//  check if the build output directory exists and is not empty
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

//  wait for the build directory to appear
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