package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

// ============================================================
// Helpers
// ============================================================

/* newTestClientBuilder returns a ClientBuilder wired to temp dirs and a test logger. */
func newTestClientBuilder(t *testing.T, webRoot string) *ClientBuilder {
	t.Helper()
	tmp := t.TempDir()
	log, err := logger.New("test", filepath.Join(tmp, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })

	return &ClientBuilder{
		builder:      New(tmp, log),
		webRoot:      webRoot,
		backupParent: filepath.Join(tmp, "backups"),
		log:          log,
	}
}

// ============================================================
// Tests
// ============================================================

/*
TestBackupWebRoot checks that the returned path is the directory the
backup actually landed in, and that a missing or empty web root
returns "" without error.
*/
func TestBackupWebRoot(t *testing.T) {
	webRoot := filepath.Join(t.TempDir(), "site")
	c := newTestClientBuilder(t, webRoot)

	got, err := c.backupWebRoot()
	if err != nil || got != "" {
		t.Fatalf("missing web root: got (%q, %v), want (\"\", nil)", got, err)
	}

	os.MkdirAll(webRoot, 0755)
	got, err = c.backupWebRoot()
	if err != nil || got != "" {
		t.Fatalf("empty web root: got (%q, %v), want (\"\", nil)", got, err)
	}

	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err = c.backupWebRoot()
	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}
	if got == "" {
		t.Fatal("backup of populated web root returned empty path")
	}
	if _, err := os.Stat(filepath.Join(got, "index.html")); err != nil {
		t.Fatalf("returned backup path %q does not contain the backed-up files: %v", got, err)
	}
}

/* TestBackupWebRootFailure checks a failing backup surfaces an error instead of being swallowed. */
func TestBackupWebRootFailure(t *testing.T) {
	webRoot := filepath.Join(t.TempDir(), "site")
	os.MkdirAll(webRoot, 0755)
	os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("hi"), 0644)

	c := newTestClientBuilder(t, webRoot)

	blocker := filepath.Join(t.TempDir(), "blocker")
	os.WriteFile(blocker, []byte(""), 0644)
	c.backupParent = blocker

	if _, err := c.backupWebRoot(); err == nil {
		t.Fatal("backup into an impossible parent succeeded, want error")
	}
}

/* readFile returns the content of path, failing the test if it can't be read. */
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	return string(data)
}

/*
TestSwapInReplacesWebRoot checks the stage-and-swap flow: the new
content ends up in the web root, the old content is gone, and no
.new/.old siblings are left behind.
*/
func TestSwapInReplacesWebRoot(t *testing.T) {
	webRoot := filepath.Join(t.TempDir(), "site")
	c := newTestClientBuilder(t, webRoot)

	os.MkdirAll(webRoot, 0755)
	os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("old"), 0644)

	buildOutput := filepath.Join(t.TempDir(), "dist")
	os.MkdirAll(buildOutput, 0755)
	os.WriteFile(filepath.Join(buildOutput, "index.html"), []byte("new"), 0644)

	staging, err := c.stage(buildOutput)
	if err != nil {
		t.Fatalf("stage failed: %v", err)
	}
	if got := readFile(t, filepath.Join(webRoot, "index.html")); got != "old" {
		t.Fatalf("staging must not touch the web root, but it serves %q", got)
	}

	disturbed, err := c.swapIn(staging)
	if err != nil {
		t.Fatalf("swap failed (disturbed=%t): %v", disturbed, err)
	}
	if got := readFile(t, filepath.Join(webRoot, "index.html")); got != "new" {
		t.Fatalf("web root serves %q after swap, want %q", got, "new")
	}
	for _, leftover := range []string{webRoot + ".new", webRoot + ".old"} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Errorf("swap left %s behind", leftover)
		}
	}
}

/* TestSwapInFirstDeploy checks the swap works when no web root exists yet. */
func TestSwapInFirstDeploy(t *testing.T) {
	webRoot := filepath.Join(t.TempDir(), "site")
	c := newTestClientBuilder(t, webRoot)

	buildOutput := filepath.Join(t.TempDir(), "dist")
	os.MkdirAll(buildOutput, 0755)
	os.WriteFile(filepath.Join(buildOutput, "index.html"), []byte("new"), 0644)

	staging, err := c.stage(buildOutput)
	if err != nil {
		t.Fatalf("stage failed: %v", err)
	}
	if disturbed, err := c.swapIn(staging); err != nil {
		t.Fatalf("swap failed (disturbed=%t): %v", disturbed, err)
	}
	if got := readFile(t, filepath.Join(webRoot, "index.html")); got != "new" {
		t.Fatalf("web root serves %q after first deploy, want %q", got, "new")
	}
}

/* TestStageFailureLeavesWebRootUntouched checks a failed staging copy never disturbs the serving site. */
func TestStageFailureLeavesWebRootUntouched(t *testing.T) {
	webRoot := filepath.Join(t.TempDir(), "site")
	c := newTestClientBuilder(t, webRoot)

	os.MkdirAll(webRoot, 0755)
	os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("old"), 0644)

	if _, err := c.stage(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("staging a missing source succeeded, want error")
	}
	if got := readFile(t, filepath.Join(webRoot, "index.html")); got != "old" {
		t.Fatalf("web root serves %q after failed staging, want untouched %q", got, "old")
	}
	if _, err := os.Stat(webRoot + ".new"); !os.IsNotExist(err) {
		t.Error("failed staging left the .new directory behind")
	}
}

/* TestValidateWebRoot checks the /var/www/html fence rejects escapes and accepts real site roots. */
func TestValidateWebRoot(t *testing.T) {
	cases := []struct {
		webRoot string
		ok      bool
	}{
		{"/var/www/html/mysite", true},
		{"/var/www/html/MINES", true},
		{"/var/www/html", false},
		{"/var/www/html/", false},
		{"/var/www/html/../..", false},
		{"/var/www/html/site/../../../etc", false},
		{"/home/deploy", false},
		{"/", false},
		{"", false},
	}

	for _, tc := range cases {
		c := &ClientBuilder{webRoot: tc.webRoot}
		err := c.validateWebRoot()
		if tc.ok && err != nil {
			t.Errorf("webRoot %q rejected, want accepted: %v", tc.webRoot, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("webRoot %q accepted, want rejected", tc.webRoot)
		}
	}
}
