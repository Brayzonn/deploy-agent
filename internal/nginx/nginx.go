package nginx

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

const nginxTimeout = 1 * time.Minute

/*
sitesAvailableDir and sitesEnabledDir are the nginx config directories.
They are package vars, not constants, only so tests can point the
outcome decision (which stats the on-disk config) at a temp directory
instead of root-owned /etc/nginx via SetSitesDirsForTest. Production
never changes them.
*/
var (
	sitesAvailableDir = "/etc/nginx/sites-available"
	sitesEnabledDir   = "/etc/nginx/sites-enabled"
)

type Site struct {
	domain           string
	domainAliases    []string
	webRoot          string
	projectType      types.ProjectType
	port             int
	nginxMaxUpload   int
	spaMetaProxyPath string
	backupDir        string
	log              *logger.Logger
}

/*
SetupResult reports what Setup did: the Outcome it decided on and, for a
regenerated config, the BackupPath of the pre-change file. The executor
uses BackupPath to restore the site if certbot then fails, so a live
site is never left HTTPS-down.
*/
type SetupResult struct {
	Outcome    Outcome
	BackupPath string
}

// ============================================================
// Construction
// ============================================================

/*
New creates a Site for the given domain. backupDir is where a
regenerated config's pre-change copy is stashed (the same dir the client
builder uses for web-root backups). nginxMaxUpload sets the API/Docker
server block's client_max_body_size in MB; 0 leaves nginx's own default
(1m) in place. spaMetaProxyPath, when non-empty on a client site,
switches the SPA fallback from serving index.html directly to proxying
unmatched paths to that path on localhost:port for server-side meta
injection.
*/
func New(domain string, domainAliases []string, webRoot string, projectType types.ProjectType, port int, nginxMaxUpload int, spaMetaProxyPath string, backupDir string, log *logger.Logger) *Site {
	return &Site{
		domain:           domain,
		domainAliases:    domainAliases,
		webRoot:          webRoot,
		projectType:      projectType,
		port:             port,
		nginxMaxUpload:   nginxMaxUpload,
		spaMetaProxyPath: spaMetaProxyPath,
		backupDir:        backupDir,
		log:              log,
	}
}

/*
SetSitesDirsForTest points the sites-available and sites-enabled
directories at test-controlled paths and returns a restore func to
defer. It exists so the outcome decision — which stats the on-disk
config — can be exercised without root-owned /etc/nginx; production
never calls it.
*/
func SetSitesDirsForTest(available, enabled string) func() {
	prevA, prevE := sitesAvailableDir, sitesEnabledDir
	sitesAvailableDir, sitesEnabledDir = available, enabled
	return func() { sitesAvailableDir, sitesEnabledDir = prevA, prevE }
}

// ============================================================
// Config management
// ============================================================

/* availablePath returns the site's config path in sites-available. */
func (n *Site) availablePath() string {
	return fmt.Sprintf("%s/%s", sitesAvailableDir, n.domain)
}

/* enabledPath returns the site's symlink path in sites-enabled. */
func (n *Site) enabledPath() string {
	return fmt.Sprintf("%s/%s", sitesEnabledDir, n.domain)
}

/* ConfigExists reports whether an nginx config already exists for the domain. */
func (n *Site) ConfigExists() bool {
	_, err := os.Stat(n.availablePath())
	return err == nil
}

/*
readConfig returns the current on-disk config contents via `sudo cat`.
Reading through the run package (rather than os.ReadFile) keeps the
decision path drivable from run.Stub and readable despite root
ownership.
*/
func (n *Site) readConfig() (string, error) {
	output, err := run.Cmd(nginxTimeout, "", "sudo", "cat", n.availablePath())
	if err != nil {
		return "", fmt.Errorf("failed to read nginx config %s: %w\nOutput: %s", n.availablePath(), err, output)
	}
	return output, nil
}

/*
writeConfig writes the fingerprint marker followed by the config body to
sites-available via `sudo tee`. The marker hashes only the body, so
certbot's later edits leave it intact and a future deploy can tell a
current config from a stale one.
*/
func (n *Site) writeConfig(body string) error {
	contents := markerLine(configFingerprint(body)) + "\n" + body

	output, err := run.Stdin(nginxTimeout, "", strings.NewReader(contents), "sudo", "tee", n.availablePath())
	if err != nil {
		return fmt.Errorf("failed to write nginx config: %w\nOutput: %s", err, output)
	}
	return nil
}

