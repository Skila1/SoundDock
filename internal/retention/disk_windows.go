//go:build windows

package retention

import "golang.org/x/sys/windows"

func diskUsage(path string) (total, free int64, err error) {
	var freeBytes, totalBytes, totalFree uint64
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	if err = windows.GetDiskFreeSpaceEx(p, &freeBytes, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return int64(totalBytes), int64(freeBytes), nil
}
