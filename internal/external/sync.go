package external

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/matcher"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/webhooks"
)

type ImportPayload struct {
	UserID       uuid.UUID   `json:"user_id"`
	Provider     string      `json:"provider"`
	ExternalID   string      `json:"external_id"`
	Mode         string      `json:"mode"`
	Name         string      `json:"name"`
	Interval     string      `json:"sync_interval"`
	Removal      string      `json:"removal_policy"`
	PlaylistUUID uuid.UUID   `json:"sounddock_playlist_id"`
	LibraryIDs   []uuid.UUID `json:"library_ids"`
	FillYouTube  *bool       `json:"fill_youtube,omitempty"`
}

func (p ImportPayload) shouldFill() bool {
	return p.FillYouTube == nil || *p.FillYouTube
}

func (p ImportPayload) CoalesceKey() string {
	return "external.playlist.import|" + p.UserID.String() + "|" + p.Provider + "|" + p.ExternalID
}

func Handler(pool *pgxpool.Pool, box *cryptox.Box, hooks *webhooks.Bus, sx Filler) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p ImportPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		if p.Mode == "" {
			p.Mode = "once"
		}
		if p.Removal == "" {
			p.Removal = "mirror"
		}
		if p.Interval == "" {
			p.Interval = "6h"
		}
		st, _ := LoadSettings(ctx, pool, box, p.Provider)
		access, extra, err := userToken(ctx, pool, box, p.UserID, p.Provider, st)
		if err != nil {
			if !st.PublicImport {
				return err
			}
			access, extra = publicAccess(ctx, st)
		}
		refresh := func(ctx context.Context) (string, error) {
			tok, _, rerr := ForceRefresh(ctx, pool, box, p.UserID, p.Provider, st)
			if rerr != nil {
				return "", rerr
			}
			access = tok
			return tok, nil
		}
		meta, tracks, err := GetPlaylistItemsRefresh(ctx, p.Provider, access, extra, p.ExternalID, refresh)
		if err != nil {
			_, _ = pool.Exec(ctx, `UPDATE external_playlists SET last_sync_status='failed', last_error=$2, last_sync_attempt_at=now() WHERE user_id=$1 AND provider=$3 AND external_playlist_id=$4`, p.UserID, err.Error(), p.Provider, p.ExternalID)
			return err
		}
		name := p.Name
		if name == "" {
			name = meta.Name
		}
		if name == "" {
			name = p.Provider + " playlist"
		}
		sdID := p.PlaylistUUID
		var existing uuid.UUID
		_ = pool.QueryRow(ctx, `SELECT sounddock_playlist_id FROM external_playlists WHERE user_id=$1 AND provider=$2 AND external_playlist_id=$3 AND sounddock_playlist_id IS NOT NULL`, p.UserID, p.Provider, p.ExternalID).Scan(&existing)
		if existing != uuid.Nil {
			sdID = existing
		}
		if sdID == uuid.Nil {
			_ = pool.QueryRow(ctx, `INSERT INTO playlists (user_id, name, description) VALUES ($1,$2,$3) RETURNING id`, p.UserID, name, meta.Description).Scan(&sdID)
		}
		var prevSnap, prevStatus string
		_ = pool.QueryRow(ctx, `
			SELECT coalesce(external_snapshot,''), coalesce(last_sync_status,'')
			FROM external_playlists WHERE user_id=$1 AND provider=$2 AND external_playlist_id=$3`,
			p.UserID, p.Provider, p.ExternalID).Scan(&prevSnap, &prevStatus)
		snap := retainedSnapshot(prevSnap, meta.Snapshot)
		var epid uuid.UUID
		err = pool.QueryRow(ctx, `
			INSERT INTO external_playlists (provider_account_id, user_id, provider, external_playlist_id, sounddock_playlist_id, name, description, owner_external_id, artwork_url, track_count, external_snapshot, sync_mode, sync_interval, removal_policy, last_sync_attempt_at, last_sync_status)
			VALUES ((SELECT id FROM external_provider_accounts WHERE user_id=$1 AND provider=$2), $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),'running')
			ON CONFLICT (user_id, provider, external_playlist_id) DO UPDATE SET
				sounddock_playlist_id=COALESCE(external_playlists.sounddock_playlist_id, EXCLUDED.sounddock_playlist_id),
				name=EXCLUDED.name, description=EXCLUDED.description, artwork_url=EXCLUDED.artwork_url, track_count=EXCLUDED.track_count,
				external_snapshot=COALESCE(NULLIF(EXCLUDED.external_snapshot,''), external_playlists.external_snapshot),
				sync_mode=EXCLUDED.sync_mode, sync_interval=EXCLUDED.sync_interval,
				removal_policy=EXCLUDED.removal_policy, last_sync_attempt_at=now(), last_sync_status='running'
			RETURNING id, sounddock_playlist_id`,
			p.UserID, p.Provider, p.ExternalID, sdID, name, meta.Description, meta.Owner, meta.Artwork, meta.TrackCount, snap, p.Mode, p.Interval, p.Removal,
		).Scan(&epid, &sdID)
		if err != nil {
			return err
		}

		if snapshotUnchanged(prevSnap, prevStatus, meta.Snapshot) && sdID != uuid.Nil {
			_, _ = pool.Exec(ctx, `UPDATE external_playlists SET last_sync_at=now(), last_sync_status='ok', last_error='', last_sync_attempt_at=now() WHERE id=$1`, epid)
			notifyPlaylistInvalidate(ctx, pool, p.UserID, sdID)
			return nil
		}

		var runID uuid.UUID
		_ = pool.QueryRow(ctx, `INSERT INTO external_sync_runs (external_playlist_id, job_id) VALUES ($1,$2) RETURNING id`, epid, job.ID).Scan(&runID)

		type pendingItem struct {
			pos        int
			tr         Track
			arts       string
			metaJSON   []byte
			trackID    *uuid.UUID
			status     string
			confidence float64
		}
		matched, unmatched, amb := 0, 0, 0
		var keepIDs []uuid.UUID
		var rows []pendingItem
		total := len(tracks)
		if total == 0 {
			total = 1
		}
		lastProg := -10
		for i, tr := range tracks {
			prog := (i * 90) / total
			touchJob(ctx, pool, job.ID, prog)
			if prog-lastProg >= 10 {
				lastProg = prog
				notifyJobProgress(ctx, pool, p.UserID, job.ID, prog)
			}
			tr.Provider = p.Provider
			res := matcher.Match(ctx, pool, p.LibraryIDs, matcher.Query{
				Provider: p.Provider, ID: tr.ID, Title: tr.Title, Artists: tr.Artists, DurationMS: tr.DurationMS, ISRC: tr.ISRC,
			})
			status := res.Status
			if status == "possible" {
				status = "unmatched"
				res.TrackID = nil
			} else if status == "ambiguous" {
				amb++
				res.TrackID = nil
			}
			if res.TrackID != nil && !trackPlayable(ctx, pool, *res.TrackID) {
				res.TrackID = nil
			}
			if res.TrackID == nil {
				if mapped := mappedPlayable(ctx, pool, p.Provider, tr.ID); mapped != uuid.Nil {
					res.TrackID = &mapped
					status = "high"
					res.Source = "mapping"
					res.Confidence = 1
				}
			}
			if res.TrackID == nil && p.shouldFill() && sx != nil {
				tid, ferr := FillTrack(ctx, sx, p.Provider, tr.ID, tr.Title, tr.Artists)
				if ferr == nil && tid != uuid.Nil {
					res.TrackID = &tid
					status = "high"
					res.Source = "youtube"
					res.Confidence = 0.9
				}
			}
			if res.TrackID != nil {
				if id, ok := keepMembership(*res.TrackID, trackPlayable(ctx, pool, *res.TrackID)); ok && (status == "exact" || status == "high") {
					matched++
					keepIDs = append(keepIDs, id)
					_, _ = pool.Exec(ctx, `INSERT INTO external_track_mappings (provider, provider_track_id, isrc, sounddock_track_id, mapping_source, confidence)
						VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (provider, provider_track_id) DO NOTHING`,
						p.Provider, tr.ID, tr.ISRC, id, res.Source, res.Confidence)
				} else {
					unmatched++
					status = "pending"
					res.TrackID = nil
				}
			} else {
				unmatched++
				if status == "" {
					status = "unmatched"
				}
			}
			arts := ""
			if len(tr.Artists) > 0 {
				arts = tr.Artists[0]
				for _, a := range tr.Artists[1:] {
					arts += ", " + a
				}
			}
			metaJSON, _ := json.Marshal(tr)
			rows = append(rows, pendingItem{pos: i, tr: tr, arts: arts, metaJSON: metaJSON, trackID: res.TrackID, status: status, confidence: res.Confidence})
		}

		removal := p.Removal
		if p.Mode == "once" {
			removal = "mirror"
		}
		var next any
		if p.Mode == "sync" {
			next = time.Now().Add(parseInterval(p.Interval))
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `DELETE FROM external_playlist_items WHERE external_playlist_id=$1`, epid); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := tx.Exec(ctx, `INSERT INTO external_playlist_items (external_playlist_id, position, provider_track_id, source_url, title, artists, album, duration_ms, isrc, metadata, mapped_track_id, match_status, match_confidence)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
				epid, row.pos, row.tr.ID, row.tr.SourceURL, row.tr.Title, row.arts, row.tr.Album, row.tr.DurationMS, row.tr.ISRC, row.metaJSON, row.trackID, row.status, row.confidence); err != nil {
				return err
			}
		}
		if err := reconcilePlaylistEntries(ctx, tx, sdID, keepIDs, removal); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE external_playlists SET last_sync_at=now(), next_sync_at=$2, last_sync_status='ok', last_error='', sounddock_playlist_id=$3, external_snapshot=COALESCE(NULLIF($4,''), external_snapshot) WHERE id=$1`, epid, next, sdID, snap); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE external_sync_runs SET finished_at=now(), matched_count=$2, unmatched_count=$3, ambiguous_count=$4, added_count=$5 WHERE id=$1`, runID, matched, unmatched, amb, len(keepIDs)); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		notifyJobProgress(ctx, pool, p.UserID, job.ID, 100)
		notifyPlaylistInvalidate(ctx, pool, p.UserID, sdID)
		if hooks != nil {
			hooks.Emit(ctx, "external.playlist.sync.completed", map[string]any{"playlist_id": sdID, "provider": p.Provider, "matched": matched, "unmatched": unmatched})
		}
		return nil
	}
}

func TickHandler(pool *pgxpool.Pool, enqueue func(context.Context, string, string, any) (uuid.UUID, error)) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		rows, err := pool.Query(ctx, `SELECT user_id, provider, external_playlist_id, sync_interval, removal_policy, sounddock_playlist_id FROM external_playlists WHERE sync_mode='sync' AND next_sync_at IS NOT NULL AND next_sync_at <= now()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		type row struct {
			uid, sd            uuid.UUID
			prov, ext, iv, rem string
		}
		var list []row
		for rows.Next() {
			var r row
			var sd *uuid.UUID
			_ = rows.Scan(&r.uid, &r.prov, &r.ext, &r.iv, &r.rem, &sd)
			if sd != nil {
				r.sd = *sd
			}
			list = append(list, r)
		}
		for _, r := range list {
			p := ImportPayload{
				UserID: r.uid, Provider: r.prov, ExternalID: r.ext, Mode: "sync",
				Interval: r.iv, Removal: r.rem, PlaylistUUID: r.sd, LibraryIDs: UserLibraryIDs(ctx, pool, r.uid),
			}
			_, _ = enqueue(ctx, "external.playlist.import", p.CoalesceKey(), p)
		}
		_ = job
		return nil
	}
}

