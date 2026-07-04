package nginx

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

const nginxTimeout = 1 * time.Minute

type Site struct {
	domain        string
	domainAliases []string
	webRoot       string
	projectType   types.ProjectType
	port          int
	log           *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/* New creates a Site for the given domain. */
func New(domain string, domainAliases []string, webRoot string, projectType types.ProjectType, port int, log *logger.Logger) *Site {
	return &Site{
		domain:        domain,
		domainAliases: domainAliases,
		webRoot:       webRoot,
		projectType:   projectType,
		port:          port,
		log:           log,
	}
}

// ============================================================
// Config management
// ============================================================

/* availablePath returns the site's config path in sites-available. */
func (n *Site) availablePath() string {
	return fmt.Sprintf("/etc/nginx/sites-available/%s", n.domain)
}

/* enabledPath returns the site's symlink path in sites-enabled. */
func (n *Site) enabledPath() string {
	return fmt.Sprintf("/etc/nginx/sites-enabled/%s", n.domain)
}

/* ConfigExists reports whether an nginx config already exists for the domain. */
func (n *Site) ConfigExists() bool {
	_, err := os.Stat(n.availablePath())
	return err == nil
}

/*
GenerateConfig writes an nginx config for the domain, choosing a
static-file template for client projects and a reverse-proxy
template for everything else. No-ops if a config already exists.
Returns whether this call created the config, so a failed setup can
clean up exactly what it made.
*/
func (n *Site) GenerateConfig() (bool, error) {
	if n.ConfigExists() {
		n.log.Warningf("Nginx config for %s already exists — current Port/WebRoot/alias settings are NOT applied; delete %s to regenerate", n.domain, n.availablePath())
		return false, nil
	}

	n.log.Info("Generating nginx configuration...")

	var config string
	switch n.projectType {
	case types.ProjectTypeClient:
		config = n.generateClientConfig()
	case types.ProjectTypeAPIJS, types.ProjectTypeAPITS, types.ProjectTypeDocker:
		config = n.generateAPIConfig()
	default:
		return false, fmt.Errorf("unsupported project type for nginx: %s", n.projectType)
	}

	output, err := run.Stdin(nginxTimeout, "", strings.NewReader(config), "sudo", "tee", n.availablePath())
	if err != nil {
		return false, fmt.Errorf("failed to write nginx config: %w\nOutput: %s", err, output)
	}

	n.log.Successf("Nginx config created: %s", n.availablePath())
	return true, nil
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
project. Pre-existing files are never touched.
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
// Orchestration
// ============================================================

/*
Setup runs the full nginx setup sequence for a site: generate the
config, enable it, test it, then reload nginx. If the test fails,
anything this run created is removed before returning the error.
*/
func (n *Site) Setup() error {
	createdConfig, err := n.GenerateConfig()
	if err != nil {
		return err
	}

	enabledSite, err := n.EnableSite()
	if err != nil {
		n.removeCreated(createdConfig, false)
		return err
	}

	if err := n.TestConfig(); err != nil {
		if createdConfig || enabledSite {
			n.log.Warning("Removing the just-created nginx config so it can't break future deploys...")
			n.removeCreated(createdConfig, enabledSite)
		}
		return err
	}

	return n.Reload()
}
