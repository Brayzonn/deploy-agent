package deploy

import (
	"errors"
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
queries answer from the head field, and rev-parse of origin/<branch>
from tip (empty means the requested commit). Failures are injected
via the fail flags: failNpmUntilReset fails npm until a "git reset"
runs, and failComposeUp fails only the deploy's "up", not the
rollback's. A scripted clone can create the repo directory the way a
real clone would.
*/
type fakeCommands struct {
	head              string
	tip               string
	failNpm           bool
	failNpmUntilReset bool
	failComposeBuild  bool
	failComposeUp     bool
	cloneCreates      string

	calls   []string
	npmDirs []string
}

func (f *fakeCommands) run(dir, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)

	switch name {
	case "git":
		switch args[0] {
		case "rev-parse":
			if strings.HasPrefix(args[1], "origin/") {
				if f.tip != "" {
					return f.tip + "\n", nil
				}
				return newCommit + "\n", nil
			}
			return f.head + "\n", nil
		case "clone":
			if f.cloneCreates != "" {
				if err := os.MkdirAll(filepath.Join(f.cloneCreates, ".git"), 0755); err != nil {
					return "", err
				}
			}
			return "", nil
		default:
			return "", nil
		}
	case "npm":
		f.npmDirs = append(f.npmDirs, dir)
		if f.failNpmUntilReset && !f.called("git reset") {
			return "", fmt.Errorf("npm exploded on the new commit")
		}
		if f.failNpm {
			return "", fmt.Errorf("npm exploded")
		}
		return "", nil
	case "docker", "docker-compose":
		if f.failComposeBuild && strings.HasSuffix(call, " build") {
			return "", fmt.Errorf("compose build exploded")
		}
		if f.failComposeUp && strings.HasSuffix(call, " up -d --remove-orphans") {
			return "", fmt.Errorf("compose up exploded")
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
	fake := &fakeCommands{head: newCommit}
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

// ============================================================
// Rollback error reporting
// ============================================================

/*
TestWithRollbackNilKeepsError checks a successful rollback leaves the
deploy error exactly as it was.
*/
func TestWithRollbackNilKeepsError(t *testing.T) {
	deployErr := errors.New("compose up failed")

	if got := withRollback(deployErr, nil); got != deployErr {
		t.Fatalf("expected the original error back, got %v", got)
	}
}

/*
TestWithRollbackFailureShowsBoth checks a failed rollback is named in
the error the Slack alert prints, and that errors.Is still finds the
original deploy error underneath.
*/
func TestWithRollbackFailureShowsBoth(t *testing.T) {
	deployErr := errors.New("compose up failed")
	got := withRollback(deployErr, errors.New("verify found new image"))

	if !errors.Is(got, deployErr) {
		t.Errorf("errors.Is lost the original deploy error: %v", got)
	}
	for _, want := range []string{"compose up failed", "rollback ALSO failed", "MANUAL INTERVENTION REQUIRED", "verify found new image"} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("error missing %q: %v", want, got)
		}
	}
}

// ============================================================
// Last-good record and superseded pushes
// ============================================================

/* failedCommit is a commit that was checked out but never deployed successfully. */
const failedCommit = "cccccccccccccccccccccccccccccccccccccccc"

/*
newDockerTestExecutor builds an executor for a Docker repo with an
empty compose file, which the fake deploys successfully unless a
failure flag is set.
*/
func newDockerTestExecutor(t *testing.T, fake *fakeCommands) *Executor {
	t.Helper()
	e := newTestExecutor(t, &types.RepoConfig{
		ProjectType:       types.ProjectTypeAPITS,
		UseDocker:         true,
		DockerComposeFile: "docker-compose.yml",
	}, fake, true)

	composePath := filepath.Join(e.ctx.Config.RepoDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0644); err != nil {
		t.Fatalf("could not write compose file: %v", err)
	}
	return e
}

/* mustWriteLastGood seeds the last-good record for the test executor's repo. */
func mustWriteLastGood(t *testing.T, e *Executor, commit string) {
	t.Helper()
	if err := writeLastGood(e.cfg.StateDir, e.ctx.RepoName, commit); err != nil {
		t.Fatalf("could not seed last-good record: %v", err)
	}
}

/*
TestRetryOfFailedCommitDeploysAgain checks a checkout left on the
requested commit by a failed deploy is not mistaken for "already
deployed": the last-good record says otherwise, so it deploys again.
*/
func TestRetryOfFailedCommitDeploysAgain(t *testing.T) {
	fake := &fakeCommands{head: newCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeAPIJS}, fake, true)
	mustWriteLastGood(t, e, oldCommit)

	if err := e.Execute(); err == nil {
		t.Fatal("expected the retried deploy to run (and fail on npm)")
	}
	if !fake.called("npm") {
		t.Fatalf("retry of a failed commit should build again, ran: %v", fake.calls)
	}
}

