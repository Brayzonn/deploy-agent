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

	// Missing web root: nothing to back up
	got, err := c.backupWebRoot()
	if err != nil || got != "" {
		t.Fatalf("missing web root: got (%q, %v), want (\"\", nil)", got, err)
	}

	// Empty web root: nothing to back up
	os.MkdirAll(webRoot, 0755)
	got, err = c.backupWebRoot()
	if err != nil || got != "" {
		t.Fatalf("empty web root: got (%q, %v), want (\"\", nil)", got, err)
	}

	// Populated web root: returned path must exist and contain the site files
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

	// Make the backup parent an existing file so MkdirAll fails
	blocker := filepath.Join(t.TempDir(), "blocker")
	os.WriteFile(blocker, []byte(""), 0644)
	c.backupParent = blocker

	if _, err := c.backupWebRoot(); err == nil {
		t.Fatal("backup into an impossible parent succeeded, want error")
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
		{"/home/zoney", false},
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
