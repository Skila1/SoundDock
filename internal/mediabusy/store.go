package mediabusy

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	KindHTTPStream  = "http_stream"
	KindHMACStream  = "hmac_stream"
	KindDiscord     = "discord"
	KindReplace     = "replace"
	KindAcquire     = "acquire"
	HoldTTL         = 45 * time.Second
	HoldHeartbeat   = 15 * time.Second
)

func InstanceID() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("api:%s:%d", host, os.Getpid())
}

func (s *Set) SetPool(pool *pgxpool.Pool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.pool = pool
	if s.instance == "" {
		s.instance = InstanceID()
	}
	s.mu.Unlock()
}

func NewHolder(kind string) string {
	switch kind {
	case KindHMACStream:
		return "hmac:" + uuid.NewString()
	case KindDiscord:
		return "discord:" + uuid.NewString()
	case KindReplace:
		return "replace:" + uuid.NewString()
	case KindAcquire:
		return "acquire:" + uuid.NewString()
	default:
		return "http:" + uuid.NewString()
	}
}

// Acquire stores a durable hold. holderID must be unique per concurrent lease.
func (s *Set) Acquire(ctx context.Context, trackID uuid.UUID, kind, holderID string) func() {
	local := s.Hold(trackID)
	if s == nil || s.pool == nil || trackID == uuid.Nil || holderID == "" {
		return local
	}
	s.upsert(ctx, trackID, kind, holderID)
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		local()
		s.Release(ctx, kind, holderID)
	}
}

func (s *Set) upsert(ctx context.Context, trackID uuid.UUID, kind, holderID string) {
	if s == nil || s.pool == nil {
		return
	}
	inst := s.instance
	if inst == "" {
		inst = InstanceID()
	}
	secs := int(HoldTTL.Seconds())
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO media_holds (track_id, kind, holder_id, instance_id, lease_until, heartbeat_at)
		VALUES ($1,$2,$3,$4, now() + make_interval(secs => $5), now())
		ON CONFLICT (kind, holder_id) DO UPDATE SET
		  track_id=EXCLUDED.track_id,
		  lease_until=EXCLUDED.lease_until,
		  heartbeat_at=now()`,
		trackID, kind, holderID, inst, secs)
}

func (s *Set) Heartbeat(ctx context.Context, kind, holderID string) {
	if s == nil || s.pool == nil || holderID == "" {
		return
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE media_holds SET lease_until=now() + make_interval(secs => $3), heartbeat_at=now()
		WHERE kind=$1 AND holder_id=$2`, kind, holderID, int(HoldTTL.Seconds()))
}

func (s *Set) Release(ctx context.Context, kind, holderID string) {
	if s == nil || s.pool == nil || holderID == "" {
		return
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM media_holds WHERE kind=$1 AND holder_id=$2`, kind, holderID)
}

func (s *Set) UpdateTrack(ctx context.Context, kind, holderID string, trackID uuid.UUID) {
	if s == nil || s.pool == nil || holderID == "" || trackID == uuid.Nil {
		return
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE media_holds SET track_id=$3, lease_until=now() + make_interval(secs => $4), heartbeat_at=now()
		WHERE kind=$1 AND holder_id=$2`, kind, holderID, trackID, int(HoldTTL.Seconds()))
}

func (s *Set) Sweep(ctx context.Context) {
	if s == nil || s.pool == nil {
		return
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM media_holds WHERE lease_until < now()`)
}

func (s *Set) Busy(ctx context.Context, trackID uuid.UUID) bool {
	if s != nil && s.Contains(trackID) {
		return true
	}
	if s == nil || s.pool == nil || trackID == uuid.Nil {
		return false
	}
	var ok bool
	_ = s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM media_holds WHERE track_id=$1 AND lease_until > now())`, trackID).Scan(&ok)
	return ok
}
