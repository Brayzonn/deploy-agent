package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

// ============================================================
// Helpers
// ============================================================

/* stagingLogger builds a throwaway logger writing under a temp dir. */
func stagingLogger(t *testing.T) *logger.Logger {
	t.Helper()

	log, err := logger.New("test", filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatalf("could not create logger: %v", err)
	}
	return log
}

/* writeOverlay builds an env overlay for repoName from relative-path → content pairs and points the agent at it. */
func writeOverlay(t *testing.T, repoName string, files map[string]string) string {
	t.Helper()

	envDir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(envDir, repoName, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatalf("could not create overlay dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("could not write overlay file: %v", err)
		}
	}
	t.Setenv(envDirEnvVar, envDir)
	return envDir
}

// ============================================================
// Tests
// ============================================================

/* TestStageEnvOverlayCopiesTree checks overlay files land at their relative paths, overwriting and restricting perms. */
func TestStageEnvOverlayCopiesTree(t *testing.T) {
	writeOverlay(t, "my-app", map[string]string{
		".env.production": "SECRET=fresh",
		"server/.env":     "API_KEY=abc",
	})

	repoDir := t.TempDir()
	rootDest := filepath.Join(repoDir, ".env.production")
	// A stale, over-permissive copy from a previous deploy must be replaced and tightened.
	if err := os.WriteFile(rootDest, []byte("SECRET=stale"), 0644); err != nil {
		t.Fatalf("could not write pre-existing env file: %v", err)
	}

	if err := stageEnvOverlay("my-app", repoDir, stagingLogger(t)); err != nil {
		t.Fatalf("staging failed: %v", err)
	}

	for dest, want := range map[string]string{
		rootDest:                                 "SECRET=fresh",
		filepath.Join(repoDir, "server", ".env"): "API_KEY=abc",
	} {
		data, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("could not read staged file %s: %v", dest, err)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want overlay copy %q", dest, data, want)
		}
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatalf("could not stat staged file %s: %v", dest, err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("%s mode = %o, want 0600", dest, perm)
		}
	}
}

/* TestStageEnvOverlayMissingIsNoOp checks a repo without an overlay directory deploys with the checkout untouched. */
func TestStageEnvOverlayMissingIsNoOp(t *testing.T) {
	t.Setenv(envDirEnvVar, t.TempDir())

	repoDir := t.TempDir()
	dest := filepath.Join(repoDir, ".env.production")
	if err := os.WriteFile(dest, []byte("SECRET=checkout"), 0600); err != nil {
		t.Fatalf("could not write checkout env file: %v", err)
	}

	if err := stageEnvOverlay("my-app", repoDir, stagingLogger(t)); err != nil {
		t.Fatalf("missing overlay should be a no-op, got: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("could not read env file: %v", err)
	}
	if string(data) != "SECRET=checkout" {
		t.Errorf("checkout copy was modified to %q, want it untouched", data)
	}
}

/* TestStageEnvOverlayUnreadableFileFails checks an overlay file that exists but can't be read fails the deploy. */
func TestStageEnvOverlayUnreadableFileFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores file permissions")
	}

	envDir := writeOverlay(t, "my-app", map[string]string{".env.production": "SECRET=locked"})
	source := filepath.Join(envDir, "my-app", ".env.production")
	if err := os.Chmod(source, 0000); err != nil {
		t.Fatalf("could not chmod overlay file: %v", err)
	}

	if err := stageEnvOverlay("my-app", t.TempDir(), stagingLogger(t)); err == nil {
		t.Error("unreadable overlay file was accepted, want error — deploying could use a stale env file")
	}
}

/* TestStageEnvOverlaySkipsSymlinks checks symlinks in the overlay are not followed or copied. */
func TestStageEnvOverlaySkipsSymlinks(t *testing.T) {
	envDir := writeOverlay(t, "my-app", map[string]string{".env.production": "SECRET=real"})

	secret := filepath.Join(t.TempDir(), "outside-secret")
	if err := os.WriteFile(secret, []byte("STOLEN=yes"), 0600); err != nil {
		t.Fatalf("could not write outside file: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(envDir, "my-app", ".env.linked")); err != nil {
		t.Fatalf("could not create symlink: %v", err)
	}

	repoDir := t.TempDir()
	if err := stageEnvOverlay("my-app", repoDir, stagingLogger(t)); err != nil {
		t.Fatalf("staging failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".env.linked")); !os.IsNotExist(err) {
		t.Error("symlinked overlay entry was staged, want it skipped")
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".env.production")); err != nil {
		t.Errorf("regular overlay file should still stage alongside skipped symlink: %v", err)
	}
}
