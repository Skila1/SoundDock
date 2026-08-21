package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var ErrNoKey = errors.New("master key is not configured")

type Box struct {
	key []byte
}

func New(master string) (*Box, error) {
	if master == "" {
		return &Box{}, nil
	}
	sum := sha256.Sum256([]byte(master))
	return &Box{key: sum[:]}, nil
}

func (b *Box) Encrypt(plain []byte) ([]byte, error) {
	if len(b.key) == 0 {
		return nil, ErrNoKey
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (b *Box) Decrypt(ct []byte) ([]byte, error) {
	if len(b.key) == 0 {
		return nil, ErrNoKey
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ct) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return gcm.Open(nil, ct[:ns], ct[ns:], nil)
}

func HMAC(key, msg []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func HMACEqual(key []byte, msg, mac string) bool {
	want := HMAC(key, []byte(msg))
	return hmac.Equal([]byte(want), []byte(mac))
}

func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func SigningKey(master string) []byte {
	sum := sha256.Sum256([]byte("sounddock-stream|" + master))
	return sum[:]
}
