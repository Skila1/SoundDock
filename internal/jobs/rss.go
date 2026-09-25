package jobs

import (
	"os"
	"strconv"
	"strings"
)

// processRSSMB is intentionally best-effort. Linux containers expose this
// through procfs; unsupported platforms return zero and leave the pool limit
// unenforced rather than guessing from heap statistics.
func processRSSMB() int {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb <= 0 {
			return 0
		}
		return int((kb + 1023) / 1024)
	}
	return 0
}
