package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (e *Engine) Control(ctx context.Context, sid uuid.UUID, action string, extra map[string]any) error {
	extra = normalizeControlExtra(action, extra)
	if action == "skip" || action == "next" {
		// Fill before the session lock so radio/YouTube work cannot stall pause/skip.
		e.runAutoplayFiller(ctx, sid)
	}
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockSessionRow(ctx, tx, sid); err != nil {
		return err
	}

	commandID := commandIDOf(extra)
	hash := ""
	if commandID != "" {
		hash = requestHash(action, extra)
		rec, ok, err := loadReceipt(ctx, tx, sid, commandID)
		if err != nil {
			return err
		}
		if ok {
			if rec.RequestHash != hash {
				return ErrCommandConflict
			}
			rec.Replay = true
			if err := receiptError(rec); err != nil {
				return err
			}
			applyUndoFromReceipt(extra, rec)
			return nil
		}
	}

	mutated, ctrlErr := e.controlTx(ctx, tx, sid, action, extra)
	if ctrlErr != nil {
		return ctrlErr
	}
	if mutated {
		if err := bumpRevision(ctx, tx, sid); err != nil {
			return err
		}
	}
	finalizeUndoExtra(ctx, tx, sid, extra)

	if commandID != "" {
		rev := currentStateRevision(ctx, tx, sid)
		itemID := currentQueueItemID(ctx, tx, sid)
		payloadMap := map[string]any{"state_revision": rev}
		if u, ok := extra["undo"]; ok {
			payloadMap["undo"] = u
		}
		if g, ok := extra["undo_generation"]; ok {
			payloadMap["undo_generation"] = g
		}
		payload, _ := json.Marshal(payloadMap)
		rec := CommandReceipt{
			CommandID:         commandID,
			Action:            action,
			RequestHash:       hash,
			ResultStatus:      200,
			ResultCode:        "ok",
			ResultingRevision: rev,
			ResultingItemID:   itemID,
			ResultJSON:        payload,
		}
		if err := insertReceipt(ctx, tx, sid, rec); err != nil {
			if uniqueViolation(err) {
				_ = tx.Rollback(ctx)
				existing, ok, loadErr := loadReceipt(ctx, e.pool, sid, commandID)
				if loadErr != nil {
					return loadErr
				}
				if ok {
					if existing.RequestHash != hash {
						return ErrCommandConflict
					}
					existing.Replay = true
					if err := receiptError(existing); err != nil {
						return err
					}
					applyUndoFromReceipt(extra, existing)
					return nil
				}
				return err
			}
			return err
		}
	}
	return e.commitSession(ctx, tx, sid, "session.state")
}

