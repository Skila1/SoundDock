package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/fingerprint"
	"github.com/sounddock/sounddock/internal/integrity"
	"github.com/sounddock/sounddock/internal/metadata"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/watch"
	"github.com/sounddock/sounddock/internal/waveform"
)

func (s *Server) libraryHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = fingerprint.EnsureSchema(ctx, s.Pool)
	var trashed, groups int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM track_files WHERE deleted_at IS NOT NULL`).Scan(&trashed)
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM duplicate_groups`).Scan(&groups)
	writeJSON(w, 200, map[string]any{
		"fingerprint":               fingerprint.Availability(),
		"ffmpeg":                    transcode.FFmpegAvailable(),
		"ffprobe":                   transcode.FFProbeAvailable(),
		"waveform_enabled":          s.boolSetting(r, waveform.SettingEnabled, true),
		"fingerprint_enabled":       s.boolSetting(r, fingerprint.SettingEnabled, true),
		"watch_enabled":             s.boolSetting(r, watch.SettingWatch, false),
		"auto_rescan_enabled":       s.boolSetting(r, watch.SettingAutoRescan, false),
		"inbox_watch_enabled":       s.boolSetting(r, watch.SettingInbox, false),
		"keep_original":             s.boolSetting(r, "keep_original", false),
		"compression_preset":        s.stringSetting(r, "compression_preset", transcode.PresetStandard),
		"metadata_external_enabled": metadata.ExternalEnabled(ctx, s.Pool),
		"trashed_files":             trashed,
		"duplicate_groups":          groups,
	})
}

func (s *Server) getTrackWaveform(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid track id")
		return
	}
	if !s.requireTrackLibrary(w, r, id, "read") {
		return
	}
	var peaks []byte
	err = s.Pool.QueryRow(r.Context(), `SELECT waveform_peaks FROM tracks WHERE id=$1`, id).Scan(&peaks)
	if err != nil {
		writeErr(w, 404, "not_found", "track not found")
		return
	}
	writeJSON(w, 200, map[string]any{
		"track_id": id,
		"peaks":    jsonRawOrNil(peaks),
		"ready":    len(peaks) > 2 && string(peaks) != "null",
	})
}

func (s *Server) libraryOrphans(w http.ResponseWriter, r *http.Request) {
	lib, err := uuid.Parse(r.URL.Query().Get("library_id"))
	if err != nil {
		writeErr(w, 400, "invalid", "library_id required")
		return
	}
	svc := integrity.New(s.Pool, s.ProviderFor)
	orphans, err := svc.Orphans(r.Context(), lib)
	if err != nil {
		writeErr(w, 500, "integrity", err.Error())
		return
	}
	if orphans == nil {
		orphans = []string{}
	}
	writeJSON(w, 200, map[string]any{"library_id": lib, "orphans": orphans, "count": len(orphans)})
}

func (s *Server) libraryFilesRemoved(w http.ResponseWriter, r *http.Request) {
	var lib uuid.UUID
	if q := r.URL.Query().Get("library_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			writeErr(w, 400, "invalid", "invalid library_id")
			return
		}
		lib = id
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	svc := integrity.New(s.Pool, s.ProviderFor)
	rows, err := svc.FilesRemoved(r.Context(), lib, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"files": rows})
}

func (s *Server) libraryIntegrityScan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LibraryID uuid.UUID `json:"library_id"`
	}
	_ = decodeJSON(r, &body)
	if body.LibraryID == uuid.Nil {
		if q := r.URL.Query().Get("library_id"); q != "" {
			body.LibraryID, _ = uuid.Parse(q)
		}
	}
	if body.LibraryID == uuid.Nil {
		writeErr(w, 400, "invalid", "library_id required")
		return
	}
	id, err := s.Jobs.Enqueue(r.Context(), integrity.JobName, integrity.Payload{LibraryID: body.LibraryID})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true, "job_id": id, "job": integrity.JobName})
}

