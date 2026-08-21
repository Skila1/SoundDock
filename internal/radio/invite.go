package radio

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

const inviteTTL = 7 * 24 * time.Hour

func SignInvite(key []byte, playlistID uuid.UUID, ttl time.Duration) (string, time.Time, error) {
	if len(key) == 0 {
		return "", time.Time{}, fmt.Errorf("signing key missing")
	}
	if ttl <= 0 {
		ttl = inviteTTL
	}
	exp := time.Now().Add(ttl)
	msg := fmt.Sprintf("%s|%d|playlist-invite", playlistID, exp.Unix())
	mac := cryptox.HMAC(key, []byte(msg))
	tok := fmt.Sprintf("%s.%d.%s", playlistID.String(), exp.Unix(), mac)
	return tok, exp, nil
}

func VerifyInvite(key []byte, raw string) (uuid.UUID, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return uuid.Nil, fmt.Errorf("malformed invite")
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, err
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return uuid.Nil, err
	}
	if time.Now().Unix() > exp {
		return uuid.Nil, fmt.Errorf("expired")
	}
	msg := fmt.Sprintf("%s|%d|playlist-invite", id, exp)
	if !cryptox.HMACEqual(key, msg, parts[2]) {
		return uuid.Nil, fmt.Errorf("bad signature")
	}
	return id, nil
}
