package stream

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

type Token struct {
	UserID  uuid.UUID
	TrackID uuid.UUID
	Expires time.Time
	Quality string
}

func Sign(key []byte, userID, trackID uuid.UUID, ttl time.Duration, quality string) string {
	if userID == uuid.Nil {
		return ""
	}
	if quality == "" {
		quality = "original"
	}
	exp := time.Now().Add(ttl).Unix()
	msg := fmt.Sprintf("stream-v2|%s|%s|%d|%s", userID, trackID, exp, quality)
	mac := cryptox.HMAC(key, []byte(msg))
	return fmt.Sprintf("%s.%s.%d.%s.%s", userID.String(), trackID.String(), exp, quality, mac)
}

func Verify(key []byte, raw string) (Token, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 5 {
		return Token{}, fmt.Errorf("malformed token")
	}
	uid, err := uuid.Parse(parts[0])
	if err != nil || uid == uuid.Nil {
		return Token{}, fmt.Errorf("malformed token")
	}
	tid, err := uuid.Parse(parts[1])
	if err != nil {
		return Token{}, err
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Token{}, err
	}
	if time.Now().Unix() > exp {
		return Token{}, fmt.Errorf("expired")
	}
	quality := parts[3]
	msg := fmt.Sprintf("stream-v2|%s|%s|%d|%s", uid, tid, exp, quality)
	if !cryptox.HMACEqual(key, msg, parts[4]) {
		return Token{}, fmt.Errorf("bad signature")
	}
	return Token{UserID: uid, TrackID: tid, Expires: time.Unix(exp, 0), Quality: quality}, nil
}
