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
	TrackID uuid.UUID
	Expires time.Time
	Quality string
}

func Sign(key []byte, trackID uuid.UUID, ttl time.Duration, quality string) string {
	if quality == "" {
		quality = "original"
	}
	exp := time.Now().Add(ttl).Unix()
	msg := fmt.Sprintf("%s|%d|%s", trackID, exp, quality)
	mac := cryptox.HMAC(key, []byte(msg))
	return fmt.Sprintf("%s.%d.%s.%s", trackID.String(), exp, quality, mac)
}

func Verify(key []byte, raw string) (Token, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 4 {
		return Token{}, fmt.Errorf("malformed token")
	}
	tid, err := uuid.Parse(parts[0])
	if err != nil {
		return Token{}, err
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Token{}, err
	}
	if time.Now().Unix() > exp {
		return Token{}, fmt.Errorf("expired")
	}
	quality := parts[2]
	msg := fmt.Sprintf("%s|%d|%s", tid, exp, quality)
	if !cryptox.HMACEqual(key, msg, parts[3]) {
		return Token{}, fmt.Errorf("bad signature")
	}
	return Token{TrackID: tid, Expires: time.Unix(exp, 0), Quality: quality}, nil
}
