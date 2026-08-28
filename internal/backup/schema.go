package backup

import (
	"strconv"
	"strings"

	"github.com/sounddock/sounddock/migrations"
)

// ImageSchemaHead is the highest numbered *.up.sql shipped in this binary.
func ImageSchemaHead() int {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return 0
	}
	max := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		n, err := strconv.Atoi(name[:4])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max
}