/*
backupConfig copies the current on-disk config to a timestamped file
under backupDir and returns its path, so a regenerate that later fails
(nginx test or certbot) can be rolled back to exactly what was serving
before.
*/
func (n *Site) backupConfig() (string, error) {
	if err := os.MkdirAll(n.backupDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s/nginx-%s-%s", n.backupDir, n.domain, timestamp)

	if output, err := run.Cmd(nginxTimeout, "", "sudo", "cp", n.availablePath(), backupPath); err != nil {
		return "", fmt.Errorf("failed to back up nginx config: %w\nOutput: %s", err, output)
	}

	n.log.Infof("Backed up existing nginx config to %s", backupPath)

	n.cleanupOldBackups()

	return backupPath, nil
}

/* cleanupOldBackups removes old nginx config backups for this domain, keeping only the last 5. */
func (n *Site) cleanupOldBackups() {
	pattern := fmt.Sprintf("nginx-%s-*", n.domain)

	matches, err := filepath.Glob(filepath.Join(n.backupDir, pattern))
	if err != nil || len(matches) <= 5 {
		return
	}

	sort.Strings(matches)

	for i := 0; i < len(matches)-5; i++ {
		os.RemoveAll(matches[i])
		n.log.Infof("Removed old nginx backup: %s", filepath.Base(matches[i]))
	}
}

// ============================================================
// Site management
// ============================================================

/*
EnableSite symlinks the site into sites-enabled, if not already
enabled. Returns whether this call created the symlink, so a failed
setup can clean up exactly what it made.
*/
func (n *Site) EnableSite() (bool, error) {
	if _, err := os.Stat(n.enabledPath()); err == nil {
		n.log.Info("Site already enabled")
		return false, nil
	}

	n.log.Info("Enabling nginx site...")

	if _, err := run.Cmd(nginxTimeout, "", "sudo", "ln", "-s", n.availablePath(), n.enabledPath()); err != nil {
		return false, fmt.Errorf("failed to enable site: %w", err)
	}

	n.log.Success("Site enabled successfully")
	return true, nil
}

/* TestConfig validates the nginx configuration via `nginx -t`. */
func (n *Site) TestConfig() error {
	n.log.Info("Testing nginx configuration...")

	output, err := run.Cmd(nginxTimeout, "", "sudo", "nginx", "-t")
	if err != nil {
		return fmt.Errorf("nginx config test failed: %w\nOutput: %s", err, output)
	}

	n.log.Success("Nginx configuration is valid")
	return nil
}

/* Reload reloads the nginx service to pick up config changes. */
func (n *Site) Reload() error {
	n.log.Info("Reloading nginx...")

	if _, err := run.Cmd(nginxTimeout, "", "sudo", "systemctl", "reload", "nginx"); err != nil {
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	n.log.Success("Nginx reloaded successfully")
	return nil
}

/*
removeCreated deletes whatever this run created (config file and/or
symlink) after a failed setup, so a broken config never stays in
sites-available/sites-enabled to poison every future deploy of every
project. Pre-existing files are never touched — only the created
outcome calls this; regenerated configs are restored from backup
instead.
*/
func (n *Site) removeCreated(createdConfig, enabledSite bool) {
	if enabledSite {
		if _, err := run.Cmd(nginxTimeout, "", "sudo", "rm", "-f", n.enabledPath()); err != nil {
			n.log.Errorf("Failed to remove broken nginx symlink %s — remove it manually or all nginx reloads will fail: %v", n.enabledPath(), err)
		} else {
			n.log.Warningf("Removed nginx symlink %s", n.enabledPath())
		}
	}

	if createdConfig {
		if _, err := run.Cmd(nginxTimeout, "", "sudo", "rm", "-f", n.availablePath()); err != nil {
			n.log.Errorf("Failed to remove broken nginx config %s — remove it manually: %v", n.availablePath(), err)
		} else {
			n.log.Warningf("Removed nginx config %s", n.availablePath())
		}
	}
}

// ============================================================
// Restore
// ============================================================

/*
RestoreConfig copies a backup file back over the live config, then
tests and reloads nginx unconditionally. The unconditional reload
covers the case where certbot already edited AND reloaded before
failing at a later step: restoring the file and reloading is correct
regardless of where in the certbot round-trip the failure happened.
*/
func (n *Site) RestoreConfig(backupPath string) error {
	n.log.Warningf("Restoring nginx config for %s from backup %s", n.domain, backupPath)

	if output, err := run.Cmd(nginxTimeout, "", "sudo", "cp", backupPath, n.availablePath()); err != nil {
		return fmt.Errorf("failed to restore nginx config from %s: %w\nOutput: %s", backupPath, err, output)
	}

	if err := n.TestConfig(); err != nil {
		return err
	}

	return n.Reload()
}

/*
restore runs RestoreConfig and downgrades a restore failure to a loud
error log — the site is already in trouble, and a caller cleaning up
after an earlier failure has nothing better to do than shout about it.
*/
func (n *Site) restore(backupPath string) {
	if err := n.RestoreConfig(backupPath); err != nil {
		n.log.Errorf("Failed to restore nginx config from backup %s — MANUAL INTERVENTION REQUIRED: %v", backupPath, err)
	}
}

// ============================================================
// Orchestration
// ============================================================

/*
Setup brings a site's nginx config to the desired state and reports the
Outcome. It decides via planAction, then dispatches:

  - created — no config yet: write, enable, test, reload; clean up
    whatever it made if the test fails.
  - unchanged — config already current: don't touch it, just ensure the
    symlink (test + reload only if the symlink had to be made).
  - regenerated — config stale or adopted: back it up, write the new
    body, enable, test, and deliberately DO NOT reload — see
    setupRegenerated.
*/
func (n *Site) Setup() (SetupResult, error) {
	want, err := n.desiredConfig()
	if err != nil {
		return SetupResult{}, err
	}

	exists := n.ConfigExists()

	existing := ""
	if exists {
		existing, err = n.readConfig()
		if err != nil {
			return SetupResult{}, err
		}
	}

	switch planAction(exists, existing, want) {
	case OutcomeCreated:
		return n.setupCreated(want)
	case OutcomeUnchanged:
		return n.setupUnchanged()
	case OutcomeRegenerated:
		return n.setupRegenerated(want, existing)
	default:
		return SetupResult{}, fmt.Errorf("unknown nginx outcome for %s", n.domain)
	}
}

/*
setupCreated is the original full setup sequence for a brand-new site:
write the config, enable it, test it, reload. If anything fails after a
write, whatever this run created is removed so a broken config can't
poison future deploys. Behavior is unchanged from before the
fingerprint work, aside from the config now carrying its marker line.
*/
func (n *Site) setupCreated(body string) (SetupResult, error) {
	res := SetupResult{Outcome: OutcomeCreated}

	n.log.Info("Generating nginx configuration...")
	if err := n.writeConfig(body); err != nil {
		return res, err
	}
	n.log.Successf("Nginx config created: %s", n.availablePath())

	enabledSite, err := n.EnableSite()
	if err != nil {
		n.removeCreated(true, false)
		return res, err
	}

	if err := n.TestConfig(); err != nil {
		n.log.Warning("Removing the just-created nginx config so it can't break future deploys...")
		n.removeCreated(true, enabledSite)
		return res, err
	}

	if err := n.Reload(); err != nil {
		return res, err
	}
	return res, nil
}

/*
setupUnchanged leaves an already-current config untouched (no write, and
no noisy "already exists" warning), but still ensures the site is
symlinked into sites-enabled. Nginx is tested and reloaded only when the
symlink actually had to be created — the reload decision tracks "did
anything change," not "did the config change." A symlink this run made
is removed if the test fails; the pre-existing config is never touched.
*/
func (n *Site) setupUnchanged() (SetupResult, error) {
	res := SetupResult{Outcome: OutcomeUnchanged}

	enabledSite, err := n.EnableSite()
	if err != nil {
		return res, err
	}

	if !enabledSite {
		return res, nil
	}

	if err := n.TestConfig(); err != nil {
		n.removeCreated(false, true)
		return res, err
	}

	if err := n.Reload(); err != nil {
		return res, err
	}
	return res, nil
}

/*
setupRegenerated replaces a stale or adopted config: back up the current
file, write the new body, ensure the symlink, and test — then
deliberately DO NOT reload. Deferred-reload design: certbot parses the
new on-disk file and does its own reload after installing SSL, so the
running nginx keeps serving the old, fully-SSL'd config until the new
one goes live WITH its 443 block in a single swap — zero HTTPS-down
window. Any failure after the backup restores it; a regenerated
(pre-existing) config is never deleted.
*/
func (n *Site) setupRegenerated(body, existing string) (SetupResult, error) {
	res := SetupResult{Outcome: OutcomeRegenerated}

	adopting := parseFingerprint(existing) == ""

	n.log.Infof("Regenerating nginx config for %s (template/settings changed)...", n.domain)

	backupPath, err := n.backupConfig()
	if err != nil {
		return res, err
	}
	res.BackupPath = backupPath

	if adopting {
		n.log.Warningf("ADOPTING a pre-existing nginx config for %s that deploy-agent did not create — original backed up to %s; manual customizations in it are NOT carried over", n.domain, backupPath)
	}

	if err := n.writeConfig(body); err != nil {
		n.restore(backupPath)
		return res, err
	}

	if _, err := n.EnableSite(); err != nil {
		n.restore(backupPath)
		return res, err
	}

	if err := n.TestConfig(); err != nil {
		n.restore(backupPath)
		return res, err
	}

	return res, nil
}
