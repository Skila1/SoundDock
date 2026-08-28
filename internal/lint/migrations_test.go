package lint

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

var migRe = regexp.MustCompile(`^(\d{4})_.*\.up\.sql$`)

func TestNoMigrationGapAfter0021(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "migrations")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var vers []int
	seen := map[int]string{}
	for _, e := range ents {
		m := migRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		if prev, ok := seen[n]; ok {
			t.Fatalf("duplicate version %d: %s and %s", n, prev, e.Name())
		}
		seen[n] = e.Name()
		vers = append(vers, n)
	}
	sort.Ints(vers)
	if len(vers) == 0 {
		t.Fatal("no migrations")
	}
	if _, ok := seen[13]; ok {
		t.Fatal("do not fill historical gap 0013")
	}
	if _, ok := seen[21]; !ok {
		t.Fatal("0021 required")
	}
	if seen[23] != "" && seen[22] == "" {
		t.Fatal("0023 must not exist without 0022")
	}
	max := vers[len(vers)-1]
	for v := 22; v <= max; v++ {
		if _, ok := seen[v]; !ok {
			t.Fatalf("migration gap at %04d (max %d)", v, max)
		}
	}
}
