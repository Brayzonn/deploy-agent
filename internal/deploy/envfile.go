package deploy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

// Env files are gitignored, so a fresh clone never contains them.
// Instead of hand-placing them into the checkout after every clone,
// each repo can have an overlay directory outside any repo — mirroring
// how repos.json works — whose contents get copied into the checkout
// after every sync. The parent directory can be overridden for testing
// or non-standard installs.
const (
	defaultEnvDir = "/etc/deploy-agent/env"
	envDirEnvVar  = "DEPLOY_AGENT_ENV_DIR"
)

// ============================================================
// Env overlay staging
// ============================================================

/*
stageEnvOverlay copies every regular file under <envDir>/<repoName>/
into repoDir at the same relative path, overwriting whatever the
checkout holds; the overlay is the source of truth, same as
repos.json. A repo with no overlay directory is a no-op, so repos
that need no env files (or keep them some other way) deploy exactly
as before — the agent never fails a deploy over an *absent* env
file; apps that require one enforce that themselves at startup and
the health check turns it into a rollback. An overlay file that
exists but can't be copied does fail the deploy: continuing could
silently build against a stale copy from a previous deploy. Copies
are written 0600 since env files hold secrets. Symlinks in the
overlay are skipped — every staged file must be a plain file placed
there deliberately.
*/
func stageEnvOverlay(repoName, repoDir string, log *logger.Logger) error {
	envDir := os.Getenv(envDirEnvVar)
	if envDir == "" {
		envDir = defaultEnvDir
	}

	overlayDir := filepath.Join(envDir, repoName)
	if _, err := os.Stat(overlayDir); os.IsNotExist(err) {
		log.Infof("No env overlay at %s — using the checkout's env files if any", overlayDir)
		return nil
	}

	staged := 0
	err := filepath.WalkDir(overlayDir, func(source string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			log.Warningf("Skipping non-regular file in env overlay: %s", source)
			return nil
		}

		rel, err := filepath.Rel(overlayDir, source)
		if err != nil {
			return err
		}
		dest := filepath.Join(repoDir, rel)
		if !strings.HasPrefix(dest, filepath.Clean(repoDir)+string(filepath.Separator)) {
			return fmt.Errorf("overlay file %s would land outside the repo at %s", source, dest)
		}

		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			return err
		}
		// WriteFile only applies the mode on create; tighten a pre-existing copy too.
		if err := os.Chmod(dest, 0600); err != nil {
			return err
		}

		log.Infof("Staged env file %s -> %s", source, dest)
		staged++
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to stage env overlay %s: %w", overlayDir, err)
	}

	if staged == 0 {
		log.Infof("Env overlay %s contains no files", overlayDir)
	}
	return nil
}
