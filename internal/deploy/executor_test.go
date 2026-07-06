package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// ============================================================
// Fixtures
// ============================================================

const (
	oldCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

/*
fakeCommands scripts every command the executor's pipeline shells
out to, recording each call so tests can assert on routing. Git
queries answer from the head field, npm and compose-build failures
are injected via the fail flags, and a scripted clone can create
the repo directory the way a real clone would.
*/
type fakeCommands struct {
	head             string // what `git rev-parse HEAD` returns
	failNpm          bool
	failComposeBuild bool
	cloneCreates     string // repo dir a scripted `git clone` should create

	calls   []string // every command as "name arg arg..."
	npmDirs []string // working dir of every npm invocation
}

func (f *fakeCommands) run(dir, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)

	switch name {
	case "git":
		switch args[0] {
		case "rev-parse":
			return f.head + "\n", nil
		case "clone":
			if f.cloneCreates != "" {
				if err := os.MkdirAll(filepath.Join(f.cloneCreates, ".git"), 0755); err != nil {
					return "", err
				}
			}
			return "", nil
		default: // status, fetch, cat-file, checkout, reset
			return "", nil
		}
	case "npm":
		f.npmDirs = append(f.npmDirs, dir)
		if f.failNpm {
			return "", fmt.Errorf("npm exploded")
		}
		return "", nil
	case "docker", "docker-compose":
		if f.failComposeBuild && strings.HasSuffix(call, " build") {
			return "", fmt.Errorf("compose build exploded")
		}
		return "", nil
	}
	return "", fmt.Errorf("unexpected command: %s", call)
}

/* called reports whether any recorded command contains the substring. */
func (f *fakeCommands) called(substr string) bool {
	for _, call := range f.calls {
		if strings.Contains(call, substr) {
			return true
		}
	}
	return false
}

// ============================================================
// Helpers
// ============================================================

/*
newTestExecutor builds an Executor around the given repo config with
throwaway state/backup/log dirs and the fake wired into run. When
existing is true, the repo dir is created with a .git marker so the
executor takes the already-cloned path.
*/
func newTestExecutor(t *testing.T, repoCfg *types.RepoConfig, fake *fakeCommands, existing bool) *Executor {
	t.Helper()

	tmp := t.TempDir()

	if repoCfg.Name == "" {
		repoCfg.Name = "test-app"
	}
	if repoCfg.DeployBranch == "" {
		repoCfg.DeployBranch = "main"
	}
	if repoCfg.RepoDir == "" {
		repoCfg.RepoDir = filepath.Join(tmp, "repo")
	}
	if existing {
		if err := os.MkdirAll(filepath.Join(repoCfg.RepoDir, ".git"), 0755); err != nil {
			t.Fatalf("could not create fake repo: %v", err)
		}
	}

	log, err := logger.New("test", filepath.Join(tmp, "logs"))
	if err != nil {
		t.Fatalf("could not create logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })

	restore := run.Stub(fake.run)
	t.Cleanup(restore)

	ctx := &types.DeploymentContext{
		RepoName:     repoCfg.Name,
		Branch:       repoCfg.DeployBranch,
		RepoOwner:    "Brayzonn",
		Pusher:       "Brayzonn",
		Commit:       newCommit,
		RepoFullName: "Brayzonn/" + repoCfg.Name,
		DeploymentID: "test",
		Config:       repoCfg,
	}

	cfg := &config.Config{
		StateDir:  filepath.Join(tmp, "state"),
		BackupDir: filepath.Join(tmp, "backups"),
	}
	if err := os.MkdirAll(cfg.StateDir, 0755); err != nil {
		t.Fatalf("could not create state dir: %v", err)
	}

	return New(ctx, cfg, log)
}

// ============================================================
// Execute-level routing
// ============================================================

/* TestExecuteIgnoresOtherBranches checks a push to a non-deploy branch exits cleanly without running anything. */
func TestExecuteIgnoresOtherBranches(t *testing.T) {
	fake := &fakeCommands{head: oldCommit}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeClient}, fake, true)
	e.ctx.Branch = "feature/other"

	if err := e.Execute(); err != nil {
		t.Fatalf("push to other branch should be ignored, got: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("ignored push should run no commands, ran: %v", fake.calls)
	}
}

/* TestExecuteSkipsRedelivery checks a webhook redelivery of the deployed commit exits without syncing or deploying. */
func TestExecuteSkipsRedelivery(t *testing.T) {
	fake := &fakeCommands{head: newCommit} // already at the requested commit
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeClient}, fake, true)

	if err := e.Execute(); err != nil {
		t.Fatalf("redelivery should exit cleanly, got: %v", err)
	}
	if fake.called("git fetch") || fake.called("npm") {
		t.Fatalf("redelivery should not sync or build, ran: %v", fake.calls)
	}
}

// ============================================================
// Strategy routing
// ============================================================

