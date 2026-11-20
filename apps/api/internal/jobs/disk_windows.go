//go:build windows

package jobs

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

// getFreeDiskSpaceGB returns available disk space on Windows using GetDiskFreeSpaceExW.
func getFreeDiskSpaceGB(path string) (float64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytes, totalBytes, totalFreeBytes int64
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 50.0, nil
	}

	r, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r == 0 {
		root, _ := syscall.UTF16PtrFromString(filepath.VolumeName(path) + "\\")
		r, _, _ = getDiskFreeSpaceEx.Call(
			uintptr(unsafe.Pointer(root)),
			uintptr(unsafe.Pointer(&freeBytes)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&totalFreeBytes)),
		)
		if r == 0 {
			return 32.0, nil
		}
	}
	return float64(freeBytes) / (1024 * 1024 * 1024), nil
}
