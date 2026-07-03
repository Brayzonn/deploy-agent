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

// ============================================================
// Construction
// ============================================================

/* New creates a GitManager for repoDir tracking the given branch. */
func New(repoDir, branch string, log *logger.Logger) *GitManager {
	return &GitManager{
		repoDir: repoDir,
		branch:  branch,
		log:     log,
	}
}

// ============================================================
// Validation
// ============================================================

/* Validate checks that repoDir exists and is a git repository. */
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

// ============================================================
// Working tree
// ============================================================

/* HasUncommittedChanges checks whether the working tree has uncommitted changes. */
func (g *GitManager) HasUncommittedChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = g.repoDir

	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check git status: %w", err)
	}

	return len(strings.TrimSpace(string(output))) > 0, nil
}

/* StashChanges stashes uncommitted changes under the given stash name. */
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

/* PopStash restores the most recently stashed changes. */
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

// ============================================================
// Sync
// ============================================================

/* Fetch fetches the latest refs from the remote without merging. */
func (g *GitManager) Fetch() error {
	g.log.Info("Fetching latest changes from GitHub...")

	cmd := exec.Command("git", "fetch")
	cmd.Dir = g.repoDir

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to fetch from GitHub: %w", err)
	}

	return nil
}

/*
CloneIfMissing clones the repo into repoDir if it isn't already
present locally. Returns true if a fresh clone happened.
*/
func (g *GitManager) CloneIfMissing(repoFullName string) (bool, error) {
	if _, err := os.Stat(g.repoDir); err == nil {
		g.log.Info("Repository already exists, skipping clone")
		return false, nil
	}

	g.log.Warning("Repository not found, cloning from GitHub...")

	repoURL := fmt.Sprintf("git@github.com:%s.git", repoFullName)
	g.log.Infof("Cloning from: %s", repoURL)

	parentDir := filepath.Dir(g.repoDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create parent directory: %w", err)
	}

	cmd := exec.Command("git", "clone", repoURL, g.repoDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to clone repository: %w\nOutput: %s", err, string(output))
	}

	g.log.Successf("Repository cloned successfully to %s", g.repoDir)
	return true, nil
}

/* CheckForUpdates reports whether the local HEAD differs from origin/<branch>. */
func (g *GitManager) CheckForUpdates() (bool, error) {
	localCmd := exec.Command("git", "rev-parse", "@")
	localCmd.Dir = g.repoDir
	localOutput, err := localCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get local HEAD: %w", err)
	}
	local := strings.TrimSpace(string(localOutput))

	remoteCmd := exec.Command("git", "rev-parse", fmt.Sprintf("origin/%s", g.branch))
	remoteCmd.Dir = g.repoDir
	remoteOutput, err := remoteCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get remote HEAD: %w", err)
	}
	remote := strings.TrimSpace(string(remoteOutput))

	return local != remote, nil
}

/* ResetHard resets the repo to a specific commit; used to roll back a failed deploy. */
func (g *GitManager) ResetHard(commit string) error {
	g.log.Warningf("Resetting repository to commit %s...", commit[:7])

	cmd := exec.Command("git", "reset", "--hard", commit)
	cmd.Dir = g.repoDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git reset failed: %w\nOutput: %s", err, string(output))
	}

	g.log.Successf("Repository reset to %s", commit[:7])
	return nil
}

/* Pull pulls the latest changes for the tracked branch from the remote. */
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

// ============================================================
// Inspection
// ============================================================

/* GetCurrentCommit returns the current HEAD commit hash. */
func (g *GitManager) GetCurrentCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = g.repoDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current commit: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
