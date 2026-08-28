package playback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CommandReceipt is the stored result of an idempotent Control call.
type CommandReceipt struct {
	CommandID         string
	Action            string
	RequestHash       string
	ResultStatus      int
	ResultCode        string
	ResultingRevision *int64
	ResultingItemID   *uuid.UUID
	ResultJSON        []byte
	Replay            bool
}

func requestHash(action string, extra map[string]any) string {
	filtered := map[string]any{}
	for k, v := range extra {
		if k == "command_id" {
			continue
		}
		filtered[k] = v
	}
	b, _ := json.Marshal(struct {
		Action string         `json:"action"`
		Extra  map[string]any `json:"extra"`
	}{Action: action, Extra: filtered})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func uniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (e *Engine) GetCommandReceipt(ctx context.Context, sessionID uuid.UUID, commandID string) (CommandReceipt, bool, error) {
	return loadReceipt(ctx, e.pool, sessionID, commandID)
}

func loadReceipt(ctx context.Context, q db, sessionID uuid.UUID, commandID string) (CommandReceipt, bool, error) {
	var rec CommandReceipt
	var code *string
	var resultJSON []byte
	err := q.QueryRow(ctx, `
		SELECT command_id, action, request_hash, result_status, result_code,
			resulting_revision, resulting_item_id, result_json
		FROM playback_command_receipts WHERE session_id=$1 AND command_id=$2`, sessionID, commandID).
		Scan(&rec.CommandID, &rec.Action, &rec.RequestHash, &rec.ResultStatus, &code,
			&rec.ResultingRevision, &rec.ResultingItemID, &resultJSON)
	if err == pgx.ErrNoRows {
		return CommandReceipt{}, false, nil
	}
	if err != nil {
		return CommandReceipt{}, false, err
	}
	if code != nil {
		rec.ResultCode = *code
	}
	rec.ResultJSON = resultJSON
	return rec, true, nil
}

func insertReceipt(ctx context.Context, q db, sessionID uuid.UUID, rec CommandReceipt) error {
	_, err := q.Exec(ctx, `
		INSERT INTO playback_command_receipts (
			session_id, command_id, action, request_hash, result_status, result_code,
			resulting_revision, resulting_item_id, result_json
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		sessionID, rec.CommandID, rec.Action, rec.RequestHash, rec.ResultStatus, nullIfEmpty(rec.ResultCode),
		rec.ResultingRevision, rec.ResultingItemID, rec.ResultJSON)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func receiptError(rec CommandReceipt) error {
	if rec.ResultStatus >= 400 {
		if rec.ResultCode != "" {
			if rec.ResultCode == ErrCommandConflict.Error() {
				return ErrCommandConflict
			}
			return fmt.Errorf("%s", rec.ResultCode)
		}
		return fmt.Errorf("command failed")
	}
	return nil
}

func currentQueueItemID(ctx context.Context, q db, sid uuid.UUID) *uuid.UUID {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT q.id FROM playback_queue_items q
		JOIN playback_sessions s ON s.id=q.session_id AND q.position=s.current_index
		WHERE s.id=$1`, sid).Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}

func currentStateRevision(ctx context.Context, q db, sid uuid.UUID) *int64 {
	var rev int64
	if err := q.QueryRow(ctx, `SELECT state_revision FROM playback_sessions WHERE id=$1`, sid).Scan(&rev); err != nil {
		return nil
	}
	return &rev
}

func commandIDOf(extra map[string]any) string {
	return strings.TrimSpace(extraString(extra, "command_id"))
}
