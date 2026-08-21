package playback

import (
	"sort"

	"github.com/google/uuid"
)

type queueMeta struct {
	Position int
	TrackID  uuid.UUID
	AlbumID  uuid.UUID
	ArtistID uuid.UUID
	Disc     int
	TrackNo  int
}

func nextIndex(items []queueMeta, idx, delta int, repeat, mode string, shuffle, ended bool, intn func(int) int) (int, bool) {
	n := len(items)
	if n == 0 {
		return 0, true
	}
	if idx < 0 || idx >= n {
		idx = 0
	}
	if repeat == "one" && delta > 0 && ended {
		return idx, false
	}
	if !shuffle {
		next := idx + delta
		if next >= n {
			if repeat == "queue" {
				return 0, false
			}
			return idx, true
		}
		if next < 0 {
			if repeat == "queue" {
				return n - 1, false
			}
			return 0, false
		}
		return next, false
	}
	if delta < 0 {
		next := idx - 1
		if next < 0 {
			if repeat == "queue" {
				return n - 1, false
			}
			return 0, false
		}
		return next, false
	}
	switch mode {
	case "album":
		return nextAlbum(items, idx, repeat)
	case "smart":
		return nextSmart(items, idx, intn)
	default:
		if n == 1 {
			if repeat == "off" {
				return 0, true
			}
			return 0, false
		}
		off := 1
		if intn != nil {
			off = intn(n-1) + 1
		}
		return (idx + off) % n, false
	}
}

func nextAlbum(items []queueMeta, idx int, repeat string) (int, bool) {
	order := make([]int, len(items))
	for i := range items {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := items[order[i]], items[order[j]]
		if a.AlbumID != b.AlbumID {
			return a.AlbumID.String() < b.AlbumID.String()
		}
		if a.Disc != b.Disc {
			return a.Disc < b.Disc
		}
		if a.TrackNo != b.TrackNo {
			return a.TrackNo < b.TrackNo
		}
		return a.Position < b.Position
	})
	pos := 0
	for i, oi := range order {
		if oi == idx {
			pos = i
			break
		}
	}
	next := pos + 1
	if next >= len(order) {
		if repeat == "queue" {
			return order[0], false
		}
		return idx, true
	}
	return order[next], false
}

func nextSmart(items []queueMeta, idx int, intn func(int) int) (int, bool) {
	cur := items[idx]
	var diff, same []int
	for i := range items {
		if i == idx {
			continue
		}
		if cur.ArtistID != uuid.Nil && items[i].ArtistID == cur.ArtistID {
			same = append(same, i)
		} else {
			diff = append(diff, i)
		}
	}
	pool := diff
	if len(pool) == 0 {
		pool = same
	}
	if len(pool) == 0 {
		return idx, false
	}
	pick := 0
	if intn != nil {
		pick = intn(len(pool))
	}
	return pool[pick], false
}
