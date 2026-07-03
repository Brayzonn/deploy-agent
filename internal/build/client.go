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

// ============================================================
// Construction
// ============================================================

/* NewClientBuilder creates a ClientBuilder that builds workDir and deploys to webRoot. */
func NewClientBuilder(workDir, webRoot string, log *logger.Logger) *ClientBuilder {
	return &ClientBuilder{
		builder: New(workDir, log),
		webRoot: webRoot,
		log:     log,
	}
}

// ============================================================
// Build
// ============================================================

/* Build installs dependencies, runs the build, and validates the output. */
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

// ============================================================
// Deployment
// ============================================================

/*
Deploy backs up the current web root, clears it, and copies the new
build output in. Returns the backup path (or "" if the backup step
failed) so the caller can restore from it if a later step fails.
*/
func (c *ClientBuilder) Deploy(buildOutput string) (string, error) {
	c.log.Info("Deploying client to web root...")

	if c.webRoot == "" || c.webRoot == "/" || c.webRoot == "/home" {
		return "", fmt.Errorf("refusing to deploy: webRoot is set to a dangerous value: '%s'", c.webRoot)
	}

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

// ============================================================
// Backup
// ============================================================

/*
backupWebRoot copies the current web root into a timestamped backup
directory before it gets overwritten. No-ops if the web root doesn't
exist yet or is empty.
*/
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

	timestamp := time.Now().Format("20060102_150405")
	backupDir := fmt.Sprintf("/var/tmp/deployment-backups/%s_%s",
		filepath.Base(c.webRoot), timestamp)

	if err := os.MkdirAll(filepath.Dir(backupDir), 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	cmd := exec.Command("cp", "-r", c.webRoot, backupDir)
	if err := cmd.Run(); err != nil {
		c.log.Warningf("Backup failed: %v", err)
		return err
	}

	c.log.Successf("Backup created: %s", backupDir)

	c.cleanupOldBackups()

	return nil
}

/* cleanupOldBackups removes old backups for this site, keeping only the last 5. */
func (c *ClientBuilder) cleanupOldBackups() {
	backupParent := "/var/tmp/deployment-backups"
	pattern := filepath.Base(c.webRoot) + "_*"

	matches, err := filepath.Glob(filepath.Join(backupParent, pattern))
	if err != nil || len(matches) <= 5 {
		return
	}

	sort.Strings(matches)

	for i := 0; i < len(matches)-5; i++ {
		os.RemoveAll(matches[i])
		c.log.Infof("Removed old backup: %s", filepath.Base(matches[i]))
	}
}

// ============================================================
// Web root
// ============================================================

/* clearWebRoot removes all files from the web root, creating it first if needed. */
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

/* copyToWebRoot copies files from the build output into the web root. */
func (c *ClientBuilder) copyToWebRoot(buildOutput string) error {
	cmd := exec.Command("cp", "-r", buildOutput+"/.", c.webRoot+"/")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// ============================================================
// Restore
// ============================================================

/*
RestoreFromBackup clears the (broken) web root and copies a prior
backup back into place, then restarts nginx to pick up the change.
*/
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

// ============================================================
// Nginx
// ============================================================

/*
RestartNginx restarts the Nginx service, trying user-level systemd,
system-level systemd, then the sysvinit service command in turn
until one succeeds.
*/
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
