//go:build unix

package storage

import "golang.org/x/sys/unix"

func DiskSpace(path string) (total, free int64, err error) {
	var st unix.Statfs_t
	if err = unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		bsize = 4096
	}
	return int64(st.Blocks) * bsize, int64(st.Bavail) * bsize, nil
}
