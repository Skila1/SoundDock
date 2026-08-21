package scan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/artwork"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/metadata"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/webhooks"
)

var audioExt = map[string]bool{
	".mp3": true, ".flac": true, ".aac": true, ".m4a": true, ".alac": true,
	".ogg": true, ".opus": true, ".wav": true, ".oga": true,
}

type Scanner struct {
	pool *pgxpool.Pool
	art  *artwork.Store
	log  *slog.Logger
	hook *webhooks.Bus
}

func New(pool *pgxpool.Pool, art *artwork.Store, log *slog.Logger, hook *webhooks.Bus) *Scanner {
	return &Scanner{pool: pool, art: art, log: log, hook: hook}
}

type Payload struct {
	LibraryID uuid.UUID `json:"library_id"`
	Kind      string    `json:"kind"`
}

func (s *Scanner) Handler(providers func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p Payload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		prov, libID, prefix, err := providers(ctx, p.LibraryID)
		if err != nil {
			return err
		}
		return s.ScanLibrary(ctx, libID, prov, prefix, p.Kind, job.ID)
	}
}

func (s *Scanner) ScanLibrary(ctx context.Context, libID uuid.UUID, prov storage.StorageProvider, prefix, kind string, jobID uuid.UUID) error {
	var runID uuid.UUID
	if err := s.pool.QueryRow(ctx, `INSERT INTO scan_runs (library_id, job_id, kind) VALUES ($1,$2,$3) RETURNING id`, libID, jobID, kind).Scan(&runID); err != nil {
		return err
	}
	it, err := prov.List(ctx, prefix)
	if err != nil {
		return err
	}
	defer it.Close()
	var added, failed, seenN int
	for it.Next() {
		e := it.Entry()
		if e.IsDir || !audioExt[strings.ToLower(path.Ext(e.Key))] {
			continue
		}
		seenN++
		if err := s.ingestFile(ctx, libID, prov, e); err != nil {
			failed++
			_, _ = s.pool.Exec(ctx, `INSERT INTO scan_file_errors (scan_run_id, storage_key, error) VALUES ($1,$2,$3)`, runID, e.Key, err.Error())
			s.log.Warn("scan file failed", "key", e.Key, "err", err)
			continue
		}
		added++
	}
	if err := it.Err(); err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE scan_runs SET files_seen=$2, files_added=$3, files_failed=$4, finished_at=now() WHERE id=$1`,
		runID, seenN, added, failed)
	if s.hook != nil {
		s.hook.Emit(ctx, "library.scan.completed", map[string]any{"library_id": libID, "seen": seenN, "failed": failed})
	}
	return nil
}

func (s *Scanner) ingestFile(ctx context.Context, libID uuid.UUID, prov storage.StorageProvider, e storage.Entry) error {
	var localPath string
	if ls, ok := prov.(storage.FFmpegSourcer); ok {
		src, err := ls.FFmpegSource(ctx, e.Key)
		if err == nil && src.Path != "" {
			localPath = src.Path
		}
		if src.Close != nil {
			defer src.Close()
		}
	}
	var probe metadata.Probe
	var hash string
	var size int64
	if localPath != "" {
		st, err := os.Stat(localPath)
		if err != nil {
			return err
		}
		size = st.Size()
		h, err := hashFile(localPath)
		if err != nil {
			return err
		}
		hash = h
		probe, _ = metadata.FromFile(localPath)
	} else {
		rc, info, err := prov.Open(ctx, e.Key)
		if err != nil {
			return err
		}
		defer rc.Close()
		tmp, err := os.CreateTemp("", "sd-scan-*"+path.Ext(e.Key))
		if err != nil {
			return err
		}
		name := tmp.Name()
		defer os.Remove(name)
		hw := sha256.New()
		n, err := io.Copy(tmp, io.TeeReader(rc, hw))
		tmp.Close()
		if err != nil {
			return err
		}
		size = n
		if info != nil && info.Size > 0 {
			size = info.Size
		}
		hash = hex.EncodeToString(hw.Sum(nil))
		probe, _ = metadata.FromFile(name)
		localPath = name
	}

	artistName := probe.AlbumArtist
	if artistName == "" {
		artistName = probe.Artist
	}
	if artistName == "" {
		artistName = "Unknown Artist"
	}
	albumTitle := probe.Album
	if albumTitle == "" {
		albumTitle = "Unknown Album"
	}

	artistID, err := s.upsertArtist(ctx, artistName)
	if err != nil {
		return err
	}
	trackArtistID := artistID
	if probe.Artist != "" && !strings.EqualFold(probe.Artist, artistName) {
		trackArtistID, _ = s.upsertArtist(ctx, probe.Artist)
	}

	albumID, err := s.upsertAlbum(ctx, libID, albumTitle, probe, artistID)
	if err != nil {
		return err
	}

	var existing uuid.UUID
	err = s.pool.QueryRow(ctx, `SELECT track_id FROM track_files WHERE library_id=$1 AND storage_key=$2`, libID, e.Key).Scan(&existing)
	var trackID uuid.UUID
	if err == nil {
		trackID = existing
		_, _ = s.pool.Exec(ctx, `UPDATE tracks SET title=$2, disc_number=$3, track_number=$4, duration_ms=$5, year=NULLIF($6,0), genre_text=$7, updated_at=now() WHERE id=$1 AND locked=false`,
			trackID, nullTitle(probe.Title, e.Key), max1(probe.Disc), probe.Track, probe.DurationMS, probe.Year, probe.Genre)
	} else {
		err = s.pool.QueryRow(ctx, `
			INSERT INTO tracks (library_id, album_id, title, disc_number, track_number, duration_ms, year, genre_text)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,0),$8) RETURNING id`,
			libID, albumID, nullTitle(probe.Title, e.Key), max1(probe.Disc), probe.Track, probe.DurationMS, probe.Year, probe.Genre).Scan(&trackID)
		if err != nil {
			return err
		}
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',0) ON CONFLICT DO NOTHING`, trackID, trackArtistID)
	if probe.Composer != "" {
		cid, _ := s.upsertArtist(ctx, probe.Composer)
		_, _ = s.pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'composer',0) ON CONFLICT DO NOTHING`, trackID, cid)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, codec, container, bitrate, sample_rate, channels, quality)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'original')
		ON CONFLICT (library_id, storage_key) DO UPDATE SET
		  size_bytes=EXCLUDED.size_bytes, content_hash=EXCLUDED.content_hash, codec=EXCLUDED.codec,
		  bitrate=EXCLUDED.bitrate, sample_rate=EXCLUDED.sample_rate, channels=EXCLUDED.channels`,
		trackID, libID, e.Key, size, hash, probe.Codec, probe.Container, probe.Bitrate, probe.SampleRate, probe.Channels)
	if err != nil {
		return err
	}

	if len(probe.Picture) > 0 && s.art != nil {
		_, _ = s.art.Save(ctx, "album", albumID, "embedded", strings.NewReader(string(probe.Picture)))
	} else if localPath != "" && s.art != nil {
		if fa := artwork.FolderArt(filepath.Dir(localPath)); fa != "" {
			f, err := os.Open(fa)
			if err == nil {
				_, _ = s.art.Save(ctx, "album", albumID, "folder", f)
				f.Close()
			}
		}
	}
	if s.hook != nil {
		s.hook.Emit(ctx, "track.added", map[string]any{"track_id": trackID, "library_id": libID})
	}
	return nil
}

