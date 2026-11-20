//go:build !windows

package jobs

import (
	"syscall"
)

// getFreeDiskSpaceGB returns available disk space on Linux/Unix using statfs.
func getFreeDiskSpaceGB(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		if err := syscall.Statfs(".", &stat); err != nil {
			return 32.0, nil
		}
	}
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	return float64(freeBytes) / (1024 * 1024 * 1024), nil
}
