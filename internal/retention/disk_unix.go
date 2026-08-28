//go:build unix

package retention

import "golang.org/x/sys/unix"

func diskUsage(path string) (total, free int64, err error) {
	var st unix.Statfs_t
	if err = unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		bsize = 4096
	}
	total = int64(st.Blocks) * bsize
	free = int64(st.Bavail) * bsize
	return total, free, nil
}
