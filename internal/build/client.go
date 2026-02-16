package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type ClientBuilder struct {
	builder *Builder
	webRoot string
	log     *logger.Logger
}

func NewClientBuilder(workDir, webRoot string, log *logger.Logger) *ClientBuilder {
	return &ClientBuilder{
		builder: New(workDir, log),
		webRoot: webRoot,
		log:     log,
	}
}

//  build the client application
func (c *ClientBuilder) Build() (*types.BuildOutput, error) {
	c.log.Info("Building client application...")

	if err := c.builder.InstallDependencies(); err != nil {
		return nil, err
	}

	result, err := c.builder.RunBuild()
	if err != nil {
		return result, err
	}

	if err := c.builder.ValidateBuildOutput(result.OutputDir); err != nil {
		return result, err
	}

	return result, nil
}

//  deploys the built client to the web root and returns backup path
func (c *ClientBuilder) Deploy(buildOutput string) (string, error) {
	c.log.Info("Deploying client to web root...")

	// Validate web root path 
	if c.webRoot == "" || c.webRoot == "/" || c.webRoot == "/home" {
		return "", fmt.Errorf("refusing to deploy: webRoot is set to a dangerous value: '%s'", c.webRoot)
	}

	// Create backup and capture the path
	timestamp := time.Now().Format("20060102_150405")
	backupDir := fmt.Sprintf("/var/tmp/deployment-backups/%s_%s", 
		filepath.Base(c.webRoot), timestamp)

	if err := c.backupWebRoot(); err != nil {
		c.log.Warningf("Backup failed: %v", err)
		backupDir = "" 
	}

	c.log.Infof("Clearing web root: %s", c.webRoot)
	if err := c.clearWebRoot(); err != nil {
		return "", fmt.Errorf("failed to clear web root: %w", err)
	}

	c.log.Info("Copying files to web root...")
	if err := c.copyToWebRoot(buildOutput); err != nil {
		return "", fmt.Errorf("failed to copy files to web root: %w", err)
	}

	c.log.Success("Client deployed successfully")
	return backupDir, nil  
}

//  create a backup of the current web root
func (c *ClientBuilder) backupWebRoot() error {
	entries, err := os.ReadDir(c.webRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read web root: %w", err)
	}

	if len(entries) == 0 {
	
		return nil
	}

	c.log.Info("Backing up current deployment...")

	// Create backup directory with timestamp
	timestamp := time.Now().Format("20060102_150405")
	backupDir := fmt.Sprintf("/var/tmp/deployment-backups/%s_%s", 
		filepath.Base(c.webRoot), timestamp)

	// Create backup parent directory
	if err := os.MkdirAll(filepath.Dir(backupDir), 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Copy web root to backup directory
	cmd := exec.Command("cp", "-r", c.webRoot, backupDir)
	if err := cmd.Run(); err != nil {
		c.log.Warningf("Backup failed: %v", err)
		return err
	}

	c.log.Successf("Backup created: %s", backupDir)

	c.cleanupOldBackups()

	return nil
}

//  removes old backups, keeping only the last 5
func (c *ClientBuilder) cleanupOldBackups() {
	backupParent := "/var/tmp/deployment-backups"
	pattern := filepath.Base(c.webRoot) + "_*"

	matches, err := filepath.Glob(filepath.Join(backupParent, pattern))
	if err != nil || len(matches) <= 5 {
		return
	}

	sort.Strings(matches)

	// Remove oldest backups, keep last 5
	for i := 0; i < len(matches)-5; i++ {
		os.RemoveAll(matches[i])
		c.log.Infof("Removed old backup: %s", filepath.Base(matches[i]))
	}
}

// clearWebRoot removes all files from the web root
func (c *ClientBuilder) clearWebRoot() error {
	if err := os.MkdirAll(c.webRoot, 0755); err != nil {
		return fmt.Errorf("failed to create web root: %w", err)
	}

	entries, err := os.ReadDir(c.webRoot)
	if err != nil {
		return fmt.Errorf("failed to read web root: %w", err)
	}

	for _, entry := range entries {
		path := filepath.Join(c.webRoot, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}

	return nil
}

//  copy files from build output to web root
func (c *ClientBuilder) copyToWebRoot(buildOutput string) error {
	cmd := exec.Command("cp", "-r", buildOutput+"/.", c.webRoot+"/")
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

//  restores the previous deployment from backup
func (c *ClientBuilder) RestoreFromBackup(backupDir string) error {
	if backupDir == "" {
		return fmt.Errorf("no backup directory provided")
	}

	c.log.Warning("Restoring previous deployment from backup...")

	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return fmt.Errorf("backup directory does not exist: %s", backupDir)
	}

	c.log.Info("Clearing broken deployment...")
	if err := c.clearWebRoot(); err != nil {
		return fmt.Errorf("failed to clear web root: %w", err)
	}

	c.log.Info("Copying backup files to web root...")
	cmd := exec.Command("cp", "-r", backupDir+"/.", c.webRoot+"/")
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restore backup: %w\nOutput: %s", err, string(output))
	}

	c.RestartNginx()

	c.log.Success("Previous deployment restored successfully")
	return nil
}

//  restart the Nginx server
func (c *ClientBuilder) RestartNginx() error {
	c.log.Info("Restarting Nginx...")

	commands := [][]string{
		{"systemctl", "--user", "restart", "nginx"},
		{"systemctl", "restart", "nginx"},
		{"service", "nginx", "restart"},
	}

	for _, cmdArgs := range commands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		if err := cmd.Run(); err == nil {
			c.log.Success("Nginx restarted successfully")
			return nil
		}
	}

	c.log.Warning("Could not restart Nginx automatically")
	return nil
}