func (e *Engine) controlTx(ctx context.Context, tx db, sid uuid.UUID, action string, extra map[string]any) (bool, error) {
	mutated := false
	if v, ok := extraBool(extra, "stop_after_current"); ok {
		if _, err := tx.Exec(ctx, `UPDATE playback_sessions SET stop_after_current=$2, updated_at=now() WHERE id=$1`, sid, v); err != nil {
			return false, err
		}
		mutated = true
	}
	if mode := extraString(extra, "shuffle_mode"); mode != "" {
		switch mode {
		case "smart", "random", "album":
			if _, err := tx.Exec(ctx, `UPDATE playback_sessions SET shuffle_mode=$2, updated_at=now() WHERE id=$1`, sid, mode); err != nil {
				return false, err
			}
			mutated = true
		}
	}
	if ms, ok := extraInt(extra, "position_ms"); ok && action != "seek" && action != "stop" && action != "pause" {
		if _, err := tx.Exec(ctx, `UPDATE playback_sessions SET position_ms=$2, updated_at=now() WHERE id=$1`, sid, ms); err != nil {
			return false, err
		}
	}
	ended, _ := extraBool(extra, "ended")
	switch action {
	case "pause":
		ms, hasPos := extraInt(extra, "position_ms")
		if hasPos {
			if ms < 0 {
				ms = 0
			}
			_, err := tx.Exec(ctx, `
				UPDATE playback_sessions
				SET status='paused', position_ms=$2, `+sqlStampPlayhead+`, updated_at=now()
				WHERE id=$1`, sid, ms)
			return true, err
		}
		_, err := tx.Exec(ctx, `
			UPDATE playback_sessions
			SET status='paused', `+sqlStampPlayhead+`, updated_at=now()
			WHERE id=$1`, sid)
		return true, err
	case "resume", "play":
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET status='playing', updated_at=now() WHERE id=$1`, sid)
		return true, err
	case "stop":
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET status='stopped', position_ms=0, stop_after_current=false, updated_at=now() WHERE id=$1`, sid)
		return true, err
	case "clear":
		if v, ok := extraBool(extra, "all"); ok && v {
			removed, err := e.loadQueueRows(ctx, tx, sid)
			if err != nil {
				return false, err
			}
			setUndoItems(extra, removed)
			if _, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid); err != nil {
				return false, err
			}
			_, err = tx.Exec(ctx, `UPDATE playback_sessions SET `+sqlEmptySession+`, updated_at=now() WHERE id=$1`, sid)
			return true, err
		}
		var idx int
		if err := tx.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&idx); err != nil {
			return false, err
		}
		upcoming, err := e.loadQueueRowsWhere(ctx, tx, sid, `AND position>$2`, idx)
		if err != nil {
			return false, err
		}
		setUndoItems(extra, upcoming)
		if err := e.clearKeepCurrent(ctx, tx, sid); err != nil {
			return false, err
		}
		return true, nil
	case "shuffle":
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET shuffle = NOT shuffle, updated_at=now() WHERE id=$1`, sid)
		return true, err
	case "repeat":
		mode := extraString(extra, "mode")
		if mode == "" {
			mode = "queue"
		}
		switch mode {
		case "off", "queue", "one":
		default:
			return mutated, fmt.Errorf("invalid repeat mode")
		}
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET repeat_mode=$2, updated_at=now() WHERE id=$1`, sid, mode)
		return true, err
	case "volume":
		vol, ok := extra["volume"].(float64)
		if !ok {
			if i, ok := extraInt(extra, "volume"); ok {
				vol = float64(i)
			}
		}
		if vol < 0 {
			vol = 0
		}
		if vol > 1 {
			vol = 1
		}
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET volume=$2, updated_at=now() WHERE id=$1`, sid, vol)
		return true, err
	case "mute":
		_, err := tx.Exec(ctx, `
			UPDATE playback_sessions
			SET muted=true, volume_restore=CASE WHEN muted THEN volume_restore ELSE volume END, updated_at=now()
			WHERE id=$1`, sid)
		return true, err
	case "unmute":
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET muted=false, updated_at=now() WHERE id=$1`, sid)
		return true, err
	case "autoplay":
		v, ok := extraBool(extra, "autoplay")
		if !ok {
			v, ok = extraBool(extra, "enabled")
		}
		if !ok {
			return mutated, fmt.Errorf("autoplay required")
		}
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET autoplay=$2, updated_at=now() WHERE id=$1`, sid, v)
		return true, err
	case "output_pref":
		pref := extraString(extra, "output_pref")
		if pref == "" {
			pref = extraString(extra, "pref")
		}
		switch pref {
		case OutputBrowser, OutputDiscord:
		default:
			return mutated, fmt.Errorf("invalid output_pref")
		}
		if pref == OutputDiscord {
			if _, err := casReleaseBrowserIfHeld(ctx, tx, sid); err != nil {
				return false, err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET output_pref=$2, updated_at=now() WHERE id=$1`, sid, pref)
		return true, err
	case "seek":
		ms, _ := extraInt(extra, "position_ms")
		if ms < 0 {
			ms = 0
		}
		_, err := tx.Exec(ctx, `
			UPDATE playback_sessions
			SET position_ms=$2, `+sqlStampPlayhead+`, updated_at=now()
			WHERE id=$1`, sid, ms)
		return true, err
	case "add":
		tracks := extraUUIDs(extra, "track_ids")
		if len(tracks) == 0 {
			return mutated, nil
		}
		next, _ := extraBool(extra, "next")
		if err := addTracksTx(ctx, tx, sid, tracks, next); err != nil {
			return false, err
		}
		return true, nil
	case "stop_after_current":
		return mutated, nil
	case "skip", "next":
		changed, err := e.move(ctx, tx, sid, 1, ended)
		return changed || mutated, err
	case "previous":
		changed, err := e.move(ctx, tx, sid, -1, false)
		return changed || mutated, err
	case "index":
		idx, ok := extraInt(extra, "index")
		if !ok {
			return mutated, fmt.Errorf("index required")
		}
		items, err := e.queueMeta(ctx, tx, sid)
		if err != nil {
			return false, err
		}
		if idx < 0 || idx >= len(items) {
			return mutated, fmt.Errorf("index out of range")
		}
		_, err = tx.Exec(ctx, `
			UPDATE playback_sessions
			SET current_index=$2, current_track_id=$3, status='playing',
				`+sqlNewInstance+`,
				duration_ms=COALESCE((SELECT t.duration_ms FROM tracks t WHERE t.id=$3), 0),
				updated_at=now()
			WHERE id=$1`, sid, idx, items[idx].TrackID)
		return true, err
	case "remove":
		pos, _ := extraInt(extra, "position")
		removed, err := e.loadQueueRowsWhere(ctx, tx, sid, `AND position=$2`, pos)
		if err != nil {
			return false, err
		}
		setUndoItems(extra, removed)
		changed, err := e.removeAt(ctx, tx, sid, pos)
		return changed || mutated, err
	case "undo":
		changed, err := e.restoreUndo(ctx, tx, sid, extra)
		return changed || mutated, err
	case "reorder":
		from, okFrom := extraInt(extra, "from")
		to, okTo := extraInt(extra, "to")
		if !okFrom || !okTo {
			return mutated, fmt.Errorf("reorder requires from and to")
		}
		changed, err := e.reorder(ctx, tx, sid, from, to)
		return changed || mutated, err
	case "replace":
		tracks := extraUUIDs(extra, "track_ids")
		start, _ := extraInt(extra, "start")
		if err := replaceQueueTx(ctx, tx, sid, tracks, start); err != nil {
			return false, err
		}
		return true, nil
	default:
		return mutated, fmt.Errorf("unknown action")
	}
}

