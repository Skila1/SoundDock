package scan

import (
	"bytes"
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/metadata"
)

const JobRefresh = "metadata.refresh"

type refreshRow struct {
	TrackID   uuid.UUID
	AlbumID   uuid.UUID
	LibraryID uuid.UUID
	Title     string
	Album     string
	Artist    string
	Genre     string
	MBID      string
	Duration  int
	Year      int
	Locked    bool
}

type RefreshReport struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Updated  int `json:"updated"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
	Covers   int `json:"covers"`
	Unmatched int `json:"unmatched"`
}

func (s *Scanner) RefreshHandler(runner *jobs.Runner) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		return s.RefreshAll(ctx, runner, job.ID)
	}
}

func (s *Scanner) RefreshAll(ctx context.Context, runner *jobs.Runner, jobID uuid.UUID) error {
	if s == nil || s.pool == nil {
		return nil
	}
	rows, err := s.listRefreshRows(ctx)
	if err != nil {
		return err
	}
	rep := RefreshReport{Total: len(rows)}
	s.reportRefresh(ctx, runner, jobID, rep)
	if len(rows) == 0 {
		if runner != nil && jobID != uuid.Nil {
			runner.SetProgress(ctx, jobID, 100)
		}
		return nil
	}
	for i, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if runner != nil && jobID != uuid.Nil && runner.Cancelled(ctx, jobID) {
			return context.Canceled
		}
		updated, cover, unmatched, ferr := s.refreshOne(ctx, row)
		if ferr != nil {
			rep.Failed++
		} else if row.Locked || !updated {
			rep.Skipped++
		} else {
			if unmatched {
				rep.Unmatched++
			} else {
				rep.Updated++
			}
			if cover {
				rep.Covers++
			}
		}
		rep.Done = i + 1
		if runner != nil && jobID != uuid.Nil {
			runner.SetProgress(ctx, jobID, rep.Done*100/rep.Total)
			s.reportRefresh(ctx, runner, jobID, rep)
		}
	}
	if runner != nil && jobID != uuid.Nil {
		runner.SetProgress(ctx, jobID, 100)
		s.reportRefresh(ctx, runner, jobID, rep)
	}
	return nil
}

func (s *Scanner) reportRefresh(ctx context.Context, runner *jobs.Runner, jobID uuid.UUID, rep RefreshReport) {
	if runner == nil || jobID == uuid.Nil {
		return
	}
	runner.SetResult(ctx, jobID, rep)
}

func (s *Scanner) listRefreshRows(ctx context.Context) ([]refreshRow, error) {
	q, err := s.pool.Query(ctx, `
		SELECT t.id, coalesce(t.album_id, '00000000-0000-0000-0000-000000000000'), t.library_id,
		       t.title, coalesce(al.title, ''),
		       coalesce((
		         SELECT ar.name FROM track_artists ta
		         JOIN artists ar ON ar.id=ta.artist_id
		         WHERE ta.track_id=t.id AND ta.role='primary'
		         ORDER BY ta.position LIMIT 1
		       ), (
		         SELECT ar.name FROM album_artists aa
		         JOIN artists ar ON ar.id=aa.artist_id
		         WHERE aa.album_id=t.album_id AND aa.role='album_artist'
		         ORDER BY aa.position LIMIT 1
		       ), ''),
		       coalesce(t.genre_text, ''), coalesce(t.mbid, ''),
		       t.duration_ms, coalesce(t.year, 0), t.locked
		FROM tracks t
		LEFT JOIN albums al ON al.id=t.album_id
		ORDER BY t.created_at`)
	if err != nil {
		return nil, err
	}
	defer q.Close()
	var out []refreshRow
	for q.Next() {
		var r refreshRow
		if err := q.Scan(&r.TrackID, &r.AlbumID, &r.LibraryID, &r.Title, &r.Album, &r.Artist, &r.Genre, &r.MBID, &r.Duration, &r.Year, &r.Locked); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Scanner) refreshOne(ctx context.Context, row refreshRow) (updated, cover, unmatched bool, err error) {
	if row.Locked {
		return false, false, false, nil
	}
	probe := metadata.Probe{
		Title:      row.Title,
		Artist:     row.Artist,
		Album:      row.Album,
		Genre:      row.Genre,
		Year:       row.Year,
		DurationMS: row.Duration,
		MBID:       row.MBID,
	}
	before := probe
	metadata.EnrichMusicBrainzForced(ctx, &probe)
	unmatched = probe.Source != "musicbrainz" && probe.RecordingMBID == "" && probe.MBID == before.MBID
	if row.AlbumID != uuid.Nil && probe.Album != "" && (strings.TrimSpace(row.Album) == "" || strings.EqualFold(row.Album, "Unknown Album")) {
		_, _ = s.pool.Exec(ctx, `
			UPDATE albums SET title=$2
			WHERE id=$1 AND (title='' OR lower(title)='unknown album')`, row.AlbumID, probe.Album)
	}
	var fallback uuid.UUID
	_ = s.pool.QueryRow(ctx, `
		SELECT artist_id FROM track_artists
		WHERE track_id=$1 AND role='primary' ORDER BY position LIMIT 1`, row.TrackID).Scan(&fallback)
	s.updateExistingTrack(ctx, row.TrackID, row.AlbumID, nullTitle(probe.Title, row.Title), probe)
	s.attachArtistCredits(ctx, row.TrackID, fallback, probe)
	s.attachGenres(ctx, row.TrackID, probe)
	if row.AlbumID != uuid.Nil && probe.MBID != "" {
		_, _ = s.pool.Exec(ctx, `UPDATE albums SET mbid=COALESCE(NULLIF(mbid,''), $2) WHERE id=$1`, row.AlbumID, probe.MBID)
	}
	if row.AlbumID != uuid.Nil && probe.Year > 0 {
		_, _ = s.pool.Exec(ctx, `UPDATE albums SET year=COALESCE(year, $2) WHERE id=$1`, row.AlbumID, probe.Year)
	}
	if len(probe.Credits) > 0 {
		_, _ = s.upsertArtistMeta(ctx, probe.Credits[0].Name, probe.Credits[0].SortName, probe.Credits[0].MBID)
	} else if probe.ArtistMBID != "" && probe.Artist != "" {
		_, _ = s.upsertArtistMeta(ctx, probe.Artist, probe.ArtistSortName, probe.ArtistMBID)
	}
	if !s.hasArtwork(ctx, row) {
		metadata.EnrichCoverArtForced(ctx, &probe)
		if len(probe.Picture) > 0 && s.art != nil {
			ownerType, ownerID := "album", row.AlbumID
			if ownerID == uuid.Nil {
				ownerType, ownerID = "track", row.TrackID
			}
			if _, serr := s.art.Save(ctx, ownerType, ownerID, "coverartarchive", bytes.NewReader(probe.Picture)); serr == nil {
				cover = true
			}
		}
	}
	updated = true
	return updated, cover, unmatched, nil
}

func (s *Scanner) hasArtwork(ctx context.Context, row refreshRow) bool {
	if s.pool == nil {
		return false
	}
	var n bool
	if row.AlbumID != uuid.Nil {
		_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artwork_assets WHERE owner_type='album' AND owner_id=$1)`, row.AlbumID).Scan(&n)
		if n {
			return true
		}
	}
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artwork_assets WHERE owner_type='track' AND owner_id=$1)`, row.TrackID).Scan(&n)
	return n
}
