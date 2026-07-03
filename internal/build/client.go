package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

const defaultBackupParent = "/var/tmp/deployment-backups"

type ClientBuilder struct {
	builder      *Builder
	webRoot      string
	backupParent string
	log          *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/*
NewClientBuilder creates a ClientBuilder that builds workDir and
deploys to webRoot, keeping pre-deploy backups under backupDir
(falling back to the default location when empty).
*/
func NewClientBuilder(workDir, webRoot, backupDir string, log *logger.Logger) *ClientBuilder {
	if backupDir == "" {
		backupDir = defaultBackupParent
	}

	return &ClientBuilder{
		builder:      New(workDir, log),
		webRoot:      webRoot,
		backupParent: backupDir,
		log:          log,
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
build output in. Returns the backup path ("" when the web root had
nothing to back up) so the caller can restore from it if a later
step fails. A failed backup aborts the deploy before the web root is
touched — the backup is what makes the wipe safe.
*/
func (c *ClientBuilder) Deploy(buildOutput string) (string, error) {
	c.log.Info("Deploying client to web root...")

	if err := c.validateWebRoot(); err != nil {
		return "", err
	}

	backupDir, err := c.backupWebRoot()
	if err != nil {
		return "", fmt.Errorf("aborting deploy, backup of current web root failed: %w", err)
	}

	c.log.Infof("Clearing web root: %s", c.webRoot)
	if err := c.clearWebRoot(); err != nil {
		return backupDir, fmt.Errorf("failed to clear web root: %w", err)
	}

	c.log.Info("Copying files to web root...")
	if err := c.copyToWebRoot(buildOutput); err != nil {
		return backupDir, fmt.Errorf("failed to copy files to web root: %w", err)
	}

	c.log.Success("Client deployed successfully")
	return backupDir, nil
}

// ============================================================
// Backup
// ============================================================

/*
backupWebRoot copies the current web root into a timestamped backup
directory before it gets overwritten, and returns the path it
created. Returns "" if the web root doesn't exist yet or is empty —
there's nothing to back up, and nothing to restore later.
*/
func (c *ClientBuilder) backupWebRoot() (string, error) {
	entries, err := os.ReadDir(c.webRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read web root: %w", err)
	}

	if len(entries) == 0 {
		return "", nil
	}

	c.log.Info("Backing up current deployment...")

	timestamp := time.Now().Format("20060102_150405")
	backupDir := fmt.Sprintf("%s/%s_%s", c.backupParent, filepath.Base(c.webRoot), timestamp)

	if err := os.MkdirAll(c.backupParent, 0700); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	cmd := exec.Command("cp", "-r", c.webRoot, backupDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("backup copy failed: %w\nOutput: %s", err, string(output))
	}

	c.log.Successf("Backup created: %s", backupDir)

	c.cleanupOldBackups()

	return backupDir, nil
}

/* cleanupOldBackups removes old backups for this site, keeping only the last 5. */
func (c *ClientBuilder) cleanupOldBackups() {
	pattern := filepath.Base(c.webRoot) + "_*"

	matches, err := filepath.Glob(filepath.Join(c.backupParent, pattern))
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

/*
validateWebRoot refuses any web root that isn't strictly inside
/var/www/html — clearWebRoot deletes everything at this path, so it
must never be steerable elsewhere on the filesystem.
*/
func (c *ClientBuilder) validateWebRoot() error {
	cleaned := filepath.Clean(c.webRoot)
	if !strings.HasPrefix(cleaned, "/var/www/html/") {
		return fmt.Errorf("refusing to touch web root outside /var/www/html: %q", c.webRoot)
	}
	return nil
}

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

	if err := c.validateWebRoot(); err != nil {
		return err
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
