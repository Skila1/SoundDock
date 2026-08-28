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
	"github.com/sounddock/sounddock/internal/fingerprint"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/metadata"
	"github.com/sounddock/sounddock/internal/organise"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/waveform"
	"github.com/sounddock/sounddock/internal/webhooks"
)

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
	Prefix    string    `json:"prefix,omitempty"`
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
		if strings.TrimSpace(p.Prefix) != "" {
			prefix = path.Join(prefix, p.Prefix)
		}
		return s.ScanLibrary(ctx, libID, prov, prefix, p.Kind, job.ID)
	}
}

func (s *Scanner) ScanLibrary(ctx context.Context, libID uuid.UUID, prov storage.StorageProvider, prefix, kind string, jobID uuid.UUID) error {
	var runID uuid.UUID
	var err error
	if jobID == uuid.Nil {
		err = s.pool.QueryRow(ctx, `INSERT INTO scan_runs (library_id, kind) VALUES ($1,$2) RETURNING id`, libID, kind).Scan(&runID)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT id FROM scan_runs WHERE job_id=$1`, jobID).Scan(&runID)
		if err != nil {
			err = s.pool.QueryRow(ctx, `INSERT INTO scan_runs (library_id, job_id, kind) VALUES ($1,$2,$3) RETURNING id`, libID, jobID, kind).Scan(&runID)
		}
	}
	if err != nil {
		return err
	}
	it, err := prov.List(ctx, prefix)
	if err != nil {
		return err
	}
	var files []storage.Entry
	for it.Next() {
		e := it.Entry()
		if e.IsDir || SkipScanKey(e.Key) || !IsAudioExt(strings.ToLower(path.Ext(e.Key))) {
			continue
		}
		files = append(files, storage.Entry{Key: e.Key, Size: e.Size, ModTime: e.ModTime, ETag: e.ETag})
	}
	listErr := it.Err()
	_ = it.Close()
	if listErr != nil {
		return listErr
	}
	total := len(files)
	s.reportScan(ctx, jobID, runID, 0, total, 0, 0)
	var added, failed, seenN int
	known := s.knownArtistFn(ctx)
	for _, e := range files {
		if ctx.Err() != nil {
			break
		}
		if jobID != uuid.Nil && seenN%25 == 24 {
			var cancel bool
			_ = s.pool.QueryRow(ctx, `SELECT cancel_requested FROM jobs WHERE id=$1`, jobID).Scan(&cancel)
			if cancel {
				break
			}
		}
		seenN++
		if err := s.ingestFile(ctx, libID, prov, e, e.Key, kind, known); err != nil {
			failed++
			_, _ = s.pool.Exec(ctx, `INSERT INTO scan_file_errors (scan_run_id, storage_key, error) VALUES ($1,$2,$3)`, runID, e.Key, err.Error())
			s.log.Warn("scan file failed", "key", e.Key, "err", err)
		} else {
			added++
		}
		if seenN == total || seenN%5 == 0 {
			s.reportScan(ctx, jobID, runID, seenN, total, added, failed)
		}
	}
	s.reportScan(ctx, jobID, runID, seenN, total, added, failed)
	_, _ = s.pool.Exec(ctx, `UPDATE scan_runs SET files_seen=$2, files_added=$3, files_failed=$4, files_total=$5, finished_at=now() WHERE id=$1`,
		runID, seenN, added, failed, total)
	if s.hook != nil {
		s.hook.Emit(ctx, "library.scan.completed", map[string]any{"library_id": libID, "seen": seenN, "failed": failed})
	}
	return nil
}

func (s *Scanner) reportScan(ctx context.Context, jobID, runID uuid.UUID, done, total, added, failed int) {
	pct := ProgressPct(done, total)
	if jobID != uuid.Nil {
		_, _ = s.pool.Exec(ctx, `UPDATE jobs SET progress=$2, updated_at=now() WHERE id=$1`, jobID, pct)
	}
	_, _ = s.pool.Exec(ctx, `UPDATE scan_runs SET files_seen=$2, files_added=$3, files_failed=$4, files_total=$5 WHERE id=$1`,
		runID, done, added, failed, total)
}

// ProgressPct is 1–99 while work remains so the bar moves as soon as listing finishes.
func ProgressPct(done, total int) int {
	if total <= 0 {
		return 100
	}
	if done <= 0 {
		return 1
	}
	if done >= total {
		return 100
	}
	pct := done * 100 / total
	if pct < 1 {
		return 1
	}
	if pct > 99 {
		return 99
	}
	return pct
}

func (s *Scanner) ingestFile(ctx context.Context, libID uuid.UUID, prov storage.StorageProvider, e storage.Entry, originalName, kind string, known func(string) bool) error {
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

	opts := s.libraryOpts(ctx, libID)
	var origSize int64 = size
	var companionPath, companionKey string
	var companionSize int64
	if localPath != "" && prov.Capabilities().Write && !opts.ReadOnly && transcode.NeedsCompress(e.Key, probe.Codec) {
		if flac, err := transcode.CompressToFLACPreset(ctx, localPath, opts.Preset); err == nil {
			in, _ := os.Stat(localPath)
			out, _ := os.Stat(flac)
			if out != nil && (in == nil || out.Size() < in.Size()) {
				if opts.KeepOriginal {
					if h, herr := hashFile(flac); herr == nil {
						companionKey = CompanionStorageKey(e.Key, h)
					} else {
						companionKey = transcode.ReplaceExt(e.Key, ".flac")
					}
					companionPath = flac
					companionSize = out.Size()
					if in != nil {
						origSize = in.Size()
					}
				} else {
					newKey := transcode.ReplaceExt(e.Key, ".flac")
					if f, oerr := os.Open(flac); oerr == nil {
						werr := prov.Write(ctx, newKey, f, storage.WriteInfo{Size: out.Size()})
						f.Close()
						if werr == nil {
							if newKey != e.Key {
								_ = prov.Delete(ctx, e.Key)
							}
							e.Key = newKey
							e.Size = out.Size()
							size = out.Size()
							if in != nil {
								origSize = in.Size()
							}
							if h, herr := hashFile(flac); herr == nil {
								hash = h
							}
							probe, _ = metadata.FromFile(flac)
						}
					}
					os.Remove(flac)
				}
			} else {
				os.Remove(flac)
			}
		}
	}
	if companionPath != "" {
		defer os.Remove(companionPath)
	}

	if originalName == "" || metadata.LooksLikeHash(originalName) {
		var fn string
		if hash != "" {
			_ = s.pool.QueryRow(ctx, `SELECT filename FROM upload_sessions WHERE content_hash=$1 AND coalesce(filename,'') <> '' ORDER BY updated_at DESC LIMIT 1`, hash).Scan(&fn)
		}
		if fn != "" {
			originalName = fn
		} else {
			originalName = e.Key
		}
	}
	if known == nil {
		known = s.knownArtistFn(ctx)
	}
	metadata.ApplyOriginalNameKnown(&probe, originalName, known)
	metadata.EnrichMusicBrainz(ctx, s.pool, &probe)

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

	title := nullTitle(probe.Title, originalName)
	acq, acqRef := "", ""
	if ref := InboxVideoID(e.Key, kind); ref != "" {
		acq, acqRef = "youtube", ref
	}
	var existing uuid.UUID
	err = s.pool.QueryRow(ctx, `SELECT track_id FROM track_files WHERE library_id=$1 AND storage_key=$2`, libID, e.Key).Scan(&existing)
	var trackID uuid.UUID
	if err == nil {
		trackID = existing
	} else if acqRef != "" {
		_ = s.pool.QueryRow(ctx, `SELECT id FROM tracks WHERE library_id=$1 AND acquisition_ref=$2 ORDER BY created_at DESC LIMIT 1`, libID, acqRef).Scan(&existing)
		if existing != uuid.Nil {
			trackID = existing
			err = nil
		}
	}
	if err == nil && trackID != uuid.Nil {
		_, _ = s.pool.Exec(ctx, `
			UPDATE tracks SET
			  title=$2,
			  album_id=$3,
			  disc_number=$4,
			  track_number=$5,
			  duration_ms=CASE WHEN $6 > 0 THEN $6 ELSE duration_ms END,
			  year=COALESCE(NULLIF($7,0), year),
			  genre_text=CASE WHEN $8 <> '' THEN $8 ELSE genre_text END,
			  metadata_source=CASE WHEN $9 <> '' THEN $9 ELSE metadata_source END,
			  metadata_confidence=COALESCE($10, metadata_confidence),
			  mbid=CASE WHEN $11 <> '' THEN $11 ELSE mbid END,
			  updated_at=now()
			WHERE id=$1 AND locked=false`,
			trackID, title, albumID, max1(probe.Disc), probe.Track, probe.DurationMS, probe.Year, probe.Genre, probe.Source, confOrNil(probe.Confidence), probe.MBID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1 AND role='primary' AND EXISTS (SELECT 1 FROM tracks WHERE id=$1 AND locked=false)`, trackID)
	} else {
		err = s.pool.QueryRow(ctx, `
			INSERT INTO tracks (library_id, album_id, title, disc_number, track_number, duration_ms, year, genre_text, metadata_source, metadata_confidence, mbid)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,0),$8,$9,$10,NULLIF($11,'')) RETURNING id`,
			libID, albumID, title, max1(probe.Disc), probe.Track, probe.DurationMS, probe.Year, probe.Genre, probe.Source, confOrNil(probe.Confidence), probe.MBID).Scan(&trackID)
		if err != nil {
			return err
		}
	}
	if acqRef != "" {
		_, _ = s.pool.Exec(ctx, `
			UPDATE tracks SET
			  acquisition=CASE WHEN acquisition='' THEN $2 ELSE acquisition END,
			  acquisition_ref=CASE WHEN $3 <> '' THEN $3 ELSE acquisition_ref END,
			  media_unavailable_at=NULL,
			  updated_at=now()
			WHERE id=$1`, trackID, acq, acqRef)
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',0) ON CONFLICT DO NOTHING`, trackID, trackArtistID)
	if probe.Composer != "" {
		cid, _ := s.upsertArtist(ctx, probe.Composer)
		_, _ = s.pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'composer',0) ON CONFLICT DO NOTHING`, trackID, cid)
	}

	var fileID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, codec, container, bitrate, sample_rate, channels, bit_depth, quality, replaygain_track_gain, replaygain_track_peak, replaygain_album_gain, replaygain_album_peak, encoder_delay, encoder_padding, original_size_bytes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,0),'original',$12,$13,$14,$15,NULLIF($16,0),NULLIF($17,0),$18)
		ON CONFLICT (library_id, storage_key) DO UPDATE SET
		  size_bytes=EXCLUDED.size_bytes, content_hash=EXCLUDED.content_hash, codec=EXCLUDED.codec,
		  bitrate=EXCLUDED.bitrate, sample_rate=EXCLUDED.sample_rate, channels=EXCLUDED.channels,
		  bit_depth=EXCLUDED.bit_depth, replaygain_track_gain=EXCLUDED.replaygain_track_gain,
		  replaygain_track_peak=EXCLUDED.replaygain_track_peak, replaygain_album_gain=EXCLUDED.replaygain_album_gain,
		  replaygain_album_peak=EXCLUDED.replaygain_album_peak, encoder_delay=EXCLUDED.encoder_delay,
		  encoder_padding=EXCLUDED.encoder_padding, original_size_bytes=EXCLUDED.original_size_bytes
		RETURNING id`,
		trackID, libID, e.Key, size, hash, probe.Codec, probe.Container, probe.Bitrate, probe.SampleRate, probe.Channels,
		probe.BitDepth, probe.ReplayGainTrack, probe.ReplayGainTrackPeak, probe.ReplayGainAlbum, probe.ReplayGainAlbumPeak,
		probe.EncoderDelay, probe.EncoderPadding, origSizePtr(origSize, size)).Scan(&fileID)
	if err != nil {
		return err
	}

	s.insertLyrics(ctx, trackID, probe)
	s.writeDuplicateGroup(ctx, fileID, hash)
	if companionPath != "" && companionKey != "" && !opts.ReadOnly {
		if f, oerr := os.Open(companionPath); oerr == nil {
			werr := prov.Write(ctx, companionKey, f, storage.WriteInfo{Size: companionSize})
			f.Close()
			if werr == nil {
				ch, _ := hashFile(companionPath)
				_, _ = s.pool.Exec(ctx, `
					INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, codec, container, bitrate, sample_rate, channels, bit_depth, quality, original_size_bytes)
					VALUES ($1,$2,$3,$4,$5,'flac','flac',$6,$7,$8,$9,$10,$11)
					ON CONFLICT (library_id, storage_key) DO UPDATE SET
					  size_bytes=EXCLUDED.size_bytes, content_hash=EXCLUDED.content_hash, original_size_bytes=EXCLUDED.original_size_bytes`,
					trackID, libID, companionKey, companionSize, ch, probe.Bitrate, probe.SampleRate, probe.Channels, probe.BitDepth,
					transcode.QualityCompressed, origSize)
			}
		}
	}
	if organise.ShouldPhysical(opts.OrgMode, opts.AllowPhysical, opts.ReadOnly) && prov.Capabilities().Write {
		s.maybeReorganise(ctx, libID, prov, fileID, &e, probe, opts.Template)
	}
	if s.boolSetting(ctx, waveform.SettingEnabled, true) {
		s.enqueueOnce(ctx, waveform.JobName, map[string]any{"track_id": trackID, "track_file_id": fileID})
	}
	if s.boolSetting(ctx, fingerprint.SettingEnabled, true) {
		s.enqueueOnce(ctx, fingerprint.JobName, map[string]any{"track_id": trackID, "track_file_id": fileID})
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

func (s *Scanner) IngestKey(ctx context.Context, libID uuid.UUID, prov storage.StorageProvider, key, originalName string) error {
	info, err := prov.Stat(ctx, key)
	if err != nil {
		return err
	}
	return s.ingestFile(ctx, libID, prov, storage.Entry{Key: key, Size: info.Size, ModTime: info.ModTime}, originalName, "", s.knownArtistFn(ctx))
}

func (s *Scanner) knownArtistFn(ctx context.Context) func(string) bool {
	cache := map[string]bool{}
	return func(name string) bool {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" || key == "unknown artist" {
			return false
		}
		if cache[key] {
			return true
		}
		var id uuid.UUID
		err := s.pool.QueryRow(ctx, `SELECT id FROM artists WHERE lower(name)=lower($1) LIMIT 1`, name).Scan(&id)
		if err != nil {
			return false
		}
		cache[key] = true
		return true
	}
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
	if t != "" && !metadata.LooksLikeHash(t) {
		return t
	}
	_, title := metadata.ParseAudioName(key)
	if title != "" {
		return title
	}
	return "Unknown Title"
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
