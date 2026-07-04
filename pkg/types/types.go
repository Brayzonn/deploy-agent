package types

import "time"

type ProjectType string

const (
	ProjectTypeClient ProjectType = "CLIENT"
	ProjectTypeAPIJS  ProjectType = "API_JS"
	ProjectTypeAPITS  ProjectType = "API_TS"
	ProjectTypeDocker ProjectType = "DOCKER"
)

type DeploymentState string

const (
	StateStarting          DeploymentState = "STARTING"
	StateSyncing           DeploymentState = "SYNCING"
	StateDeployingServer   DeploymentState = "DEPLOYING_SERVER"
	StateDeployingClient   DeploymentState = "DEPLOYING_CLIENT"
	StateDeployingFull     DeploymentState = "DEPLOYING_FULLSTACK"
	StateBuildingDocker    DeploymentState = "BUILDING_DOCKER"
	StateDeployingDocker   DeploymentState = "DEPLOYING_DOCKER"
	StateRunningMigrations DeploymentState = "RUNNING_MIGRATIONS"
	StateSuccess           DeploymentState = "SUCCESS"
	StateFailed            DeploymentState = "FAILED"
)

/*
RepoConfig describes how one repository deploys. Configs load from
the repos JSON file when present (keyed by repo name, snake_case
fields per the json tags below), falling back to the compiled-in
defaults — see internal/config/repos.go.
*/
type RepoConfig struct {
	Name          string      `json:"name,omitempty"`
	DeployBranch  string      `json:"deploy_branch,omitempty"`
	RepoDir       string      `json:"repo_dir,omitempty"`
	WebRoot       string      `json:"web_root,omitempty"`
	ProjectType   ProjectType `json:"project_type,omitempty"`
	FullStack     bool        `json:"full_stack,omitempty"`
	ClientDir     string      `json:"client_dir,omitempty"`
	ServerDir     string      `json:"server_dir,omitempty"`
	ServerEntry   string      `json:"server_entry,omitempty"`
	PM2Ecosystem  string      `json:"pm2_ecosystem,omitempty"`
	Domain        string      `json:"domain,omitempty"`
	DomainAliases []string    `json:"domain_aliases,omitempty"`
	ServerDomain  string      `json:"server_domain,omitempty"` // fullstack API domain; defaults to "api." + Domain
	Port          int         `json:"port,omitempty"`

	UseDocker          bool     `json:"use_docker,omitempty"`
	DockerComposeFile  string   `json:"docker_compose_file,omitempty"`
	DockerEnvFile      string   `json:"docker_env_file,omitempty"`
	RequiresMigrations bool     `json:"requires_migrations,omitempty"`
	MigrationCommand   string   `json:"migration_command,omitempty"`
	MigrationService   string   `json:"migration_service,omitempty"`
	InfraServices      []string `json:"infra_services,omitempty"`
	HealthCheckURL     string   `json:"health_check_url,omitempty"`
	HealthCheckTimeout int      `json:"health_check_timeout,omitempty"`

	ClientDomain        string   `json:"client_domain,omitempty"`
	ClientDomainAliases []string `json:"client_domain_aliases,omitempty"`
	ClientWebRoot       string   `json:"client_web_root,omitempty"`
}

type DeploymentContext struct {
	RepoName     string
	Branch       string
	RepoOwner    string
	Pusher       string
	Commit       string
	RepoFullName string
	DeploymentID string
	StartTime    time.Time
	Config       *RepoConfig
}

/*
BuildOutput reports where a successful build landed and how long it
took. Failures travel in the returned error, not here.
*/
type BuildOutput struct {
	OutputDir string
	Duration  time.Duration
}
