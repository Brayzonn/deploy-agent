package pm2

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)


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

func New(appName, workDir string, log *logger.Logger) *PM2Manager {
	return &PM2Manager{
		appName: appName,
		workDir: workDir,
		log:     log,
	}
}

func IsInstalled() bool {
	cmd := exec.Command("pm2", "--version")
	return cmd.Run() == nil
}

//  checks if the PM2 app exists
func (p *PM2Manager) AppExists() (bool, error) {
	cmd := exec.Command("pm2", "jlist")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get PM2 list: %w", err)
	}

	var processes []PM2Process
	if err := json.Unmarshal(output, &processes); err != nil {
		return false, fmt.Errorf("failed to parse PM2 list: %w", err)
	}

	for _, proc := range processes {
		if proc.Name == p.appName {
			return true, nil
		}
	}

	return false, nil
}

//  returns the current status of the PM2 app
func (p *PM2Manager) GetStatus() (string, error) {
	cmd := exec.Command("pm2", "jlist")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get PM2 list: %w", err)
	}

	var processes []PM2Process
	if err := json.Unmarshal(output, &processes); err != nil {
		return "", fmt.Errorf("failed to parse PM2 list: %w", err)
	}

	for _, proc := range processes {
		if proc.Name == p.appName {
			return proc.PM2Env.Status, nil
		}
	}

	return "", fmt.Errorf("app not found: %s", p.appName)
}

//  starts the PM2 app using ecosystem file
func (p *PM2Manager) Start(ecosystemFile string) error {
	p.log.Infof("Starting PM2 app '%s' with ecosystem file...", p.appName)
	
	cmd := exec.Command("pm2", "start", ecosystemFile)
	cmd.Dir = p.workDir
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start PM2 app: %w\nOutput: %s", err, string(output))
	}

	return nil
}

//  restarts the PM2 app
func (p *PM2Manager) Restart(ecosystemFile string) error {
	p.log.Infof("Restarting PM2 app '%s'...", p.appName)
	
	var cmd *exec.Cmd
	if ecosystemFile != "" {
		cmd = exec.Command("pm2", "restart", ecosystemFile)
	} else {
		cmd = exec.Command("pm2", "restart", p.appName)
	}
	cmd.Dir = p.workDir
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart PM2 app: %w\nOutput: %s", err, string(output))
	}

	return nil
}

//  deletes the PM2 app
func (p *PM2Manager) Delete() error {
	p.log.Infof("Deleting PM2 app '%s'...", p.appName)
	
	cmd := exec.Command("pm2", "delete", p.appName)
	cmd.Dir = p.workDir
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete PM2 app: %w", err)
	}

	return nil
}

//  ensures the PM2 app is running
func (p *PM2Manager) EnsureRunning(ecosystemFile string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, err := p.GetStatus()
		
		switch {
		case err != nil && strings.Contains(err.Error(), "app not found"):
			p.log.Infof("Creating new PM2 app '%s' (attempt %d/%d)...", p.appName, attempt, maxAttempts)
			if err := p.Start(ecosystemFile); err != nil {
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

// Save saves the PM2 process list
func (p *PM2Manager) Save() error {
	cmd := exec.Command("pm2", "save")
	if err := cmd.Run(); err != nil {
		p.log.Warning("Failed to save PM2 configuration")
		return err
	}
	return nil
}