func (s *Server) libraryTrashFile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid file id")
		return
	}
	svc := integrity.New(s.Pool, s.ProviderFor)
	key, err := svc.Trash(r.Context(), id)
	if err != nil {
		writeErr(w, 400, "trash", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "storage_key": key})
}

func (s *Server) libraryRestoreFile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid file id")
		return
	}
	svc := integrity.New(s.Pool, s.ProviderFor)
	key, err := svc.Restore(r.Context(), id)
	if err == integrity.ErrConflict {
		writeErr(w, 409, "conflict", "original storage key is not free")
		return
	}
	if err != nil {
		writeErr(w, 400, "restore", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "storage_key": key})
}

func (s *Server) libraryDuplicateGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT g.id::text, g.method, g.created_at,
			COALESCE(json_agg(json_build_object(
				'track_file_id', tf.id, 'track_id', tf.track_id, 'storage_key', tf.storage_key,
				'quality', tf.quality, 'size_bytes', tf.size_bytes, 'content_hash', tf.content_hash
			) ORDER BY tf.created_at), '[]'::json)
		FROM duplicate_groups g
		JOIN duplicates d ON d.group_id=g.id
		JOIN track_files tf ON tf.id=d.track_file_id
		WHERE tf.deleted_at IS NULL
		GROUP BY g.id, g.method, g.created_at
		ORDER BY g.created_at DESC
		LIMIT 200`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "method", "created_at", "files"))
}

func (s *Server) libraryIngestSettings(w http.ResponseWriter, r *http.Request) {
	s.libraryHealth(w, r)
}

func (s *Server) libraryPutIngestSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WaveformEnabled         *bool   `json:"waveform_enabled"`
		FingerprintEnabled      *bool   `json:"fingerprint_enabled"`
		WatchEnabled            *bool   `json:"watch_enabled"`
		AutoRescanEnabled       *bool   `json:"auto_rescan_enabled"`
		InboxWatchEnabled       *bool   `json:"inbox_watch_enabled"`
		KeepOriginal            *bool   `json:"keep_original"`
		KeepOriginalLibraryID   *string `json:"keep_original_library_id"`
		CompressionPreset       *string `json:"compression_preset"`
		MetadataExternalEnabled *bool   `json:"metadata_external_enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	setBool := func(key string, v *bool) {
		if v == nil {
			return
		}
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO server_settings (key, value) VALUES ($1, to_jsonb($2::bool)) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, key, *v)
	}
	setBool(waveform.SettingEnabled, body.WaveformEnabled)
	setBool(fingerprint.SettingEnabled, body.FingerprintEnabled)
	setBool(watch.SettingWatch, body.WatchEnabled)
	setBool(watch.SettingAutoRescan, body.AutoRescanEnabled)
	setBool(watch.SettingInbox, body.InboxWatchEnabled)
	setBool("keep_original", body.KeepOriginal)
	setBool(metadata.SettingMetadataExternal, body.MetadataExternalEnabled)
	if body.KeepOriginalLibraryID != nil && body.KeepOriginal != nil {
		if id, err := uuid.Parse(*body.KeepOriginalLibraryID); err == nil {
			setBool("keep_original."+id.String(), body.KeepOriginal)
		}
	}
	if body.CompressionPreset != nil {
		p := transcode.NormalizePreset(*body.CompressionPreset)
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO server_settings (key, value) VALUES ('compression_preset', to_jsonb($1::text)) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, p)
	}
	s.libraryHealth(w, r)
}

func (s *Server) boolSetting(r *http.Request, key string, def bool) bool {
	if s.Pool == nil {
		return def
	}
	var v bool
	err := s.Pool.QueryRow(r.Context(), `SELECT (value)::boolean FROM server_settings WHERE key=$1`, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

func (s *Server) stringSetting(r *http.Request, key, def string) string {
	if s.Pool == nil {
		return def
	}
	var v string
	err := s.Pool.QueryRow(r.Context(), `SELECT value #>> '{}' FROM server_settings WHERE key=$1`, key).Scan(&v)
	if err != nil || v == "" {
		return def
	}
	return v
}

func jsonRawOrNil(b []byte) any {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return json.RawMessage(b)
}
