package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type Config struct {
	LogDir          string
	StateDir        string
	BackupDir       string
	VerboseLogDir   string
	SlackWebhookURL string
	SSLEmail        string  
}

func LoadConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	
	return &Config{
		LogDir:          filepath.Join(homeDir, "logs"),
		StateDir:        "/var/tmp/deployment-states",
		BackupDir:       "/var/tmp/deployment-backups",
		VerboseLogDir:   filepath.Join(homeDir, "logs", "deployments"),
		SlackWebhookURL: os.Getenv("SLACK_WEBHOOK_URL"),
		SSLEmail:        os.Getenv("SSL_EMAIL"),
	}
}

func ValidateEnvironment() (*types.DeploymentContext, error) {
	required := map[string]string{
		"GITHUB_REPO_NAME":      os.Getenv("GITHUB_REPO_NAME"),
		"GITHUB_BRANCH":         os.Getenv("GITHUB_BRANCH"),
		"GITHUB_REPO_OWNER":     os.Getenv("GITHUB_REPO_OWNER"),
		"GITHUB_PUSHER":         os.Getenv("GITHUB_PUSHER"),
		"GITHUB_COMMIT":         os.Getenv("GITHUB_COMMIT"),
		"GITHUB_REPO_FULL_NAME": os.Getenv("GITHUB_REPO_FULL_NAME"),
	}

	for key, value := range required {
		if value == "" {
			return nil, fmt.Errorf("%s is not set", key)
		}
	}

	deploymentID := fmt.Sprintf("%s_%d", time.Now().Format("20060102_150405"), os.Getpid())

	repoConfig, err := GetRepoConfig(required["GITHUB_REPO_NAME"], required["GITHUB_REPO_OWNER"])
	if err != nil {
		return nil, fmt.Errorf("failed to get repo config: %w", err)
	}

	return &types.DeploymentContext{
		RepoName:     required["GITHUB_REPO_NAME"],
		Branch:       required["GITHUB_BRANCH"],
		RepoOwner:    required["GITHUB_REPO_OWNER"],
		Pusher:       required["GITHUB_PUSHER"],
		Commit:       required["GITHUB_COMMIT"],
		RepoFullName: required["GITHUB_REPO_FULL_NAME"],
		DeploymentID: deploymentID,
		StartTime:    time.Now(),
		Config:       repoConfig,
	}, nil
}

// create all required directories
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		c.LogDir,
		c.StateDir,
		c.BackupDir,
		c.VerboseLogDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}