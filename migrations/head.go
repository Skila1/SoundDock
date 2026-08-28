package migrations

import (
	"strconv"
	"strings"
)

// Head is the highest applied migration version shipped in this binary.
func Head() int64 {
	entries, err := FS.ReadDir(".")
	if err != nil {
		return 0
	}
	var max int64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		n := 0
		for n < len(name) && name[n] >= '0' && name[n] <= '9' {
			n++
		}
		if n == 0 {
			continue
		}
		v, err := strconv.ParseInt(name[:n], 10, 64)
		if err != nil {
			continue
		}
		if v > max {
			max = v
		}
	}
	return max
}