func (s *Scanner) upsertArtist(ctx context.Context, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM artists WHERE lower(name)=lower($1) LIMIT 1`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO artists (name, sort_name) VALUES ($1,$1) RETURNING id`, name).Scan(&id)
	return id, err
}

func (s *Scanner) upsertAlbum(ctx context.Context, libID uuid.UUID, title string, probe metadata.Probe, artistID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT a.id FROM albums a
		JOIN album_artists aa ON aa.album_id=a.id AND aa.role='album_artist'
		WHERE a.library_id=$1 AND lower(a.title)=lower($2) AND coalesce(a.year,0)=coalesce(NULLIF($3,0),0)
		  AND coalesce(a.edition_title,'')='' AND aa.artist_id=$4
		LIMIT 1`, libID, title, probe.Year, artistID).Scan(&id)
	if err == nil {
		return id, nil
	}
	discTotal := probe.DiscTotal
	if discTotal < 1 {
		discTotal = 1
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO albums (title, year, disc_count, edition_title, library_id)
		VALUES ($1, NULLIF($2,0), $3, '', $4) RETURNING id`, title, probe.Year, discTotal, libID).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO album_artists (album_id, artist_id, role, position) VALUES ($1,$2,'album_artist',0)`, id, artistID)
	return id, nil
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func nullTitle(t, key string) string {
	if t != "" {
		return t
	}
	return strings.TrimSuffix(filepath.Base(key), filepath.Ext(key))
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
