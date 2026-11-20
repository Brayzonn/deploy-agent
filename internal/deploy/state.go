package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Per-repo commit records kept next to the deploy locks in StateDir.
// last-good is written only after a deploy succeeds, so it always
// names what production should be running; the skip check and the
// rollback target come from it instead of from wherever git happens
// to be. last-attempted is written just before a deploy starts, so a
// commit that already failed can be told apart from one that never ran.
const (
	lastGoodPrefix      = "last-good-"
	lastAttemptedPrefix = "last-attempted-"
)

// ============================================================
// Commit records
// ============================================================

/* readLastGood returns the last successfully deployed commit, or "" if none was saved yet. */
func readLastGood(stateDir, repo string) (string, error) {
	return readCommit(filepath.Join(stateDir, lastGoodPrefix+repo))
}

/* writeLastGood records commit as the last successful deploy. */
func writeLastGood(stateDir, repo, commit string) error {
	return writeCommit(filepath.Join(stateDir, lastGoodPrefix+repo), commit)
}

/* readLastAttempted returns the last commit a deploy started on, or "" if none was saved yet. */
func readLastAttempted(stateDir, repo string) (string, error) {
	return readCommit(filepath.Join(stateDir, lastAttemptedPrefix+repo))
}

/* writeLastAttempted records commit as the last commit a deploy started on. */
func writeLastAttempted(stateDir, repo, commit string) error {
	return writeCommit(filepath.Join(stateDir, lastAttemptedPrefix+repo), commit)
}

// ============================================================
// Helpers
// ============================================================

/* readCommit reads a commit record; a missing file is not an error and reads as "". */
func readCommit(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

/*
writeCommit writes the record via a temp file in the same directory
and a rename, so a crash mid-write can never leave a half-written
hash behind — readers see either the old commit or the new one.
*/
func writeCommit(path, commit string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(commit + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to sync %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("failed to save %s: %w", path, err)
	}
	return nil
}
