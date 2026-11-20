package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

// ============================================================
// Helpers
// ============================================================

/* mustRun executes a command in dir and fails the test on error. */
func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

/* commit writes file with content, commits it, and returns the new HEAD hash. */
func commit(t *testing.T, dir, file, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", "-A")
	mustRun(t, dir, "git", "commit", "-m", msg)
	return mustRun(t, dir, "git", "rev-parse", "HEAD")
}

// ============================================================
// Tests
// ============================================================

/*
TestSyncScenarios exercises SyncToCommit against real git repos,
covering the deploy-target states it must handle deterministically:
plain sync, wrong branch with dirty and untracked files, force-pushed
remote, a webhook commit that no longer exists, and a commit that a
newer push has superseded.
*/
func TestSyncScenarios(t *testing.T) {
	tmp := t.TempDir()
	log, err := logger.New("test", filepath.Join(tmp, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	origin := filepath.Join(tmp, "origin")
	os.MkdirAll(origin, 0755)
	mustRun(t, origin, "git", "init", "-b", "main")
	c1 := commit(t, origin, "app.txt", "v1", "c1")
	mustRun(t, origin, "git", "checkout", "-b", "develop")
	commit(t, origin, "app.txt", "dev", "dev work")
	mustRun(t, origin, "git", "checkout", "main")
	c2 := commit(t, origin, "app.txt", "v2", "c2")

	deployDir := filepath.Join(tmp, "deploy")
	mustRun(t, tmp, "git", "clone", origin, deployDir)
	g := New(deployDir, "main", log)

	if err := g.SyncToCommit(c2); err != nil {
		t.Fatalf("scenario 1 (plain sync): %v", err)
	}
	if got, _ := g.GetCurrentCommit(); got != c2 {
		t.Fatalf("scenario 1: HEAD = %s, want %s", got, c2)
	}

	mustRun(t, deployDir, "git", "checkout", "develop")
	os.WriteFile(filepath.Join(deployDir, "app.txt"), []byte("hand-edit"), 0644)
	os.WriteFile(filepath.Join(deployDir, ".env.production"), []byte("SECRET=1"), 0600)

	changes, err := g.LocalChanges()
	if err != nil || len(changes) != 1 {
		t.Fatalf("scenario 2: LocalChanges = %v, %v; want 1 tracked change (untracked excluded)", changes, err)
	}

	if err := g.SyncToCommit(c2); err != nil {
		t.Fatalf("scenario 2 (wrong branch + dirty): %v", err)
	}
	if got, _ := g.GetCurrentCommit(); got != c2 {
		t.Fatalf("scenario 2: HEAD = %s, want %s", got, c2)
	}
	if branch := mustRun(t, deployDir, "git", "branch", "--show-current"); branch != "main" {
		t.Fatalf("scenario 2: on branch %q, want main", branch)
	}
	if body, _ := os.ReadFile(filepath.Join(deployDir, "app.txt")); string(body) != "v2" {
		t.Fatalf("scenario 2: tracked file = %q, want %q", body, "v2")
	}
	if _, err := os.Stat(filepath.Join(deployDir, ".env.production")); err != nil {
		t.Fatalf("scenario 2: untracked .env.production was destroyed: %v", err)
	}

	mustRun(t, origin, "git", "reset", "--hard", c1)
	c3 := commit(t, origin, "app.txt", "v3", "c3 (rewrote history)")
	if err := g.SyncToCommit(c3); err != nil {
		t.Fatalf("scenario 3 (force push): %v", err)
	}
	if got, _ := g.GetCurrentCommit(); got != c3 {
		t.Fatalf("scenario 3: HEAD = %s, want %s", got, c3)
	}

	gone := strings.Repeat("d", 40)
	if err := g.SyncToCommit(gone); err == nil {
		t.Fatal("scenario 4 (missing commit): expected error, got nil")
	} else if !strings.Contains(err.Error(), "force push") {
		t.Fatalf("scenario 4: error should explain the force-push cause: %v", err)
	}
	if got, _ := g.GetCurrentCommit(); got != c3 {
		t.Fatalf("scenario 4: HEAD moved to %s after failed sync, want %s", got, c3)
	}

	commit(t, origin, "app.txt", "v4", "c4 (newer push)")
	if err := g.SyncToCommit(c3); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("scenario 5 (superseded): expected ErrSuperseded, got %v", err)
	}
	if got, _ := g.GetCurrentCommit(); got != c3 {
		t.Fatalf("scenario 5: HEAD moved to %s after a superseded sync, want %s", got, c3)
	}
}