func UserLibraryIDs(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) []uuid.UUID {
	var admin bool
	_ = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND r.name='Administrator')`, userID).Scan(&admin)
	var rows pgx.Rows
	var err error
	if admin {
		rows, err = pool.Query(ctx, `SELECT id FROM libraries`)
	} else {
		rows, err = pool.Query(ctx, `SELECT library_id FROM library_grants WHERE user_id=$1
			UNION SELECT lg.library_id FROM library_grants lg JOIN user_roles ur ON ur.role_id=lg.role_id WHERE ur.user_id=$1`, userID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

func parseInterval(s string) time.Duration {
	switch s {
	case "1h":
		return time.Hour
	case "12h":
		return 12 * time.Hour
	case "24h", "daily":
		return 24 * time.Hour
	default:
		return 6 * time.Hour
	}
}

func touchJob(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, progress int) {
	if progress < 0 {
		progress = 0
	}
	if progress > 99 {
		progress = 99
	}
	_, _ = pool.Exec(ctx, `UPDATE jobs SET locked_until=now()+interval '30 minutes', progress=$2, updated_at=now() WHERE id=$1`, id, progress)
}

func notifyJobProgress(ctx context.Context, pool *pgxpool.Pool, userID, jobID uuid.UUID, progress int) {
	if pool == nil || userID == uuid.Nil {
		return
	}
	_ = playback.Notify(ctx, pool, playback.Signal{
		T:     "job.progress",
		RID:   jobID.String(),
		Rev:   int64(progress),
		Scope: "user",
		Actor: userID.String(),
		Keys:  []string{"job.progress"},
	})
}

func notifyPlaylistInvalidate(ctx context.Context, pool *pgxpool.Pool, userID, playlistID uuid.UUID) {
	if pool == nil || userID == uuid.Nil {
		return
	}
	ids := []string{}
	if playlistID != uuid.Nil {
		ids = append(ids, playlistID.String())
	}
	_ = playback.Notify(ctx, pool, playback.Signal{
		T:     "resource.invalidate",
		Scope: "user",
		Actor: userID.String(),
		Keys:  []string{"playlists", "playlist", "unmatched", "me-providers"},
		IDs:   ids,
	})
}

func userToken(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, userID uuid.UUID, provider string, st Settings) (access, extra string, err error) {
	return EnsureAccess(ctx, pool, box, userID, provider, st)
}