/* TestRedeliveryOfLastGoodIsSkipped checks a commit that is both checked out and recorded as last good is skipped. */
func TestRedeliveryOfLastGoodIsSkipped(t *testing.T) {
	fake := &fakeCommands{head: newCommit}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeClient}, fake, true)
	mustWriteLastGood(t, e, newCommit)

	if err := e.Execute(); err != nil {
		t.Fatalf("redelivery should exit cleanly, got: %v", err)
	}
	if fake.called("git fetch") || fake.called("npm") {
		t.Fatalf("redelivery should not sync or build, ran: %v", fake.calls)
	}
}

/*
TestRollbackUsesLastGood checks a failed build rolls git back to the
last-good commit, not to the commit a previous failure left checked out.
*/
func TestRollbackUsesLastGood(t *testing.T) {
	fake := &fakeCommands{head: failedCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeAPIJS}, fake, true)
	mustWriteLastGood(t, e, oldCommit)

	if err := e.Execute(); err == nil {
		t.Fatal("expected the deploy to fail")
	}
	if !fake.called("git reset --hard " + oldCommit) {
		t.Fatalf("rollback should return to last good %s, ran: %v", oldCommit[:7], fake.calls)
	}
	if fake.called("git reset --hard " + failedCommit) {
		t.Fatalf("rollback must not return to the previously failed commit, ran: %v", fake.calls)
	}
}

/* TestSuccessRecordsLastGood checks a successful deploy saves its commit as both last attempted and last good. */
func TestSuccessRecordsLastGood(t *testing.T) {
	fake := &fakeCommands{head: oldCommit}
	e := newDockerTestExecutor(t, fake)

	if err := e.Execute(); err != nil {
		t.Fatalf("expected the deploy to succeed, got: %v", err)
	}
	if got, _ := readLastGood(e.cfg.StateDir, e.ctx.RepoName); got != newCommit {
		t.Errorf("last good = %q, want %q", got, newCommit)
	}
	if got, _ := readLastAttempted(e.cfg.StateDir, e.ctx.RepoName); got != newCommit {
		t.Errorf("last attempted = %q, want %q", got, newCommit)
	}
}

/* TestFailureKeepsLastGood checks a failed deploy records the attempt but leaves last good untouched. */
func TestFailureKeepsLastGood(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failComposeBuild: true}
	e := newDockerTestExecutor(t, fake)
	mustWriteLastGood(t, e, oldCommit)

	if err := e.Execute(); err == nil {
		t.Fatal("expected the deploy to fail")
	}
	if got, _ := readLastGood(e.cfg.StateDir, e.ctx.RepoName); got != oldCommit {
		t.Errorf("last good = %q after a failed deploy, want it unchanged at %q", got, oldCommit)
	}
	if got, _ := readLastAttempted(e.cfg.StateDir, e.ctx.RepoName); got != newCommit {
		t.Errorf("last attempted = %q, want %q", got, newCommit)
	}
}

/*
TestSupersededPushIsSkipped checks a push that is no longer the branch
tip exits cleanly without checking out, building, or recording anything.
*/
func TestSupersededPushIsSkipped(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, tip: failedCommit}
	e := newDockerTestExecutor(t, fake)

	if err := e.Execute(); err != nil {
		t.Fatalf("superseded push should exit cleanly, got: %v", err)
	}
	if fake.called("git checkout") || fake.called(" build") {
		t.Fatalf("superseded push should not check out or build, ran: %v", fake.calls)
	}
	if got, _ := readLastAttempted(e.cfg.StateDir, e.ctx.RepoName); got != "" {
		t.Errorf("superseded push should record nothing, last attempted = %q", got)
	}
}

/* TestCommitRecordRoundTrip checks a saved commit reads back exactly, and a missing record reads as "". */
func TestCommitRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if got, err := readLastGood(dir, "app"); err != nil || got != "" {
		t.Fatalf("missing record = %q, %v; want \"\", nil", got, err)
	}
	if err := writeLastGood(dir, "app", newCommit); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if got, err := readLastGood(dir, "app"); err != nil || got != newCommit {
		t.Fatalf("read back %q, %v; want %q", got, err, newCommit)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected only the record file, temp files left behind: %v", entries)
	}
}

// ============================================================
// Disk use
// ============================================================

