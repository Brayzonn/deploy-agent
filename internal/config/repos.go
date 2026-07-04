package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// Repo configs load from a JSON file so adding or fixing a project
// doesn't require recompiling and shipping a new agent binary. The
// path can be overridden for testing or non-standard installs.
const (
	defaultReposFile = "/etc/deploy-agent/repos.json"
	reposFileEnvVar  = "DEPLOY_AGENT_REPOS_FILE"
	webRootParent    = "/var/www/html/"
)

// ============================================================
// Lookup
// ============================================================

/*
GetRepoConfig resolves the deployment config for a repo from the
repos file. Unknown repos are an error — deploys are allowlist-only.
*/
func GetRepoConfig(repoName, repoOwner string) (*types.RepoConfig, error) {
	configs, err := loadRepoConfigs()
	if err != nil {
		return nil, err
	}

	config, exists := configs[repoName]
	if !exists {
		return nil, fmt.Errorf("repository %q is not in the deployment allowlist", repoName)
	}

	if err := validateRepoConfig(repoName, config); err != nil {
		return nil, err
	}

	return config, nil
}

// ============================================================
// Loading
// ============================================================

/*
loadRepoConfigs reads the repos file — a JSON object keyed by repo
name — rejecting unknown fields so a typo'd or removed key fails
loudly instead of silently doing nothing. The file is the only
source of repo configs: a missing file fails the deploy with
install instructions rather than falling back to anything baked
into the binary.
*/
func loadRepoConfigs() (map[string]*types.RepoConfig, error) {
	path := os.Getenv(reposFileEnvVar)
	if path == "" {
		path = defaultReposFile
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("repos file %s does not exist — install one (see repos.example.json in the deploy-agent repo)", path)
		}
		return nil, fmt.Errorf("failed to read repos file %s: %w", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var configs map[string]*types.RepoConfig
	if err := decoder.Decode(&configs); err != nil {
		return nil, fmt.Errorf("failed to parse repos file %s: %w", path, err)
	}

	return configs, nil
}

// ============================================================
// Validation
// ============================================================

/*
validateRepoConfig checks the shape of a repo config before it can
steer filesystem paths: RepoDir must be absolute and traversal-free,
web roots must live under the served parent (the same guard the
client deployer enforces), and the project type must be one the
executor can route. Name and DeployBranch get their defaults here.
*/
func validateRepoConfig(repoName string, config *types.RepoConfig) error {
	if config == nil {
		return fmt.Errorf("repo %q: config is null", repoName)
	}

	if config.Name == "" {
		config.Name = repoName
	}
	if config.Name != repoName {
		return fmt.Errorf("repo %q: name field %q does not match its key", repoName, config.Name)
	}
	if config.DeployBranch == "" {
		config.DeployBranch = "main"
	}

	if !filepath.IsAbs(config.RepoDir) || strings.Contains(config.RepoDir, "..") {
		return fmt.Errorf("repo %q: repo_dir must be an absolute path without traversal, got %q", repoName, config.RepoDir)
	}

	for field, webRoot := range map[string]string{"web_root": config.WebRoot, "client_web_root": config.ClientWebRoot} {
		if webRoot == "" {
			continue
		}
		cleaned := filepath.Clean(webRoot)
		if !strings.HasPrefix(cleaned, webRootParent) || cleaned == filepath.Clean(webRootParent) {
			return fmt.Errorf("repo %q: %s must be inside %s, got %q", repoName, field, webRootParent, webRoot)
		}
	}

	switch config.ProjectType {
	case types.ProjectTypeClient, types.ProjectTypeAPIJS, types.ProjectTypeAPITS, types.ProjectTypeDocker:
	default:
		return fmt.Errorf("repo %q: unknown project_type %q", repoName, config.ProjectType)
	}

	return nil
}
