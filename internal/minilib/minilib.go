package minilib

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Owner struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	DiscordUserID string
	Visibility    string
}

type EntryMeta struct {
	FirstRequestedAt time.Time
	LastRequestedAt  time.Time
	RequestCount     int
}

func NormalizeVisibility(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "public") {
		return "public"
	}
	return "private"
}

func CanSee(viewerID uuid.UUID, admin bool, owner Owner) bool {
	if owner.ID == uuid.Nil {
		return false
	}
	if viewerID != uuid.Nil && owner.UserID != uuid.Nil && viewerID == owner.UserID {
		return true
	}
	if owner.Visibility == "public" {
		return true
	}
	return admin
}

func Inspecting(viewerID uuid.UUID, admin bool, owner Owner) bool {
	if !admin {
		return false
	}
	if viewerID != uuid.Nil && owner.UserID == viewerID {
		return false
	}
	return owner.Visibility != "public"
}

func Record(ctx context.Context, db DB, origin string, userID uuid.UUID, discordID string, trackIDs []uuid.UUID) error {
	if db == nil || len(trackIDs) == 0 {
		return nil
	}
	if origin != "" && origin != "user" {
		return nil
	}
	owner, err := EnsureOwner(ctx, db, userID, discordID)
	if err != nil || owner.ID == uuid.Nil {
		return err
	}
	for _, id := range trackIDs {
		if id == uuid.Nil {
			continue
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO personal_library_entries (owner_id, track_id, first_requested_at, last_requested_at, request_count)
			VALUES ($1,$2,now(),now(),1)
			ON CONFLICT (owner_id, track_id) DO UPDATE SET
				last_requested_at = now(),
				request_count = personal_library_entries.request_count + 1`, owner.ID, id); err != nil {
			return err
		}
	}
	return nil
}

func EnsureOwner(ctx context.Context, db DB, userID uuid.UUID, discordID string) (Owner, error) {
	discordID = strings.TrimSpace(discordID)
	if userID == uuid.Nil && discordID == "" {
		return Owner{}, nil
	}
	if userID != uuid.Nil {
		o, err := ownerByUser(ctx, db, userID)
		if err == nil {
			if discordID != "" && o.DiscordUserID == "" {
				_, _ = db.Exec(ctx, `
					UPDATE personal_library_owners
					SET discord_user_id=$2, updated_at=now()
					WHERE id=$1 AND discord_user_id IS NULL
					  AND NOT EXISTS (SELECT 1 FROM personal_library_owners x WHERE x.discord_user_id=$2)`,
					o.ID, discordID)
				o.DiscordUserID = discordID
			}
			return o, nil
		}
		if err != pgx.ErrNoRows {
			return Owner{}, err
		}
	}
	if discordID != "" {
		o, err := ownerByDiscord(ctx, db, discordID)
		if err == nil {
			if userID != uuid.Nil && o.UserID == uuid.Nil {
				_, _ = db.Exec(ctx, `
					UPDATE personal_library_owners
					SET user_id=$2, updated_at=now()
					WHERE id=$1 AND user_id IS NULL
					  AND NOT EXISTS (SELECT 1 FROM personal_library_owners x WHERE x.user_id=$2)`,
					o.ID, userID)
				o.UserID = userID
			}
			return o, nil
		}
		if err != pgx.ErrNoRows {
			return Owner{}, err
		}
	}
	vis := "private"
	if userID != uuid.Nil {
		_ = db.QueryRow(ctx, `SELECT personal_library_visibility FROM users WHERE id=$1`, userID).Scan(&vis)
		vis = NormalizeVisibility(vis)
	}
	var uid any
	var did any
	if userID != uuid.Nil {
		uid = userID
	}
	if discordID != "" {
		did = discordID
	}
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO personal_library_owners (user_id, discord_user_id, visibility)
		VALUES ($1,$2,$3)
		RETURNING id`, uid, did, vis).Scan(&id)
	if err != nil {
		if userID != uuid.Nil {
			if o, e := ownerByUser(ctx, db, userID); e == nil {
				return o, nil
			}
		}
		if discordID != "" {
			if o, e := ownerByDiscord(ctx, db, discordID); e == nil {
				return o, nil
			}
		}
		return Owner{}, err
	}
	return Owner{ID: id, UserID: userID, DiscordUserID: discordID, Visibility: vis}, nil
}

func ownerByUser(ctx context.Context, db DB, userID uuid.UUID) (Owner, error) {
	var o Owner
	var did *string
	err := db.QueryRow(ctx, `
		SELECT id, coalesce(user_id, '00000000-0000-0000-0000-000000000000'::uuid), discord_user_id, visibility
		FROM personal_library_owners WHERE user_id=$1`, userID).Scan(&o.ID, &o.UserID, &did, &o.Visibility)
	if err != nil {
		return Owner{}, err
	}
	if did != nil {
		o.DiscordUserID = *did
	}
	return o, nil
}

func ownerByDiscord(ctx context.Context, db DB, discordID string) (Owner, error) {
	var o Owner
	var uid *uuid.UUID
	err := db.QueryRow(ctx, `
		SELECT id, user_id, coalesce(discord_user_id,''), visibility
		FROM personal_library_owners WHERE discord_user_id=$1`, discordID).Scan(&o.ID, &uid, &o.DiscordUserID, &o.Visibility)
	if err != nil {
		return Owner{}, err
	}
	if uid != nil {
		o.UserID = *uid
	}
	return o, nil
}

func OwnerByUser(ctx context.Context, db DB, userID uuid.UUID) (Owner, error) {
	return ownerByUser(ctx, db, userID)
}

func OwnerByDiscord(ctx context.Context, db DB, discordID string) (Owner, error) {
	return ownerByDiscord(ctx, db, strings.TrimSpace(discordID))
}

func Reconcile(ctx context.Context, db DB, userID uuid.UUID, discordID string) error {
	discordID = strings.TrimSpace(discordID)
	if userID == uuid.Nil || discordID == "" {
		return nil
	}
	a, aerr := ownerByUser(ctx, db, userID)
	b, berr := ownerByDiscord(ctx, db, discordID)
	if aerr != nil && aerr != pgx.ErrNoRows {
		return aerr
	}
	if berr != nil && berr != pgx.ErrNoRows {
		return berr
	}
	if aerr == pgx.ErrNoRows && berr == pgx.ErrNoRows {
		_, err := EnsureOwner(ctx, db, userID, discordID)
		return err
	}
	if aerr == pgx.ErrNoRows {
		if b.UserID != uuid.Nil && b.UserID != userID {
			return nil
		}
		_, err := db.Exec(ctx, `UPDATE personal_library_owners SET user_id=$2, updated_at=now() WHERE id=$1 AND (user_id IS NULL OR user_id=$2)`, b.ID, userID)
		return err
	}
	if berr == pgx.ErrNoRows {
		_, err := db.Exec(ctx, `
			UPDATE personal_library_owners
			SET discord_user_id=$2, updated_at=now()
			WHERE id=$1 AND discord_user_id IS NULL
			  AND NOT EXISTS (SELECT 1 FROM personal_library_owners x WHERE x.discord_user_id=$2)`, a.ID, discordID)
		return err
	}
	if a.ID == b.ID {
		return nil
	}
	if b.UserID != uuid.Nil && b.UserID != userID {
		return nil
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO personal_library_entries (owner_id, track_id, first_requested_at, last_requested_at, request_count)
		SELECT $1, track_id, first_requested_at, last_requested_at, request_count
		FROM personal_library_entries WHERE owner_id=$2
		ON CONFLICT (owner_id, track_id) DO UPDATE SET
			first_requested_at = LEAST(personal_library_entries.first_requested_at, EXCLUDED.first_requested_at),
			last_requested_at = GREATEST(personal_library_entries.last_requested_at, EXCLUDED.last_requested_at),
			request_count = personal_library_entries.request_count + EXCLUDED.request_count`, a.ID, b.ID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `DELETE FROM personal_library_owners WHERE id=$1`, b.ID); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE personal_library_owners
		SET discord_user_id=$2, updated_at=now()
		WHERE id=$1 AND (discord_user_id IS NULL OR discord_user_id=$2)`, a.ID, discordID)
	return err
}

func DetachDiscord(ctx context.Context, db DB, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return nil
	}
	_, err := db.Exec(ctx, `UPDATE personal_library_owners SET discord_user_id=NULL, updated_at=now() WHERE user_id=$1`, userID)
	return err
}

func SetVisibility(ctx context.Context, db DB, userID uuid.UUID, vis string) error {
	vis = NormalizeVisibility(vis)
	if _, err := db.Exec(ctx, `UPDATE users SET personal_library_visibility=$2, updated_at=now() WHERE id=$1`, userID, vis); err != nil {
		return err
	}
	o, err := EnsureOwner(ctx, db, userID, "")
	if err != nil {
		return err
	}
	if o.ID == uuid.Nil {
		return nil
	}
	_, err = db.Exec(ctx, `UPDATE personal_library_owners SET visibility=$2, updated_at=now() WHERE id=$1`, o.ID, vis)
	return err
}

func LinkedUserID(ctx context.Context, db DB, discordID string) uuid.UUID {
	discordID = strings.TrimSpace(discordID)
	if discordID == "" {
		return uuid.Nil
	}
	var id uuid.UUID
	_ = db.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider='discord' AND provider_user_id=$1`, discordID).Scan(&id)
	return id
}
