package pm2

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
)

// ============================================================
// Helpers
// ============================================================

/*
fakePM2 is a scripted pm2 daemon: it answers `pm2 jlist` from its
status field ("" means the app doesn't exist) and mutates that
status the way the real commands would, while recording every
subcommand it saw.
*/
type fakePM2 struct {
	appName string
	status  string
	calls   []string
}

func (f *fakePM2) run(dir, name string, args ...string) (string, error) {
	if name != "pm2" {
		return "", fmt.Errorf("unexpected command %s", name)
	}
	f.calls = append(f.calls, args[0])

	switch args[0] {
	case "jlist":
		if f.status == "" {
			return "[]", nil
		}
		return fmt.Sprintf(`[{"name":%q,"pm2_env":{"status":%q}}]`, f.appName, f.status), nil
	case "start", "restart":
		f.status = "online"
		return "", nil
	case "delete":
		f.status = ""
		return "", nil
	}

	return "", fmt.Errorf("unexpected pm2 subcommand %s", args[0])
}

/* newTestApp wires an App to a fake pm2 daemon and a test logger. */
func newTestApp(t *testing.T, initialStatus string) (*App, *fakePM2) {
	t.Helper()

	log, err := logger.New("test", filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })

	fake := &fakePM2{appName: "my-app", status: initialStatus}
	t.Cleanup(run.Stub(fake.run))

	origSleep := sleep
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = origSleep })

	return New("my-app", t.TempDir(), log), fake
}

// ============================================================
// Tests
// ============================================================

/* TestEnsureRunningStartsMissingApp checks a missing app is created and confirmed online. */
func TestEnsureRunningStartsMissingApp(t *testing.T) {
	mgr, fake := newTestApp(t, "")

	if err := mgr.EnsureRunning("ecosystem.config.js", "", 5); err != nil {
		t.Fatalf("EnsureRunning failed: %v", err)
	}
	if !contains(fake.calls, "start") {
		t.Errorf("start was never called, calls: %v", fake.calls)
	}
}

/* TestEnsureRunningRestartsStoppedApp checks a stopped app is restarted rather than recreated. */
func TestEnsureRunningRestartsStoppedApp(t *testing.T) {
	mgr, fake := newTestApp(t, "stopped")

	if err := mgr.EnsureRunning("", "app.js", 5); err != nil {
		t.Fatalf("EnsureRunning failed: %v", err)
	}
	if !contains(fake.calls, "restart") {
		t.Errorf("restart was never called, calls: %v", fake.calls)
	}
	if contains(fake.calls, "delete") {
		t.Errorf("stopped app was deleted, calls: %v", fake.calls)
	}
}

/* TestEnsureRunningRecreatesErroredApp checks an errored app is deleted, recreated, and confirmed online. */
func TestEnsureRunningRecreatesErroredApp(t *testing.T) {
	mgr, fake := newTestApp(t, "errored")

	if err := mgr.EnsureRunning("", "app.js", 5); err != nil {
		t.Fatalf("EnsureRunning failed: %v", err)
	}

	deleteIdx := index(fake.calls, "delete")
	startIdx := index(fake.calls, "start")
	if deleteIdx < 0 || startIdx < 0 || startIdx < deleteIdx {
		t.Errorf("want delete then start, calls: %v", fake.calls)
	}
}

/* TestEnsureRunningGivesUp checks an app that never comes up produces an error, not an infinite loop. */
func TestEnsureRunningGivesUp(t *testing.T) {
	mgr, fake := newTestApp(t, "launching")

	err := mgr.EnsureRunning("", "app.js", 3)
	if err == nil {
		t.Fatal("EnsureRunning succeeded for an app stuck in an unknown state")
	}
	if !strings.Contains(err.Error(), "launching") {
		t.Errorf("error %q does not report the stuck status", err)
	}
	_ = fake
}

/* TestStartNeitherConfigured checks the unstartable configuration errors instead of running `pm2 start ""`. */
func TestStartNeitherConfigured(t *testing.T) {
	mgr, fake := newTestApp(t, "")

	if err := mgr.Start("", ""); err == nil {
		t.Fatal("Start with no ecosystem file and no server entry succeeded")
	}
	if len(fake.calls) != 0 {
		t.Errorf("pm2 was invoked despite unstartable config: %v", fake.calls)
	}
}

// ============================================================
// Slice helpers
// ============================================================

func contains(s []string, v string) bool { return index(s, v) >= 0 }

func index(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
