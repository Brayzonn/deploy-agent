package nginx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type NginxManager struct {
	domain        string
	domainAliases []string
	webRoot       string
	projectType   types.ProjectType
	port          int 
	log           *logger.Logger
}

func New(domain string, domainAliases []string, webRoot string, projectType types.ProjectType, port int, log *logger.Logger) *NginxManager {
	return &NginxManager{
		domain:        domain,
		domainAliases: domainAliases,
		webRoot:       webRoot,
		projectType:   projectType,
		port:          port,
		log:           log,
	}
}

func (n *NginxManager) ConfigExists() bool {
	configPath := fmt.Sprintf("/etc/nginx/sites-available/%s", n.domain)
	_, err := os.Stat(configPath)
	return err == nil
}

//  creates nginx configuration file
func (n *NginxManager) GenerateConfig() error {
	if n.ConfigExists() {
		n.log.Info("Nginx config already exists, skipping generation")
		return nil
	}

	n.log.Info("Generating nginx configuration...")

	var config string
	switch n.projectType {
	case types.ProjectTypeClient:
		config = n.generateClientConfig()
	case types.ProjectTypeAPIJS, types.ProjectTypeAPITS, types.ProjectTypeDocker:
		config = n.generateAPIConfig()
	default:
		return fmt.Errorf("unsupported project type for nginx: %s", n.projectType)
	}

	configPath := fmt.Sprintf("/etc/nginx/sites-available/%s", n.domain)
	
	cmd := exec.Command("sudo", "tee", configPath)
	cmd.Stdin = strings.NewReader(config)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to write nginx config: %w\nOutput: %s", err, string(output))
	}

	n.log.Successf("Nginx config created: %s", configPath)
	return nil
}

// EnableSite enables the nginx site
func (n *NginxManager) EnableSite() error {
	sourcePath := fmt.Sprintf("/etc/nginx/sites-available/%s", n.domain)
	targetPath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", n.domain)

	if _, err := os.Stat(targetPath); err == nil {
		n.log.Info("Site already enabled")
		return nil
	}

	n.log.Info("Enabling nginx site...")

	cmd := exec.Command("sudo", "ln", "-s", sourcePath, targetPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to enable site: %w", err)
	}

	n.log.Success("Site enabled successfully")
	return nil
}

// validates nginx configuration
func (n *NginxManager) TestConfig() error {
	n.log.Info("Testing nginx configuration...")

	cmd := exec.Command("sudo", "nginx", "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx config test failed: %w\nOutput: %s", err, string(output))
	}

	n.log.Success("Nginx configuration is valid")
	return nil
}

// reloads nginx
func (n *NginxManager) Reload() error {
	n.log.Info("Reloading nginx...")

	cmd := exec.Command("sudo", "systemctl", "reload", "nginx")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	n.log.Success("Nginx reloaded successfully")
	return nil
}

// Setup 
func (n *NginxManager) Setup() error {
	if err := n.GenerateConfig(); err != nil {
		return err
	}

	if err := n.EnableSite(); err != nil {
		return err
	}

	if err := n.TestConfig(); err != nil {
		return err
	}

	if err := n.Reload(); err != nil {
		return err
	}

	return nil
}