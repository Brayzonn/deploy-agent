package logger

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================
// Old log cleanup
// ============================================================

/*
PruneOld deletes deployment log files in logPath whose last change is
older than maxAge, so the log directory cannot fill the disk over
months of deploys. Only files this package writes
(deployment_*.log) are touched. Returns how many files were deleted;
files that cannot be read or removed are skipped, since a cleanup
problem must never stop a deploy.
*/
func PruneOld(logPath string, maxAge time.Duration) int {
	entries, err := os.ReadDir(logPath)
	if err != nil {
		return 0
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, "deployment_") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(logPath, name)) == nil {
			removed++
		}
	}
	return removed
}
