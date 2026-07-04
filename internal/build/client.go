package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

const (
	defaultBackupParent = "/var/tmp/deployment-backups"

	copyTimeout    = 10 * time.Minute
	serviceTimeout = 1 * time.Minute
)

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
		return nil, err
	}

	if err := c.builder.ValidateBuildOutput(result.OutputDir); err != nil {
		return nil, err
	}

	return result, nil
}

// ============================================================
// Deployment
// ============================================================

/*
Deploy backs up the current web root, stages the new build into a
sibling directory, then swaps it into place with renames — the old
site serves until the swap, so a failed copy never leaves a partial
or empty web root. Returns the backup path ("" when the web root
had nothing to back up, or when a failure left the old site still
serving and there is nothing to restore). A failed backup aborts
the deploy before anything else happens.
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

	c.log.Info("Staging new build next to web root...")
	staging, err := c.stage(buildOutput)
	if err != nil {
		return "", fmt.Errorf("old site still serving: %w", err)
	}

	c.log.Infof("Swapping new build into web root: %s", c.webRoot)
	if disturbed, err := c.swapIn(staging); err != nil {
		if disturbed {
			return backupDir, fmt.Errorf("web root left broken: %w", err)
		}
		return "", fmt.Errorf("old site still serving: %w", err)
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

	if output, err := run.Cmd(copyTimeout, "", "cp", "-r", c.webRoot, backupDir); err != nil {
		return "", fmt.Errorf("backup copy failed: %w\nOutput: %s", err, output)
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

// ============================================================
// Staging & swap
// ============================================================

/*
stage copies src's contents into the "<webRoot>.new" sibling
directory, replacing any leftover from a crashed run. Staging next
to the web root keeps the swap on one filesystem so the renames in
swapIn stay atomic. The web root itself is untouched.
*/
func (c *ClientBuilder) stage(src string) (string, error) {
	staging := c.webRoot + ".new"

	if err := os.RemoveAll(staging); err != nil {
		return "", fmt.Errorf("failed to remove leftover staging directory: %w", err)
	}
	if err := os.MkdirAll(staging, 0755); err != nil {
		return "", fmt.Errorf("failed to create staging directory: %w", err)
	}

	output, err := run.Cmd(copyTimeout, "", "cp", "-r", src+"/.", staging+"/")
	if err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("failed to stage files: %w\nOutput: %s", err, output)
	}

	return staging, nil
}

/*
swapIn replaces the web root with the staged directory using two
renames — current root aside to "<webRoot>.old", staging into place
— so the site never serves a partially-copied tree. If the second
rename fails, the old root is moved back. The returned disturbed
flag is true only when that recovery also failed and the web root
is actually broken, telling the caller a backup restore is needed.
*/
func (c *ClientBuilder) swapIn(staging string) (disturbed bool, err error) {
	oldDir := c.webRoot + ".old"

	if err := os.RemoveAll(oldDir); err != nil {
		os.RemoveAll(staging)
		return false, fmt.Errorf("failed to remove leftover directory %s: %w", oldDir, err)
	}

	hadPrevious := false
	if _, statErr := os.Stat(c.webRoot); statErr == nil {
		hadPrevious = true
		if err := os.Rename(c.webRoot, oldDir); err != nil {
			os.RemoveAll(staging)
			return false, fmt.Errorf("failed to move current web root aside: %w", err)
		}
	}

	if err := os.Rename(staging, c.webRoot); err != nil {
		if hadPrevious {
			if backErr := os.Rename(oldDir, c.webRoot); backErr != nil {
				return true, fmt.Errorf("swap failed and the previous version could not be moved back (%v): %w", backErr, err)
			}
		}
		os.RemoveAll(staging)
		return false, fmt.Errorf("failed to swap new build into place: %w", err)
	}

	os.RemoveAll(oldDir)
	return false, nil
}

// ============================================================
// Restore
// ============================================================

/*
RestoreFromBackup stages a prior backup and swaps it over the
(broken) web root the same way a deploy goes in, then restarts
nginx to pick up the change.
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

	staging, err := c.stage(backupDir)
	if err != nil {
		return fmt.Errorf("failed to stage backup for restore: %w", err)
	}

	if _, err := c.swapIn(staging); err != nil {
		return fmt.Errorf("failed to swap backup into place: %w", err)
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
		if _, err := run.Cmd(serviceTimeout, "", cmdArgs[0], cmdArgs[1:]...); err == nil {
			c.log.Success("Nginx restarted successfully")
			return nil
		}
	}

	c.log.Warning("Could not restart Nginx automatically")
	return nil
}
