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
repos file (or the built-in defaults when no file is installed).
Unknown repos are an error — deploys are allowlist-only.
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
loudly instead of silently doing nothing. When the env var is unset
and no file exists at the default path, the compiled-in defaults
are used so the agent keeps working before a file is installed. An
explicitly configured path that can't be read is always an error.
*/
func loadRepoConfigs() (map[string]*types.RepoConfig, error) {
	path := os.Getenv(reposFileEnvVar)
	explicit := path != ""
	if !explicit {
		path = defaultReposFile
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return builtinRepoConfigs(), nil
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

// ============================================================
// Built-in defaults
// ============================================================

/*
builtinRepoConfigs returns the compiled-in repo configs, used only
when no repos file is installed. New projects should go in the
repos file, not here.
*/
func builtinRepoConfigs() map[string]*types.RepoConfig {
	return map[string]*types.RepoConfig{
		"zoneyhub": {
			Name:        "zoneyhub",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "zoneyhub"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "zoneyhub"),
			ProjectType: types.ProjectTypeClient,
			FullStack:   false,
			ClientDir:   "client",
			ServerDir:   "server",
			ServerEntry: "app.js",
		},
		"my-music-stats": {
			Name:         "my-music-stats",
			RepoDir:      fmt.Sprintf("/home/zoney/%s", "my-music-stats"),
			WebRoot:      "/var/www/html/weeklies",
			ProjectType:  types.ProjectTypeAPITS,
			FullStack:    true,
			ClientDir:    "client",
			ServerDir:    "server",
			ServerEntry:  "src/main.js",
			PM2Ecosystem: "ecosystem.config.js",
		},
		"URL-Shortener-App": {
			Name:        "URL-Shortener-App",
			RepoDir:     fmt.Sprintf("/home/zoney/%s", "URL-Shortener-App"),
			WebRoot:     fmt.Sprintf("/var/www/html/%s", "URL-Shortener-App"),
			ProjectType: types.ProjectTypeAPIJS,
			FullStack:   false,
			ClientDir:   "client",
			ServerDir:   "server",
			ServerEntry: "app.js",
		},
		"MINES": {
			Name:                "MINES",
			RepoDir:             fmt.Sprintf("/home/zoney/%s", "MINES"),
			ProjectType:         types.ProjectTypeDocker,
			UseDocker:           true,
			DockerComposeFile:   "docker-compose.prod.yml",
			DockerEnvFile:       ".env.production",
			ServerDir:           "server",
			RequiresMigrations:  true,
			MigrationCommand:    "npm run db:migrate",
			MigrationService:    "api",
			InfraServices:       []string{"postgres", "redis"},
			Domain:              "api.mines.zoneyhub.com",
			Port:                4600,
			HealthCheckURL:      "http://localhost:4600/api/v1/health",
			HealthCheckTimeout:  30,
			ClientDir:           "client",
			ClientDomain:        "mines.zoneyhub.com",
			ClientDomainAliases: []string{},
			ClientWebRoot:       "/var/www/html/MINES",
		},
		"notifykit": {
			Name:               "notifykit",
			RepoDir:            fmt.Sprintf("/home/zoney/%s", "notifykit"),
			ProjectType:        types.ProjectTypeDocker,
			UseDocker:          true,
			DockerComposeFile:  "docker-compose.prod.yml",
			DockerEnvFile:      ".env.production",
			RequiresMigrations: true,
			MigrationCommand:   "npx prisma migrate deploy",
			MigrationService:   "api",
			InfraServices:      []string{"postgres", "redis"},
			Domain:             "api.notifykit.dev",
			Port:               5932,
			HealthCheckURL:     "http://localhost:5932/api/v1/health",
			HealthCheckTimeout: 30,
		},
		"notifykit-web": {
			Name:          "notifykit-web",
			RepoDir:       fmt.Sprintf("/home/zoney/%s", "notifykit-web"),
			WebRoot:       fmt.Sprintf("/var/www/html/%s", "notifykit-web"),
			ProjectType:   types.ProjectTypeClient,
			FullStack:     false,
			ClientDir:     "client",
			Domain:        "notifykit.dev",
			DomainAliases: []string{"www.notifykit.dev"},
		},
		"notifykit-admin": {
			Name:          "notifykit-admin",
			RepoDir:       fmt.Sprintf("/home/zoney/%s", "notifykit-admin"),
			WebRoot:       fmt.Sprintf("/var/www/html/%s", "notifykit-admin"),
			ProjectType:   types.ProjectTypeClient,
			FullStack:     false,
			ClientDir:     ".",
			Domain:        "admin.notifykit.dev",
			DomainAliases: []string{},
		},
		"NotifyKit-Docs": {
			Name:          "NotifyKit-Docs",
			RepoDir:       fmt.Sprintf("/home/zoney/%s", "NotifyKit-Docs"),
			WebRoot:       "/var/www/html/NotifyKit-Docs",
			ProjectType:   types.ProjectTypeClient,
			FullStack:     false,
			ClientDir:     ".",
			Domain:        "docs.notifykit.dev",
			DomainAliases: []string{},
		},
	}
}
