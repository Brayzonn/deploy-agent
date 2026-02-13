package types

import "time"

type ProjectType string

const (
	ProjectTypeClient ProjectType = "CLIENT"
	ProjectTypeAPIJS  ProjectType = "API_JS"
	ProjectTypeAPITS  ProjectType = "API_TS"
)

type DeploymentState string

const (
	StateStarting        DeploymentState = "STARTING"
	StateFetching        DeploymentState = "FETCHING"
	StatePulling         DeploymentState = "PULLING"
	StateDeployingServer DeploymentState = "DEPLOYING_SERVER"
	StateDeployingClient DeploymentState = "DEPLOYING_CLIENT"
	StateDeployingFull   DeploymentState = "DEPLOYING_FULLSTACK"
	StateSuccess         DeploymentState = "SUCCESS"
	StateFailed          DeploymentState = "FAILED"
)

type RepoConfig struct {
	Name          string
	RepoDir       string
	WebRoot       string
	ProjectType   ProjectType
	FullStack     bool
	ClientDir     string
	ServerDir     string
	ServerEntry   string
	PM2Ecosystem  string
}

type DeploymentContext struct {
	RepoName      string
	Branch        string
	RepoOwner     string
	Pusher        string
	Commit        string
	RepoFullName  string
	DeploymentID  string
	StartTime     time.Time
	Config        *RepoConfig
}

type BuildOutput struct {
	Success   bool
	OutputDir string
	Duration  time.Duration
	Error     error
}