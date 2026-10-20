package external

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/ssrf"
)

// ArtSaver stores an image as the artwork of a SoundDock object (artwork.Store).
type ArtSaver interface {
	Save(ctx context.Context, ownerType string, ownerID uuid.UUID, source string, r io.Reader) (uuid.UUID, error)
}

// coverSize is the edge length requested from providers that template artwork URLs.
const coverSize = "1000"

// coverURL turns a provider artwork URL into one that can be downloaded. Apple
// Music returns a template with {w}x{h} placeholders.
func coverURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	r := strings.NewReplacer("{w}", coverSize, "{h}", coverSize, "{f}", "jpg", "{c}", "")
	return r.Replace(raw)
}

// copyPlaylistCover downloads a provider's playlist artwork and stores it as the
// SoundDock playlist's cover. It leaves any cover the playlist already has.
func copyPlaylistCover(ctx context.Context, q execer, art ArtSaver, playlistID uuid.UUID, urls ...string) bool {
	if art == nil || playlistID == uuid.Nil {
		return false
	}
	var n int
	_ = q.QueryRow(ctx, `SELECT count(*) FROM artwork_assets WHERE owner_type='playlist' AND owner_id=$1`, playlistID).Scan(&n)
	if n > 0 {
		return false
	}
	for _, raw := range urls {
		u := coverURL(raw)
		if u == "" {
			continue
		}
		opt := ssrf.DefaultOptions()
		opt.MaxBytes = 20 << 20
		opt.Timeout = 20 * time.Second
		rc, _, _, err := ssrf.Fetch(ctx, u, opt)
		if err != nil {
			continue
		}
		_, err = art.Save(ctx, "playlist", playlistID, "provider", rc)
		rc.Close()
		if err == nil {
			return true
		}
	}
	return false
}
