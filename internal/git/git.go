package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

type GitManager struct {
	repoDir string
	branch  string
	log     *logger.Logger
}

func New(repoDir, branch string, log *logger.Logger) *GitManager {
	return &GitManager{
		repoDir: repoDir,
		branch:  branch,
		log:     log,
	}
}

//  check if the directory is a valid git repository
func (g *GitManager) Validate() error {
	if _, err := os.Stat(g.repoDir); os.IsNotExist(err) {
		return fmt.Errorf("repository directory does not exist: %s", g.repoDir)
	}

	gitDir := fmt.Sprintf("%s/.git", g.repoDir)
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("not a git repository: %s", g.repoDir)
	}

	return nil
}

//  check if there are uncommitted changes
func (g *GitManager) HasUncommittedChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = g.repoDir
	
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check git status: %w", err)
	}

	return len(strings.TrimSpace(string(output))) > 0, nil
}

//  stash uncommitted changes
func (g *GitManager) StashChanges(stashName string) error {
	g.log.Warning("Uncommitted changes detected. Stashing...")
	
	cmd := exec.Command("git", "stash", "push", "-m", stashName)
	cmd.Dir = g.repoDir
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stash changes: %w", err)
	}

	g.log.Success("Changes stashed successfully")
	return nil
}

//  restore stashed changes
func (g *GitManager) PopStash() error {
	g.log.Info("Restoring stashed changes...")
	
	cmd := exec.Command("git", "stash", "pop")
	cmd.Dir = g.repoDir
	
	if err := cmd.Run(); err != nil {
		g.log.Warning("Failed to restore stashed changes")
		return err
	}

	g.log.Success("Stashed changes restored")
	return nil
}

//  fetche latest changes from remote
func (g *GitManager) Fetch() error {
	g.log.Info("Fetching latest changes from GitHub...")
	
	cmd := exec.Command("git", "fetch")
	cmd.Dir = g.repoDir
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to fetch from GitHub: %w", err)
	}

	return nil
}

//  clone the repository if it doesn't exist
func (g *GitManager) CloneIfMissing(repoFullName string) error {
	if _, err := os.Stat(g.repoDir); err == nil {
		g.log.Info("Repository already exists, skipping clone")
		return nil
	}

	g.log.Warning("Repository not found, cloning from GitHub...")

	repoURL := fmt.Sprintf("https://github.com/%s.git", repoFullName)
	g.log.Infof("Cloning from: %s", repoURL)

	parentDir := filepath.Dir(g.repoDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	cmd := exec.Command("git", "clone", repoURL, g.repoDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w\nOutput: %s", err, string(output))
	}

	g.log.Successf("Repository cloned successfully to %s", g.repoDir)
	return nil
}

//  check if there are new commits to pull
func (g *GitManager) CheckForUpdates() (bool, error) {
	// Get local HEAD
	localCmd := exec.Command("git", "rev-parse", "@")
	localCmd.Dir = g.repoDir
	localOutput, err := localCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get local HEAD: %w", err)
	}
	local := strings.TrimSpace(string(localOutput))

	// Get remote HEAD
	remoteCmd := exec.Command("git", "rev-parse", fmt.Sprintf("origin/%s", g.branch))
	remoteCmd.Dir = g.repoDir
	remoteOutput, err := remoteCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get remote HEAD: %w", err)
	}
	remote := strings.TrimSpace(string(remoteOutput))

	return local != remote, nil
}

//  pull latest changes from remote
func (g *GitManager) Pull() error {
	g.log.Info("Pulling latest changes from GitHub...")
	
	cmd := exec.Command("git", "pull", "origin", g.branch)
	cmd.Dir = g.repoDir
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to pull from GitHub: %w\nOutput: %s", err, string(output))
	}

	return nil
}

//  return the current commit hash
func (g *GitManager) GetCurrentCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = g.repoDir
	
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current commit: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}