/* TestDeployRoutesDockerProjects checks UseDocker sends the deploy down the compose path. */
func TestDeployRoutesDockerProjects(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failComposeBuild: true}
	e := newTestExecutor(t, &types.RepoConfig{
		ProjectType:       types.ProjectTypeAPITS,
		UseDocker:         true,
		DockerComposeFile: "docker-compose.yml",
	}, fake, true)

	composePath := filepath.Join(e.ctx.Config.RepoDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0644); err != nil {
		t.Fatalf("could not write compose file: %v", err)
	}

	err := e.Execute()
	if err == nil || !strings.Contains(err.Error(), "docker build failed") {
		t.Fatalf("expected docker build failure, got: %v", err)
	}
	if fake.called("npm") {
		t.Fatalf("docker route should not run npm, ran: %v", fake.calls)
	}
}

/* TestExecuteStagesEnvOverlay checks overlay files land in the checkout after sync, before the build runs. */
func TestExecuteStagesEnvOverlay(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failComposeBuild: true}
	e := newTestExecutor(t, &types.RepoConfig{
		ProjectType:       types.ProjectTypeAPITS,
		UseDocker:         true,
		DockerComposeFile: "docker-compose.yml",
	}, fake, true)

	composePath := filepath.Join(e.ctx.Config.RepoDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0644); err != nil {
		t.Fatalf("could not write compose file: %v", err)
	}

	writeOverlay(t, "test-app", map[string]string{
		".env.production": "SECRET=overlay",
		"client/.env":     "VITE_API=overlay",
	})

	if err := e.Execute(); err == nil {
		t.Fatal("expected docker build failure")
	}

	for rel, want := range map[string]string{
		".env.production": "SECRET=overlay",
		"client/.env":     "VITE_API=overlay",
	} {
		data, err := os.ReadFile(filepath.Join(e.ctx.Config.RepoDir, rel))
		if err != nil {
			t.Fatalf("overlay file %s was not staged into the checkout: %v", rel, err)
		}
		if string(data) != want {
			t.Errorf("staged %s = %q, want overlay copy %q", rel, data, want)
		}
	}
}

/* TestDeployRoutesClientProjects checks a CLIENT project builds npm in the client dir. */
func TestDeployRoutesClientProjects(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeClient}, fake, true)

	err := e.Execute()
	if err == nil || !strings.Contains(err.Error(), "client build failed") {
		t.Fatalf("expected client build failure, got: %v", err)
	}
	if len(fake.npmDirs) == 0 || fake.npmDirs[0] != e.ctx.Config.RepoDir {
		t.Fatalf("client build should run npm in the repo dir, ran in: %v", fake.npmDirs)
	}
}

/* TestDeployRoutesServerProjects checks an API project builds npm in the configured server dir. */
func TestDeployRoutesServerProjects(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{
		ProjectType: types.ProjectTypeAPIJS,
		ServerDir:   "server",
	}, fake, true)

	err := e.Execute()
	if err == nil || !strings.Contains(err.Error(), "server build failed") {
		t.Fatalf("expected server build failure, got: %v", err)
	}
	wantDir := filepath.Join(e.ctx.Config.RepoDir, "server")
	if len(fake.npmDirs) == 0 || fake.npmDirs[0] != wantDir {
		t.Fatalf("server build should run npm in %s, ran in: %v", wantDir, fake.npmDirs)
	}
}

/* TestDeployFullstackServerFirst checks the fullstack route deploys the server before the client. */
func TestDeployFullstackServerFirst(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{
		ProjectType: types.ProjectTypeAPIJS,
		FullStack:   true,
		ClientDir:   "client",
		ServerDir:   "server",
	}, fake, true)

	err := e.Execute()
	if err == nil || !strings.Contains(err.Error(), "server build failed") {
		t.Fatalf("fullstack should fail on the server step first, got: %v", err)
	}
	wantDir := filepath.Join(e.ctx.Config.RepoDir, "server")
	if len(fake.npmDirs) == 0 || fake.npmDirs[0] != wantDir {
		t.Fatalf("fullstack should build the server first in %s, ran in: %v", wantDir, fake.npmDirs)
	}
}

// ============================================================
// Rollback guards
// ============================================================

/* TestServerBuildFailureRollsBackGit checks a failed server build resets git to the pre-sync commit. */
func TestServerBuildFailureRollsBackGit(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeAPIJS}, fake, true)

	if err := e.Execute(); err == nil {
		t.Fatal("expected the deploy to fail")
	}
	if !fake.called("git reset --hard " + oldCommit) {
		t.Fatalf("failed build should reset git to %s, ran: %v", oldCommit[:7], fake.calls)
	}
}

/* TestFreshCloneFailureSkipsGitRollback checks a fresh clone with no previous commit never resets git to anything. */
func TestFreshCloneFailureSkipsGitRollback(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeAPIJS}, fake, false)
	fake.cloneCreates = e.ctx.Config.RepoDir

	if err := e.Execute(); err == nil {
		t.Fatal("expected the deploy to fail")
	}
	if !fake.called("git clone") {
		t.Fatalf("missing repo should be cloned, ran: %v", fake.calls)
	}
	if fake.called("git reset") {
		t.Fatalf("fresh clone has no previous commit — nothing to reset to, ran: %v", fake.calls)
	}
}