func (e *Engine) clearKeepCurrent(ctx context.Context, tx db, sid uuid.UUID) error {
	var idx int
	var tid *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT current_index, current_track_id FROM playback_sessions WHERE id=$1`, sid).Scan(&idx, &tid); err != nil {
		return err
	}
	if tid == nil || *tid == uuid.Nil {
		if _, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET current_index=0, current_track_id=NULL, updated_at=now() WHERE id=$1`, sid)
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1 AND position<>$2`, sid, idx); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM playback_queue_items WHERE session_id=$1`, sid).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO playback_queue_items (session_id, position, track_id) VALUES ($1, 0, $2)`, sid, *tid); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=0 WHERE session_id=$1`, sid); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE playback_sessions SET current_index=0, current_track_id=$2, updated_at=now() WHERE id=$1`, sid, *tid)
	return err
}

func (e *Engine) move(ctx context.Context, tx db, sid uuid.UUID, delta int, ended bool) (bool, error) {
	var idx int
	var repeat, mode string
	var shuffle, stopAfter bool
	if err := tx.QueryRow(ctx, `
		SELECT current_index, repeat_mode, shuffle, shuffle_mode, stop_after_current
		FROM playback_sessions WHERE id=$1`, sid).Scan(&idx, &repeat, &shuffle, &mode, &stopAfter); err != nil {
		return false, err
	}
	if stopAfter && delta > 0 {
		_, err := tx.Exec(ctx, `UPDATE playback_sessions SET status='stopped', stop_after_current=false, updated_at=now() WHERE id=$1`, sid)
		return true, err
	}
	items, err := e.queueMeta(ctx, tx, sid)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, nil
	}
	next, stop := nextIndex(items, idx, delta, repeat, mode, shuffle, ended, e.intn)
	if stop {
		_, err = tx.Exec(ctx, `UPDATE playback_sessions SET status='stopped', stop_after_current=false, updated_at=now() WHERE id=$1`, sid)
		return true, err
	}
	tid := items[next].TrackID
	_, err = tx.Exec(ctx, `
		UPDATE playback_sessions
		SET current_index=$2, current_track_id=$3, status='playing',
			`+sqlNewInstance+`,
			duration_ms=COALESCE((SELECT t.duration_ms FROM tracks t WHERE t.id=$3), 0),
			updated_at=now()
		WHERE id=$1`, sid, next, tid)
	return true, err
}

func (e *Engine) queueMeta(ctx context.Context, q db, sid uuid.UUID) ([]queueMeta, error) {
	rows, err := q.Query(ctx, `
		SELECT q.position, q.track_id,
			coalesce(t.album_id, '00000000-0000-0000-0000-000000000000'),
			coalesce(t.disc_number, 1), coalesce(t.track_number, 0),
			coalesce((
				SELECT ta.artist_id FROM track_artists ta
				WHERE ta.track_id=q.track_id AND ta.role='primary'
				ORDER BY ta.position LIMIT 1
			), '00000000-0000-0000-0000-000000000000')
		FROM playback_queue_items q
		LEFT JOIN tracks t ON t.id=q.track_id
		WHERE q.session_id=$1
		ORDER BY q.position`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []queueMeta
	for rows.Next() {
		var it queueMeta
		if err := rows.Scan(&it.Position, &it.TrackID, &it.AlbumID, &it.Disc, &it.TrackNo, &it.ArtistID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (e *Engine) removeAt(ctx context.Context, tx db, sid uuid.UUID, pos int) (bool, error) {
	var cur int
	if err := tx.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&cur); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1 AND position=$2`, sid, pos)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=position-1 WHERE session_id=$1 AND position>$2`, sid, pos); err != nil {
		return false, err
	}
	newIdx := cur
	if pos < cur {
		newIdx = cur - 1
	}
	if newIdx < 0 {
		newIdx = 0
	}
	var tid uuid.UUID
	err = tx.QueryRow(ctx, `SELECT track_id FROM playback_queue_items WHERE session_id=$1 AND position=$2`, sid, newIdx).Scan(&tid)
	if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `UPDATE playback_sessions SET `+sqlEmptySession+`, updated_at=now() WHERE id=$1`, sid)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if pos == cur {
		_, err = tx.Exec(ctx, `
			UPDATE playback_sessions
			SET current_index=$2, current_track_id=$3,
				`+sqlNewInstance+`,
				duration_ms=COALESCE((SELECT t.duration_ms FROM tracks t WHERE t.id=$3), 0),
				updated_at=now()
			WHERE id=$1`, sid, newIdx, tid)
	} else {
		_, err = tx.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, updated_at=now() WHERE id=$1`, sid, newIdx, tid)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (e *Engine) reorder(ctx context.Context, tx db, sid uuid.UUID, from, to int) (bool, error) {
	items, err := e.queue(ctx, tx, sid)
	if err != nil {
		return false, err
	}
	n := len(items)
	if from < 0 || from >= n || to < 0 || to >= n {
		return false, fmt.Errorf("invalid reorder")
	}
	if from == to {
		return false, nil
	}
	type row struct {
		id    uuid.UUID
		track uuid.UUID
	}
	rows := make([]row, n)
	for i, it := range items {
		rows[i] = row{id: it["id"].(uuid.UUID), track: it["track_id"].(uuid.UUID)}
	}
	moved := rows[from]
	rest := append(append([]row{}, rows[:from]...), rows[from+1:]...)
	movedRows := append(append([]row{}, rest[:to]...), append([]row{moved}, rest[to:]...)...)

	var cur int
	if err := tx.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&cur); err != nil {
		return false, err
	}
	newIdx := cur
	if cur == from {
		newIdx = to
	} else if from < cur && to >= cur {
		newIdx = cur - 1
	} else if from > cur && to <= cur {
		newIdx = cur + 1
	}

	for i, r := range movedRows {
		if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=$2 WHERE id=$1`, r.id, i); err != nil {
			return false, err
		}
	}
	var tid uuid.UUID
	if newIdx >= 0 && newIdx < len(movedRows) {
		tid = movedRows[newIdx].track
	}
	if _, err := tx.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, updated_at=now() WHERE id=$1`, sid, newIdx, tid); err != nil {
		return false, err
	}
	return true, nil
}

