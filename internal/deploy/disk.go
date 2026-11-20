package deploy

import (
	"fmt"
	"syscall"
)

// Below this much free space a build or image pull is likely to fail
// part-way, and Postgres/Redis on the same disk can stop writing.
// The limit is 2 GiB.
const minFreeDiskBytes = 2 << 30

// ============================================================
// Disk space
// ============================================================

/* freeDiskBytes returns the bytes available to this user on the filesystem holding path. */
func freeDiskBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

/*
checkDiskSpace warns, in the log and in the Slack warning that main
sends after the deploy, when the repo's filesystem is low on space.
It never fails the deploy: the build itself reports the real error
if space runs out.
*/
func (e *Executor) checkDiskSpace() {
	free, err := freeDiskBytes(e.ctx.Config.RepoDir)
	if err != nil {
		e.log.Warningf("Could not check free disk space: %v", err)
		return
	}
	if free < minFreeDiskBytes {
		msg := fmt.Sprintf("low disk space: %.1f GB free on the filesystem holding %s (warning threshold %d GB)",
			float64(free)/(1<<30), e.ctx.Config.RepoDir, minFreeDiskBytes>>30)
		e.log.Warningf("%s — deploys and databases on this disk may start failing", msg)
		e.manualActions = append(e.manualActions, msg)
	}
}
