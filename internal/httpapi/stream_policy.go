package httpapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

const (
	settingRemoteBitrate     = "stream_remote_max_bitrate"
	settingLANBitrate        = "stream_lan_max_bitrate"
	settingRemoteConcurrency = "stream_remote_concurrency"
	defaultRemoteBitrateKbps = 192
	defaultLANBitrateKbps    = 0
	DefaultRemoteConcurrency = 8
	offlineTokenTTL          = 30 * 24 * time.Hour
	offlineTokenPrefix       = "offline."
)

// StreamPolicy is the LAN vs remote envelope for a request.
// Classify with r.RemoteAddr only after proxyHeaders has run. Unknown / unparseable
// addresses fail closed to remote.
type StreamPolicy struct {
	LAN            bool   `json:"lan"`
	MaxBitrateKbps int    `json:"max_bitrate_kbps"`
	MaxConcurrency int    `json:"max_concurrency"`
	Quality        string `json:"quality"`
}

type OfflineClaims struct {
	UserID   uuid.UUID
	DeviceID string
	TrackID  uuid.UUID
	IssuedAt time.Time
	Expires  time.Time
}

var (
	policySlotMu  sync.Mutex
	policySlotCur = map[string]int{}
)

// IsLANAddr reports whether addr (host or host:port) is loopback, RFC1918, or
// link-local. Nil / unparseable addresses are remote (fail closed).
func IsLANAddr(addr string) bool {
	ip := parseRemoteIP(addr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func parseRemoteIP(addr string) net.IP {
	host := strings.TrimSpace(addr)
	if host == "" {
		return nil
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return net.ParseIP(host)
}

func remoteIPString(r *http.Request) string {
	ip := parseRemoteIP(r.RemoteAddr)
	if ip == nil {
		return r.RemoteAddr
	}
	return ip.String()
}

func (s *Server) PolicyFor(r *http.Request) StreamPolicy {
	lan := IsLANAddr(r.RemoteAddr)
	remoteBr, lanBr, remoteN := s.loadStreamSettings(r.Context())
	p := StreamPolicy{LAN: lan}
	if lan {
		p.MaxBitrateKbps = lanBr
		p.MaxConcurrency = 0
		p.Quality = qualityForMaxBitrate(lanBr)
		return p
	}
	p.MaxBitrateKbps = remoteBr
	p.MaxConcurrency = remoteN
	p.Quality = qualityForMaxBitrate(remoteBr)
	return p
}

func qualityForMaxBitrate(kbps int) string {
	if kbps <= 0 {
		return "original"
	}
	best := "low"
	for _, c := range []struct {
		name string
		kbps int
	}{{"low", 96}, {"medium", 192}, {"high", 320}} {
		if c.kbps <= kbps {
			best = c.name
		}
	}
	return best
}

var qualityRank = map[string]int{"low": 1, "medium": 2, "high": 3, "original": 4}

func minQuality(a, b string) string {
	if qualityRank[a] == 0 {
		a = "original"
	}
	if qualityRank[b] == 0 {
		b = "original"
	}
	if qualityRank[a] <= qualityRank[b] {
		return a
	}
	return b
}

// CapStreamQuality returns the more restricted of the request and the LAN/remote cap.
func (s *Server) CapStreamQuality(r *http.Request, requested string) string {
	if requested == "" {
		requested = "original"
	}
	return minQuality(requested, s.PolicyFor(r).Quality)
}

// AcquireStreamSlot enforces remote concurrency from server_settings. LAN is unlimited
// here; integrator may keep s.Slots as a process-wide backstop (see WIREUP).
func (s *Server) AcquireStreamSlot(r *http.Request) bool {
	if IsLANAddr(r.RemoteAddr) {
		return true
	}
	limit := s.streamSetting(r.Context(), settingRemoteConcurrency, DefaultRemoteConcurrency)
	if limit <= 0 {
		limit = DefaultRemoteConcurrency
	}
	key := "remote:" + remoteIPString(r)
	policySlotMu.Lock()
	defer policySlotMu.Unlock()
	if policySlotCur[key] >= limit {
		return false
	}
	policySlotCur[key]++
	return true
}

func (s *Server) ReleaseStreamSlot(r *http.Request) {
	if IsLANAddr(r.RemoteAddr) {
		return
	}
	key := "remote:" + remoteIPString(r)
	policySlotMu.Lock()
	defer policySlotMu.Unlock()
	policySlotCur[key]--
	if policySlotCur[key] <= 0 {
		delete(policySlotCur, key)
	}
}

func (s *Server) loadStreamSettings(ctx context.Context) (remoteBr, lanBr, remoteN int) {
	remoteBr = s.streamSetting(ctx, settingRemoteBitrate, defaultRemoteBitrateKbps)
	lanBr = s.streamSetting(ctx, settingLANBitrate, defaultLANBitrateKbps)
	remoteN = s.streamSetting(ctx, settingRemoteConcurrency, DefaultRemoteConcurrency)
	if remoteN <= 0 {
		remoteN = DefaultRemoteConcurrency
	}
	return
}

func (s *Server) streamSetting(ctx context.Context, key string, def int) int {
	if s.Pool == nil {
		return def
	}
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT (value #>> '{}')::int FROM server_settings WHERE key=$1`, key).Scan(&n)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) putStreamSetting(ctx context.Context, key string, n int) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, to_jsonb($2::int))
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, key, n)
	return err
}

func (s *Server) adminGetStreamPolicy(w http.ResponseWriter, r *http.Request) {
	remoteBr, lanBr, remoteN := s.loadStreamSettings(r.Context())
	writeJSON(w, 200, map[string]any{
		"remote_max_bitrate":    remoteBr,
		"lan_max_bitrate":       lanBr,
		"remote_concurrency":    remoteN,
		"lan_concurrency":       0,
		"client_is_lan":         IsLANAddr(r.RemoteAddr),
		"fail_closed_to_remote": true,
	})
}

func (s *Server) adminPutStreamPolicy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RemoteMaxBitrate  *int `json:"remote_max_bitrate"`
		LANMaxBitrate     *int `json:"lan_max_bitrate"`
		RemoteConcurrency *int `json:"remote_concurrency"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", "invalid body")
		return
	}
	if body.RemoteMaxBitrate != nil {
		if *body.RemoteMaxBitrate < 0 {
			writeErr(w, 400, "invalid", "remote_max_bitrate must be >= 0")
			return
		}
		if err := s.putStreamSetting(r.Context(), settingRemoteBitrate, *body.RemoteMaxBitrate); err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
	}
	if body.LANMaxBitrate != nil {
		if *body.LANMaxBitrate < 0 {
			writeErr(w, 400, "invalid", "lan_max_bitrate must be >= 0")
			return
		}
		if err := s.putStreamSetting(r.Context(), settingLANBitrate, *body.LANMaxBitrate); err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
	}
	if body.RemoteConcurrency != nil {
		if *body.RemoteConcurrency < 1 {
			writeErr(w, 400, "invalid", "remote_concurrency must be >= 1")
			return
		}
		if err := s.putStreamSetting(r.Context(), settingRemoteConcurrency, *body.RemoteConcurrency); err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
	}
	if s.Audit != nil {
		s.Audit.Event(r.Context(), &currentUser(r).ID, "stream.policy", "", r.RemoteAddr, nil)
	}
	s.adminGetStreamPolicy(w, r)
}

func (s *Server) mintOfflineToken(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		writeErr(w, 401, "unauthorized", "login required")
		return
	}
	var body struct {
		TrackID  uuid.UUID `json:"track_id"`
		DeviceID string    `json:"device_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", "invalid body")
		return
	}
	deviceID, ok := sanitizeDeviceID(body.DeviceID)
	if !ok || body.TrackID == uuid.Nil {
		writeErr(w, 400, "invalid", "track_id and device_id required")
		return
	}
	var libID uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT library_id FROM tracks WHERE id=$1`, body.TrackID).Scan(&libID); err != nil {
		writeErr(w, 404, "not_found", "track not found")
		return
	}
	if !s.userHasLibraryAction(r.Context(), u, libID, "stream") {
		writeErr(w, http.StatusForbidden, "library_grant", "library stream not granted")
		return
	}
	issued := time.Now()
	exp := issued.Add(offlineTokenTTL)
	tok, err := s.signOffline(u.ID, deviceID, body.TrackID, issued, exp)
	if err != nil {
		writeErr(w, 500, "token", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"token":      tok,
		"track_id":   body.TrackID,
		"device_id":  deviceID,
		"user_id":    u.ID,
		"expires_at": exp.UTC().Format(time.RFC3339),
		"url":        "/api/v1/tracks/" + body.TrackID.String() + "/stream?offline_token=" + tok,
	})
}

func (s *Server) revokeOfflineTokens(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		writeErr(w, 401, "unauthorized", "login required")
		return
	}
	var body struct {
		DeviceID string `json:"device_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", "invalid body")
		return
	}
	deviceID, ok := sanitizeDeviceID(body.DeviceID)
	if !ok {
		writeErr(w, 400, "invalid", "device_id required")
		return
	}
	key := offlineRevokeKey(u.ID, deviceID)
	if err := s.putStreamSetting(r.Context(), key, int(time.Now().Unix())); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func offlineRevokeKey(userID uuid.UUID, deviceID string) string {
	return "offline_revoke:" + userID.String() + ":" + deviceID
}