const undoItemsKey = "__undo_items"

type undoItem struct {
	ID                       uuid.UUID
	TrackID                  uuid.UUID
	Position                 int
	Origin                   string
	RequestedByUserID        *uuid.UUID
	RequestedByDiscordUserID *string
}

func setUndoItems(extra map[string]any, items []undoItem) {
	if extra == nil || len(items) == 0 {
		return
	}
	extra[undoItemsKey] = items
}

func finalizeUndoExtra(ctx context.Context, q db, sid uuid.UUID, extra map[string]any) {
	if extra == nil {
		return
	}
	items, ok := extra[undoItemsKey].([]undoItem)
	delete(extra, undoItemsKey)
	if !ok || len(items) == 0 {
		return
	}
	var gen int64
	if r := currentStateRevision(ctx, q, sid); r != nil {
		gen = *r
	}
	maps := make([]map[string]any, len(items))
	for i, it := range items {
		maps[i] = it.toMap()
	}
	extra["undo"] = map[string]any{"undo_generation": gen, "items": maps}
	extra["undo_generation"] = gen
}

func applyUndoFromReceipt(extra map[string]any, rec CommandReceipt) {
	if extra == nil || len(rec.ResultJSON) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.ResultJSON, &payload); err != nil {
		return
	}
	if u, ok := payload["undo"]; ok {
		extra["undo"] = u
	}
	if g, ok := payload["undo_generation"]; ok {
		extra["undo_generation"] = g
	}
}

