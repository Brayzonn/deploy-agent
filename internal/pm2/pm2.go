package pm2

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
)

const pm2Timeout = 1 * time.Minute

type PM2Manager struct {
	appName string
	workDir string
	log     *logger.Logger
}

type PM2Process struct {
	Name   string `json:"name"`
	PM2Env struct {
		Status string `json:"status"`
	} `json:"pm2_env"`
}

// ============================================================
// Construction
// ============================================================

/* New creates a PM2Manager for the given app name and working directory. */
func New(appName, workDir string, log *logger.Logger) *PM2Manager {
	return &PM2Manager{
		appName: appName,
		workDir: workDir,
		log:     log,
	}
}

/* IsInstalled reports whether the pm2 CLI is available on PATH. */
func IsInstalled() bool {
	_, err := run.Cmd(pm2Timeout, "", "pm2", "--version")
	return err == nil
}

// ============================================================
// Status
// ============================================================

/* AppExists checks whether the PM2 app is present in `pm2 jlist`. */
func (p *PM2Manager) AppExists() (bool, error) {
	output, err := run.Out(pm2Timeout, "", "pm2", "jlist")
	if err != nil {
		return false, fmt.Errorf("failed to get PM2 list: %w", err)
	}

	var processes []PM2Process
	if err := json.Unmarshal([]byte(output), &processes); err != nil {
		return false, fmt.Errorf("failed to parse PM2 list: %w", err)
	}

	for _, proc := range processes {
		if proc.Name == p.appName {
			return true, nil
		}
	}

	return false, nil
}

/* GetStatus returns the current PM2 status string for the app (e.g. "online"). */
func (p *PM2Manager) GetStatus() (string, error) {
	output, err := run.Out(pm2Timeout, "", "pm2", "jlist")
	if err != nil {
		return "", fmt.Errorf("failed to get PM2 list: %w", err)
	}

	var processes []PM2Process
	if err := json.Unmarshal([]byte(output), &processes); err != nil {
		return "", fmt.Errorf("failed to parse PM2 list: %w", err)
	}

	for _, proc := range processes {
		if proc.Name == p.appName {
			return proc.PM2Env.Status, nil
		}
	}

	return "", fmt.Errorf("app not found: %s", p.appName)
}

// ============================================================
// Process management
// ============================================================

/*
Start starts the PM2 app from the ecosystem file when one is
configured, otherwise from the server entry file under the app's
name. Errors when neither is configured — `pm2 start ""` can never
succeed, so failing with the reason beats retrying it.
*/
func (p *PM2Manager) Start(ecosystemFile, serverEntry string) error {
	var args []string
	switch {
	case ecosystemFile != "":
		p.log.Infof("Starting PM2 app '%s' with ecosystem file %s...", p.appName, ecosystemFile)
		args = []string{"start", ecosystemFile}
	case serverEntry != "":
		p.log.Infof("Starting PM2 app '%s' from entry file %s...", p.appName, serverEntry)
		args = []string{"start", serverEntry, "--name", p.appName}
	default:
		return fmt.Errorf("cannot start PM2 app '%s': neither PM2Ecosystem nor ServerEntry is configured", p.appName)
	}

	output, err := run.Cmd(pm2Timeout, p.workDir, "pm2", args...)
	if err != nil {
		return fmt.Errorf("failed to start PM2 app: %w\nOutput: %s", err, output)
	}

	return nil
}

/*
Restart restarts the PM2 app, preferring the ecosystem file when one
is given and falling back to restarting by app name.
*/
func (p *PM2Manager) Restart(ecosystemFile string) error {
	p.log.Infof("Restarting PM2 app '%s'...", p.appName)

	target := p.appName
	if ecosystemFile != "" {
		target = ecosystemFile
	}

	output, err := run.Cmd(pm2Timeout, p.workDir, "pm2", "restart", target)
	if err != nil {
		return fmt.Errorf("failed to restart PM2 app: %w\nOutput: %s", err, output)
	}

	return nil
}

/* Delete deletes the PM2 app. */
func (p *PM2Manager) Delete() error {
	p.log.Infof("Deleting PM2 app '%s'...", p.appName)

	if _, err := run.Cmd(pm2Timeout, p.workDir, "pm2", "delete", p.appName); err != nil {
		return fmt.Errorf("failed to delete PM2 app: %w", err)
	}

	return nil
}

/*
EnsureRunning polls the app's status and nudges it toward "online":
starting it if missing, restarting it if stopped, and deleting and
recreating it if errored. Gives up after maxAttempts.
*/
func (p *PM2Manager) EnsureRunning(ecosystemFile, serverEntry string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, err := p.GetStatus()

		switch {
		case err != nil && strings.Contains(err.Error(), "app not found"):
			p.log.Infof("Creating new PM2 app '%s' (attempt %d/%d)...", p.appName, attempt, maxAttempts)
			if err := p.Start(ecosystemFile, serverEntry); err != nil {
				p.log.Warningf("Failed to start PM2 app: %v", err)
			}

		case status == "online":
			p.log.Successf("PM2 app '%s' is running", p.appName)
			return nil

		case status == "stopped" || status == "stopping":
			p.log.Infof("PM2 app '%s' is stopped, restarting...", p.appName)
			if err := p.Restart(""); err != nil {
				p.log.Warningf("Failed to restart PM2 app: %v", err)
			}

		case status == "errored":
			p.log.Warning("PM2 app has errored, deleting and recreating...")
			p.Delete()
		}

		time.Sleep(3 * time.Second)
	}

	status, err := p.GetStatus()
	if err != nil {
		return fmt.Errorf("PM2 app not running after %d attempts: %w", maxAttempts, err)
	}

	if status != "online" {
		return fmt.Errorf("PM2 app status is '%s' after %d attempts", status, maxAttempts)
	}

	return nil
}

// ============================================================
// Persistence
// ============================================================

/* Save persists the current PM2 process list so it survives a reboot. */
func (p *PM2Manager) Save() error {
	if _, err := run.Cmd(pm2Timeout, "", "pm2", "save"); err != nil {
		p.log.Warning("Failed to save PM2 configuration")
		return err
	}
	return nil
}