/* TestSuccessPrunesDocker checks a successful Docker deploy prunes dangling images and old build cache. */
func TestSuccessPrunesDocker(t *testing.T) {
	fake := &fakeCommands{head: oldCommit}
	e := newDockerTestExecutor(t, fake)

	if err := e.Execute(); err != nil {
		t.Fatalf("expected the deploy to succeed, got: %v", err)
	}
	for _, want := range []string{"docker image prune -f", "docker builder prune -f --filter until=168h"} {
		if !fake.called(want) {
			t.Errorf("successful deploy should run %q, ran: %v", want, fake.calls)
		}
	}
}

/* TestFailureSkipsPrune checks a failed deploy leaves images alone, so nothing a rollback might need is pruned. */
func TestFailureSkipsPrune(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failComposeBuild: true}
	e := newDockerTestExecutor(t, fake)

	if err := e.Execute(); err == nil {
		t.Fatal("expected the deploy to fail")
	}
	if fake.called("prune") {
		t.Fatalf("failed deploy must not prune, ran: %v", fake.calls)
	}
}

/* TestFreeDiskBytesReadsRealFilesystem checks the statfs call works on this platform. */
func TestFreeDiskBytesReadsRealFilesystem(t *testing.T) {
	free, err := freeDiskBytes(t.TempDir())
	if err != nil || free == 0 {
		t.Fatalf("freeDiskBytes = %d, %v; want a positive number", free, err)
	}
}

// ============================================================
// Docker rollback
// ============================================================

/* indexOf returns the position of the first recorded call containing substr, or -1. */
func (f *fakeCommands) indexOf(substr string) int {
	for i, call := range f.calls {
		if strings.Contains(call, substr) {
			return i
		}
	}
	return -1
}

/*
TestDockerRollbackResetsGitFirst checks a failed Docker deploy resets
git to the previous commit before bringing the old containers back,
so compose reads the previous commit's compose file.
*/
func TestDockerRollbackResetsGitFirst(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failComposeUp: true}
	e := newDockerTestExecutor(t, fake)

	err := e.Execute()
	if err == nil || !strings.Contains(err.Error(), "docker deployment failed") {
		t.Fatalf("expected the compose up step to fail, got: %v", err)
	}
	reset := fake.indexOf("git reset --hard " + oldCommit)
	rollbackCompose := fake.indexOf("up -d --no-build")
	if rollbackCompose == -1 {
		rollbackCompose = fake.indexOf("docker-compose.yml down")
	}
	if reset == -1 || rollbackCompose == -1 {
		t.Fatalf("expected a git reset and a rollback compose call, ran: %v", fake.calls)
	}
	if reset > rollbackCompose {
		t.Fatalf("git reset must come before the rollback's compose call, ran: %v", fake.calls)
	}
}

// ============================================================
// PM2 build failure
// ============================================================

/*
TestServerBuildFailureRebuildsPrevious checks a failed server build
resets git and rebuilds the previous commit so the live app's files
are whole again, without restarting PM2.
*/
func TestServerBuildFailureRebuildsPrevious(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failNpmUntilReset: true}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeAPIJS}, fake, true)

	err := e.Execute()
	if err == nil || !strings.Contains(err.Error(), "server build failed") {
		t.Fatalf("expected the server build to fail, got: %v", err)
	}
	if strings.Contains(err.Error(), "rollback ALSO failed") {
		t.Fatalf("the previous version rebuilt fine, so no rollback failure should be reported: %v", err)
	}
	reset := fake.indexOf("git reset --hard " + oldCommit)
	if reset == -1 || len(fake.calls) <= reset || !fake.calledAfter(reset, "npm") {
		t.Fatalf("expected npm to run again after the git reset, ran: %v", fake.calls)
	}
	if fake.called("pm2") {
		t.Fatalf("restoring the build must not touch PM2, ran: %v", fake.calls)
	}
}

/* TestServerRebuildFailureIsReported checks a failed rebuild of the previous version shows up in the deploy error. */
func TestServerRebuildFailureIsReported(t *testing.T) {
	fake := &fakeCommands{head: oldCommit, failNpm: true}
	e := newTestExecutor(t, &types.RepoConfig{ProjectType: types.ProjectTypeAPIJS}, fake, true)

	err := e.Execute()
	if err == nil || !strings.Contains(err.Error(), "rollback ALSO failed") || !strings.Contains(err.Error(), "rebuild of previous version") {
		t.Fatalf("expected the failed rebuild in the error, got: %v", err)
	}
}

/* calledAfter reports whether any call after position i contains substr. */
func (f *fakeCommands) calledAfter(i int, substr string) bool {
	for _, call := range f.calls[i+1:] {
		if strings.Contains(call, substr) {
			return true
		}
	}
	return false
}