func (it undoItem) toMap() map[string]any {
	m := map[string]any{
		"id":       it.ID.String(),
		"track_id": it.TrackID.String(),
		"position": it.Position,
		"origin":   it.Origin,
	}
	if it.RequestedByUserID != nil && *it.RequestedByUserID != uuid.Nil {
		m["requested_by_user_id"] = it.RequestedByUserID.String()
	}
	if it.RequestedByDiscordUserID != nil && strings.TrimSpace(*it.RequestedByDiscordUserID) != "" {
		m["requested_by_discord_user_id"] = strings.TrimSpace(*it.RequestedByDiscordUserID)
	}
	if rb := requestedByMap(it.RequestedByUserID, it.RequestedByDiscordUserID, nil); rb != nil {
		m["requested_by"] = rb
	}
	return m
}

func (e *Engine) loadQueueRows(ctx context.Context, q db, sid uuid.UUID) ([]undoItem, error) {
	return e.loadQueueRowsWhere(ctx, q, sid, "")
}

func (e *Engine) loadQueueRowsWhere(ctx context.Context, q db, sid uuid.UUID, where string, args ...any) ([]undoItem, error) {
	query := `
		SELECT id, position, track_id, coalesce(origin, 'user'),
			requested_by_user_id, requested_by_discord_user_id
		FROM playback_queue_items
		WHERE session_id=$1 ` + where + `
		ORDER BY position`
	allArgs := append([]any{sid}, args...)
	rows, err := q.Query(ctx, query, allArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []undoItem
	for rows.Next() {
		var it undoItem
		if err := rows.Scan(&it.ID, &it.Position, &it.TrackID, &it.Origin, &it.RequestedByUserID, &it.RequestedByDiscordUserID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (e *Engine) restoreUndo(ctx context.Context, tx db, sid uuid.UUID, extra map[string]any) (bool, error) {
	var rev int64
	var curTrack *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT state_revision, current_track_id FROM playback_sessions WHERE id=$1`, sid).Scan(&rev, &curTrack); err != nil {
		return false, err
	}
	gen, ok := undoGenerationOf(extra)
	if !ok || gen != rev {
		return false, ErrUndoStale
	}
	snap := parseUndoItems(extra)
	if len(snap) == 0 {
		return false, ErrUndoStale
	}
	existing, err := e.loadQueueRows(ctx, tx, sid)
	if err != nil {
		return false, err
	}
	desired := mergeUndoRows(existing, snap)
	if err := e.replaceQueueRows(ctx, tx, sid, desired); err != nil {
		return false, err
	}
	newIdx := 0
	var newTrack any
	if curTrack != nil && *curTrack != uuid.Nil {
		found := false
		for i, r := range desired {
			if r.TrackID == *curTrack {
				newIdx = i
				newTrack = r.TrackID
				found = true
				break
			}
		}
		if !found && len(desired) > 0 {
			newTrack = desired[0].TrackID
		}
	} else if len(desired) > 0 {
		newTrack = desired[0].TrackID
	}
	if len(desired) == 0 {
		newTrack = nil
		newIdx = 0
	}
	_, err = tx.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, updated_at=now() WHERE id=$1`, sid, newIdx, newTrack)
	return true, err
}

func mergeUndoRows(existing, snap []undoItem) []undoItem {
	byID := map[uuid.UUID]struct{}{}
	out := append([]undoItem(nil), existing...)
	for _, r := range out {
		if r.ID != uuid.Nil {
			byID[r.ID] = struct{}{}
		}
	}
	sort.SliceStable(snap, func(i, j int) bool { return snap[i].Position < snap[j].Position })
	for _, it := range snap {
		if it.TrackID == uuid.Nil {
			continue
		}
		if it.ID != uuid.Nil {
			if _, ok := byID[it.ID]; ok {
				continue
			}
		}
		idx := it.Position
		if idx < 0 {
			idx = 0
		}
		if idx > len(out) {
			idx = len(out)
		}
		out = append(out[:idx], append([]undoItem{it}, out[idx:]...)...)
		if it.ID != uuid.Nil {
			byID[it.ID] = struct{}{}
		}
	}
	for i := range out {
		out[i].Position = i
	}
	return out
}

func (e *Engine) replaceQueueRows(ctx context.Context, tx db, sid uuid.UUID, rows []undoItem) error {
	if _, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid); err != nil {
		return err
	}
	used := map[uuid.UUID]struct{}{}
	for i, it := range rows {
		id := it.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		if _, ok := used[id]; ok {
			id = uuid.New()
		}
		used[id] = struct{}{}
		origin := it.Origin
		if origin == "" {
			origin = OriginUser
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO playback_queue_items (id, session_id, position, track_id, origin, requested_by_user_id, requested_by_discord_user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, sid, i, it.TrackID, origin, nullUUID(it.RequestedByUserID), nullString(it.RequestedByDiscordUserID))
		if err != nil && uniqueViolation(err) {
			id = uuid.New()
			used[id] = struct{}{}
			_, err = tx.Exec(ctx, `
				INSERT INTO playback_queue_items (id, session_id, position, track_id, origin, requested_by_user_id, requested_by_discord_user_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				id, sid, i, it.TrackID, origin, nullUUID(it.RequestedByUserID), nullString(it.RequestedByDiscordUserID))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func nullUUID(id *uuid.UUID) any {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return *id
}

func nullString(s *string) any {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return strings.TrimSpace(*s)
}

func undoGenerationOf(extra map[string]any) (int64, bool) {
	if n, ok := extraInt64Val(extra, "undo_generation"); ok {
		return n, true
	}
	if u, ok := extra["undo"].(map[string]any); ok {
		if n, ok := extraInt64Val(u, "undo_generation"); ok {
			return n, true
		}
		if n, ok := extraInt64Val(u, "generation"); ok {
			return n, true
		}
	}
	return 0, false
}

func extraInt64Val(extra map[string]any, key string) (int64, bool) {
	n, ok := extraInt(extra, key)
	if !ok {
		return 0, false
	}
	return int64(n), true
}

func parseUndoItems(extra map[string]any) []undoItem {
	raw := extraSlice(extra, "items")
	if len(raw) == 0 {
		if u, ok := extra["undo"].(map[string]any); ok {
			raw = extraSlice(u, "items")
		}
	}
	var out []undoItem
	for _, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		it, ok := parseUndoItem(m)
		if !ok {
			continue
		}
		out = append(out, it)
	}
	return out
}

func extraSlice(extra map[string]any, key string) []any {
	if extra == nil {
		return nil
	}
	switch v := extra[key].(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, len(v))
		for i, m := range v {
			out[i] = m
		}
		return out
	}
	return nil
}

func parseUndoItem(m map[string]any) (undoItem, bool) {
	tid := parseAnyUUID(m["track_id"])
	if tid == uuid.Nil {
		return undoItem{}, false
	}
	pos, _ := extraInt(m, "position")
	origin := extraString(m, "origin")
	if origin == "" {
		origin = OriginUser
	}
	it := undoItem{
		ID:       parseAnyUUID(m["id"]),
		TrackID:  tid,
		Position: pos,
		Origin:   origin,
	}
	if uid := parseAnyUUID(m["requested_by_user_id"]); uid != uuid.Nil {
		it.RequestedByUserID = &uid
	}
	if s := extraString(m, "requested_by_discord_user_id"); s != "" {
		it.RequestedByDiscordUserID = &s
	}
	if rb, ok := m["requested_by"].(map[string]any); ok {
		if it.RequestedByUserID == nil {
			if uid := parseAnyUUID(rb["user_id"]); uid != uuid.Nil {
				it.RequestedByUserID = &uid
			}
		}
		if it.RequestedByDiscordUserID == nil {
			if s := extraString(rb, "discord_user_id"); s != "" {
				it.RequestedByDiscordUserID = &s
			}
		}
	}
	return it, true
}

func parseAnyUUID(v any) uuid.UUID {
	switch t := v.(type) {
	case uuid.UUID:
		return t
	case *uuid.UUID:
		if t != nil {
			return *t
		}
	case string:
		id, err := uuid.Parse(strings.TrimSpace(t))
		if err == nil {
			return id
		}
	}
	return uuid.Nil
}
