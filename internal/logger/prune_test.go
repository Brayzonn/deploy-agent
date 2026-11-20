package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ============================================================
// Old log cleanup
// ============================================================

/*
TestPruneOldRemovesOnlyOldDeploymentLogs checks old deployment logs
are deleted while recent logs and unrelated files are kept.
*/
func TestPruneOldRemovesOnlyOldDeploymentLogs(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-40 * 24 * time.Hour)

	files := map[string]bool{
		"deployment_old.log":    true,
		"deployment_recent.log": false,
		"notes.txt":             true,
	}
	for name, backdate := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if backdate {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}

	if got := PruneOld(dir, 30*24*time.Hour); got != 1 {
		t.Fatalf("removed %d files, want 1", got)
	}
	for name, wantGone := range map[string]bool{"deployment_old.log": true, "deployment_recent.log": false, "notes.txt": false} {
		_, err := os.Stat(filepath.Join(dir, name))
		if gone := os.IsNotExist(err); gone != wantGone {
			t.Errorf("%s: gone = %v, want %v", name, gone, wantGone)
		}
	}
}

/* TestPruneOldMissingDir checks a missing log directory is not an error. */
func TestPruneOldMissingDir(t *testing.T) {
	if got := PruneOld(filepath.Join(t.TempDir(), "nope"), time.Hour); got != 0 {
		t.Fatalf("removed %d from a missing dir, want 0", got)
	}
}
