package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
)

// Timeouts per operation class: quick local queries, network
// transfers, and working-tree rewrites.
const (
	queryTimeout    = 30 * time.Second
	transferTimeout = 10 * time.Minute
	checkoutTimeout = 2 * time.Minute
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

/*
LocalChanges returns the porcelain status lines for local changes to
tracked files. Untracked files are excluded — SyncToCommit leaves
them alone, so they aren't at risk.
*/
func (g *GitManager) LocalChanges() ([]string, error) {
	output, err := run.Out(queryTimeout, g.repoDir, "git", "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to check git status: %w", err)
	}

	var changes []string
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if line == "" || strings.HasPrefix(line, "??") {
			continue
		}
		changes = append(changes, line)
	}
	return changes, nil
}

// ============================================================
// Sync
// ============================================================

/*
SyncToCommit forces the checkout to exactly the given commit on the
tracked branch: fetch the branch, verify the commit exists, then
force the branch ref to it and check it out. This handles every
starting state the same way (wrong branch, diverged history, force-
pushed remote, dirty tracked files). Local changes to tracked files
are discarded; untracked files are left alone.
*/
func (g *GitManager) SyncToCommit(commit string) error {
	g.log.Infof("Syncing checkout to %s@%s...", g.branch, short(commit))

	if output, err := run.Cmd(transferTimeout, g.repoDir, "git", "fetch", "origin", g.branch); err != nil {
		return fmt.Errorf("failed to fetch origin/%s: %w\nOutput: %s", g.branch, err, output)
	}

	if _, err := run.Cmd(queryTimeout, g.repoDir, "git", "cat-file", "-e", commit+"^{commit}"); err != nil {
		return fmt.Errorf("commit %s not found after fetching origin/%s — likely removed by a force push; the push that replaced it triggers its own deploy", short(commit), g.branch)
	}

	if output, err := run.Cmd(checkoutTimeout, g.repoDir, "git", "checkout", "-f", "-B", g.branch, commit); err != nil {
		return fmt.Errorf("failed to check out commit %s: %w\nOutput: %s", short(commit), err, output)
	}

	g.log.Successf("Checkout is at %s@%s", g.branch, short(commit))
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

	output, err := run.Cmd(transferTimeout, "", "git", "clone", repoURL, g.repoDir)
	if err != nil {
		return false, fmt.Errorf("failed to clone repository: %w\nOutput: %s", err, output)
	}

	g.log.Successf("Repository cloned successfully to %s", g.repoDir)
	return true, nil
}

/* ResetHard resets the repo to a specific commit; used to roll back a failed deploy. */
func (g *GitManager) ResetHard(commit string) error {
	g.log.Warningf("Resetting repository to commit %s...", short(commit))

	output, err := run.Cmd(checkoutTimeout, g.repoDir, "git", "reset", "--hard", commit)
	if err != nil {
		return fmt.Errorf("git reset failed: %w\nOutput: %s", err, output)
	}

	g.log.Successf("Repository reset to %s", short(commit))
	return nil
}

// ============================================================
// Inspection
// ============================================================

/* GetCurrentCommit returns the current HEAD commit hash. */
func (g *GitManager) GetCurrentCommit() (string, error) {
	output, err := run.Out(queryTimeout, g.repoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get current commit: %w", err)
	}

	return strings.TrimSpace(output), nil
}

// ============================================================
// Helpers
// ============================================================

/* short abbreviates a commit hash for logging, tolerating short input. */
func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
