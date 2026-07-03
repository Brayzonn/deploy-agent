package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/pkg/types"
)

var (
	namePattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

type Config struct {
	LogDir          string
	StateDir        string
	BackupDir       string
	VerboseLogDir   string
	SlackWebhookURL string
	SSLEmail        string
}

// ============================================================
// Construction
// ============================================================

/*
LoadConfig builds the agent-wide configuration from environment
variables and fixed filesystem locations.
*/
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

// ============================================================
// Validation
// ============================================================

/*
ValidateEnvironment checks that the webhook-supplied environment
variables required for a deployment are all present, then resolves
the matching repo config and assembles the deployment context.
*/
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

	if err := validateWebhookValues(required); err != nil {
		return nil, err
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

/*
validateWebhookValues rejects webhook-supplied values that could
steer filesystem paths or be parsed as git flags: path traversal,
path separators in names, leading dashes, and malformed commit
hashes. These values feed directly into RepoDir/WebRoot paths and
git command arguments, so they must be strictly shaped.
*/
func validateWebhookValues(required map[string]string) error {
	for _, key := range []string{"GITHUB_REPO_NAME", "GITHUB_REPO_OWNER", "GITHUB_PUSHER"} {
		value := required[key]
		if !namePattern.MatchString(value) || strings.Contains(value, "..") {
			return fmt.Errorf("%s contains unsafe characters: %q", key, value)
		}
	}

	branch := required["GITHUB_BRANCH"]
	if !branchPattern.MatchString(branch) || strings.Contains(branch, "..") {
		return fmt.Errorf("GITHUB_BRANCH contains unsafe characters: %q", branch)
	}

	if commit := required["GITHUB_COMMIT"]; !commitPattern.MatchString(commit) {
		return fmt.Errorf("GITHUB_COMMIT is not a full 40-character commit hash: %q", commit)
	}

	expectedFullName := required["GITHUB_REPO_OWNER"] + "/" + required["GITHUB_REPO_NAME"]
	if required["GITHUB_REPO_FULL_NAME"] != expectedFullName {
		return fmt.Errorf("GITHUB_REPO_FULL_NAME %q does not match owner/name %q",
			required["GITHUB_REPO_FULL_NAME"], expectedFullName)
	}

	return nil
}

// ============================================================
// Setup
// ============================================================

/* EnsureDirectories creates all directories the agent writes to, if missing. */
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