func sanitizeDeviceID(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if len(id) < 1 || len(id) > 128 {
		return "", false
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ':' {
			continue
		}
		return "", false
	}
	return id, true
}

func (s *Server) signOffline(userID uuid.UUID, deviceID string, trackID uuid.UUID, issued, exp time.Time) (string, error) {
	if len(s.SignKey) == 0 {
		return "", fmt.Errorf("signing key missing")
	}
	msg := offlineMsg(userID, deviceID, trackID, issued.Unix(), exp.Unix())
	mac := cryptox.HMAC(s.SignKey, []byte(msg))
	payload := fmt.Sprintf("%s.%s.%s.%d.%d", userID, deviceID, trackID, issued.Unix(), exp.Unix())
	return offlineTokenPrefix + payload + "." + mac, nil
}

func offlineMsg(userID uuid.UUID, deviceID string, trackID uuid.UUID, issued, exp int64) string {
	return fmt.Sprintf("offline-v1|%s|%s|%s|%d|%d", userID, deviceID, trackID, issued, exp)
}

func (s *Server) VerifyOfflineToken(ctx context.Context, raw string) (OfflineClaims, error) {
	if !strings.HasPrefix(raw, offlineTokenPrefix) {
		return OfflineClaims{}, fmt.Errorf("not an offline token")
	}
	parts := strings.Split(strings.TrimPrefix(raw, offlineTokenPrefix), ".")
	if len(parts) != 6 {
		return OfflineClaims{}, fmt.Errorf("malformed token")
	}
	userID, err := uuid.Parse(parts[0])
	if err != nil {
		return OfflineClaims{}, err
	}
	deviceID, ok := sanitizeDeviceID(parts[1])
	if !ok {
		return OfflineClaims{}, fmt.Errorf("bad device")
	}
	trackID, err := uuid.Parse(parts[2])
	if err != nil {
		return OfflineClaims{}, err
	}
	issued, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return OfflineClaims{}, err
	}
	exp, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return OfflineClaims{}, err
	}
	if time.Now().Unix() > exp {
		return OfflineClaims{}, fmt.Errorf("expired")
	}
	msg := offlineMsg(userID, deviceID, trackID, issued, exp)
	if !cryptox.HMACEqual(s.SignKey, msg, parts[5]) {
		return OfflineClaims{}, fmt.Errorf("bad signature")
	}
	if s.revokedOffline(ctx, userID, deviceID, issued) {
		return OfflineClaims{}, fmt.Errorf("revoked")
	}
	return OfflineClaims{
		UserID:   userID,
		DeviceID: deviceID,
		TrackID:  trackID,
		IssuedAt: time.Unix(issued, 0),
		Expires:  time.Unix(exp, 0),
	}, nil
}

func (s *Server) revokedOffline(ctx context.Context, userID uuid.UUID, deviceID string, issued int64) bool {
	watermark := s.streamSetting(ctx, offlineRevokeKey(userID, deviceID), 0)
	return watermark > 0 && issued <= int64(watermark)
}
