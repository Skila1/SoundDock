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
	"github.com/sounddock/sounddock/internal/scapex"
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

func Handler(pool *pgxpool.Pool, box *cryptox.Box, hooks *webhooks.Bus, sx *scapex.Client) jobs.Handler {
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
		meta, tracks, err := GetPlaylistItems(ctx, p.Provider, access, extra, p.ExternalID)
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
		var epid uuid.UUID
		err = pool.QueryRow(ctx, `
			INSERT INTO external_playlists (provider_account_id, user_id, provider, external_playlist_id, sounddock_playlist_id, name, description, owner_external_id, artwork_url, track_count, external_snapshot, sync_mode, sync_interval, removal_policy, last_sync_attempt_at, last_sync_status)
			VALUES ((SELECT id FROM external_provider_accounts WHERE user_id=$1 AND provider=$2), $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),'running')
			ON CONFLICT (user_id, provider, external_playlist_id) DO UPDATE SET
				sounddock_playlist_id=COALESCE(external_playlists.sounddock_playlist_id, EXCLUDED.sounddock_playlist_id),
				name=EXCLUDED.name, description=EXCLUDED.description, artwork_url=EXCLUDED.artwork_url, track_count=EXCLUDED.track_count,
				external_snapshot=EXCLUDED.external_snapshot, sync_mode=EXCLUDED.sync_mode, sync_interval=EXCLUDED.sync_interval,
				removal_policy=EXCLUDED.removal_policy, last_sync_attempt_at=now(), last_sync_status='running'
			RETURNING id, sounddock_playlist_id`,
			p.UserID, p.Provider, p.ExternalID, sdID, name, meta.Description, meta.Owner, meta.Artwork, meta.TrackCount, meta.Snapshot, p.Mode, p.Interval, p.Removal,
		).Scan(&epid, &sdID)
		if err != nil {
			return err
		}

		var runID uuid.UUID
		_ = pool.QueryRow(ctx, `INSERT INTO external_sync_runs (external_playlist_id, job_id) VALUES ($1,$2) RETURNING id`, epid, job.ID).Scan(&runID)

		_, _ = pool.Exec(ctx, `DELETE FROM external_playlist_items WHERE external_playlist_id=$1`, epid)
		matched, unmatched, amb := 0, 0, 0
		var keepIDs []uuid.UUID
		total := len(tracks)
		if total == 0 {
			total = 1
		}
		for i, tr := range tracks {
			touchJob(ctx, pool, job.ID, (i*90)/total)
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
			if res.TrackID == nil && p.shouldFill() && sx != nil {
				tid, ferr := FillTrack(ctx, sx, p.Provider, tr.ID, tr.Title, tr.Artists)
				if ferr == nil && tid != uuid.Nil {
					res.TrackID = &tid
					status = "high"
					res.Source = "youtube"
					res.Confidence = 0.9
				}
			}
			if res.TrackID != nil && (status == "exact" || status == "high") {
				matched++
				keepIDs = append(keepIDs, *res.TrackID)
				_, _ = pool.Exec(ctx, `INSERT INTO external_track_mappings (provider, provider_track_id, isrc, sounddock_track_id, mapping_source, confidence)
					VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (provider, provider_track_id) DO NOTHING`,
					p.Provider, tr.ID, tr.ISRC, *res.TrackID, res.Source, res.Confidence)
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
			_, _ = pool.Exec(ctx, `INSERT INTO external_playlist_items (external_playlist_id, position, provider_track_id, source_url, title, artists, album, duration_ms, isrc, metadata, mapped_track_id, match_status, match_confidence)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
				epid, i, tr.ID, tr.SourceURL, tr.Title, arts, tr.Album, tr.DurationMS, tr.ISRC, metaJSON, res.TrackID, status, res.Confidence)
		}

		if p.Removal == "mirror" || p.Mode == "once" {
			_, _ = pool.Exec(ctx, `DELETE FROM playlist_entries WHERE playlist_id=$1`, sdID)
			for i, tid := range keepIDs {
				_, _ = pool.Exec(ctx, `INSERT INTO playlist_entries (playlist_id, track_id, position) VALUES ($1,$2,$3)`, sdID, tid, i)
			}
		} else {
			var max int
			_ = pool.QueryRow(ctx, `SELECT coalesce(max(position),-1) FROM playlist_entries WHERE playlist_id=$1`, sdID).Scan(&max)
			for _, tid := range keepIDs {
				var n int
				_ = pool.QueryRow(ctx, `SELECT count(*) FROM playlist_entries WHERE playlist_id=$1 AND track_id=$2`, sdID, tid).Scan(&n)
				if n == 0 {
					max++
					_, _ = pool.Exec(ctx, `INSERT INTO playlist_entries (playlist_id, track_id, position) VALUES ($1,$2,$3)`, sdID, tid, max)
				}
			}
		}

		var next any
		if p.Mode == "sync" {
			next = time.Now().Add(parseInterval(p.Interval))
		}
		_, _ = pool.Exec(ctx, `UPDATE external_playlists SET last_sync_at=now(), next_sync_at=$2, last_sync_status='ok', last_error='', sounddock_playlist_id=$3 WHERE id=$1`, epid, next, sdID)
		_, _ = pool.Exec(ctx, `UPDATE external_sync_runs SET finished_at=now(), matched_count=$2, unmatched_count=$3, ambiguous_count=$4, added_count=$5 WHERE id=$1`, runID, matched, unmatched, amb, len(keepIDs))
		if hooks != nil {
			hooks.Emit(ctx, "external.playlist.sync.completed", map[string]any{"playlist_id": sdID, "provider": p.Provider, "matched": matched, "unmatched": unmatched})
		}
		return nil
	}
}

func TickHandler(pool *pgxpool.Pool, enqueue func(context.Context, string, any) (uuid.UUID, error)) jobs.Handler {
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
			_, _ = enqueue(ctx, "external.playlist.import", ImportPayload{
				UserID: r.uid, Provider: r.prov, ExternalID: r.ext, Mode: "sync",
				Interval: r.iv, Removal: r.rem, PlaylistUUID: r.sd, LibraryIDs: UserLibraryIDs(ctx, pool, r.uid),
			})
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

func userToken(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, userID uuid.UUID, provider string, st Settings) (access, extra string, err error) {
	extra = st.Extra["developer_token"]
	if extra == "" {
		extra = st.Extra["api_key"]
	}
	var accEnc []byte
	err = pool.QueryRow(ctx, `SELECT access_token_enc FROM external_provider_accounts WHERE user_id=$1 AND provider=$2 AND status='connected'`, userID, provider).Scan(&accEnc)
	if err != nil {
		return "", extra, err
	}
	if box != nil && len(accEnc) > 0 {
		if p, e := box.Decrypt(accEnc); e == nil {
			access = string(p)
		}
	}
	return access, extra, nil
}
