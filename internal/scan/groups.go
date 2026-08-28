package scan

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	// DurationWindowMS is the ±3s comparison window inside an artist+title block.
	DurationWindowMS = 3000
	// DuplicateGroupMinMembers is the persist threshold (groups of 1 are not stored).
	DuplicateGroupMinMembers = 2

	dupMethodContentHash = "content_hash"
	dupMethodArtistTitle = "artist_title"
)

type timedTrack struct {
	ID         uuid.UUID
	DurationMS int
}

// NormalizeBlockingPart lowercases, trims, and collapses whitespace for artist/title keys.
func NormalizeBlockingPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimSpace(b.String())
}

// ArtistTitleBlockingKey is the scan blocking key: normalised artist + title.
func ArtistTitleBlockingKey(artist, title string) string {
	return NormalizeBlockingPart(artist) + "\t" + NormalizeBlockingPart(title)
}

func skipArtistTitleBlock(artist, title string) bool {
	a := NormalizeBlockingPart(artist)
	t := NormalizeBlockingPart(title)
	if a == "" || t == "" {
		return true
	}
	if a == "unknown artist" || t == "unknown title" {
		return true
	}
	return false
}

func contentHashBlockingKey(hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return ""
	}
	return dupMethodContentHash + ":" + hash
}

func artistTitleClusterKey(blockKey string, clusterIndex int) string {
	return dupMethodArtistTitle + ":" + blockKey + ":" + strconv.Itoa(clusterIndex)
}

// ClusterByDuration groups tracks whose durations form a chain of gaps ≤ windowMS.
// One cluster is returned per connected component - never one row per pair.
func ClusterByDuration(tracks []timedTrack, windowMS int) [][]timedTrack {
	if len(tracks) == 0 {
		return nil
	}
	sorted := append([]timedTrack(nil), tracks...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].DurationMS != sorted[j].DurationMS {
			return sorted[i].DurationMS < sorted[j].DurationMS
		}
		return bytesLessUUID(sorted[i].ID, sorted[j].ID)
	})
	var out [][]timedTrack
	cur := []timedTrack{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		prev := cur[len(cur)-1]
		if sorted[i].DurationMS-prev.DurationMS <= windowMS {
			cur = append(cur, sorted[i])
			continue
		}
		out = append(out, cur)
		cur = []timedTrack{sorted[i]}
	}
	out = append(out, cur)
	return out
}

func bytesLessUUID(a, b uuid.UUID) bool {
	for i := 0; i < 16; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

func uniqueSortedUUIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return bytesLessUUID(out[i], out[j]) })
	return out
}
