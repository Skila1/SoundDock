package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"golang.org/x/crypto/argon2"
)

const (
	dekSize                = 32
	wrapMagic              = "SDWK"
	wrapVersion       byte = 1
	MinPassphrase          = 12
	kdfTimeDefault         = 3
	kdfMemoryDefault       = 64 * 1024
	kdfThreadsDefault      = 4
	kdfSaltSize            = 16
)

var (
	ErrPassphraseRequired = errors.New("recovery passphrase is not set")
	ErrPassphraseShort    = errors.New("recovery passphrase must be at least 12 characters")
	ErrWrongPassphrase    = errors.New("recovery passphrase is incorrect")
	ErrWrapCorrupt        = errors.New("recovery box is corrupt")
	ErrNoMaster           = errors.New("master key is not configured")
)

// KDFParams are stored in backup_destination JSON and copied into the archive header.
type KDFParams struct {
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	Salt    []byte `json:"salt"`
}

func defaultKDF() KDFParams {
	salt := make([]byte, kdfSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		panic(err)
	}
	return KDFParams{
		Time:    kdfTimeDefault,
		Memory:  kdfMemoryDefault,
		Threads: kdfThreadsDefault,
		Salt:    salt,
	}
}

func newDEK() ([]byte, error) {
	dek := make([]byte, dekSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}
	return dek, nil
}

func boxDEK(box *cryptox.Box, dek []byte) ([]byte, error) {
	if box == nil {
		return nil, ErrNoMaster
	}
	return box.Encrypt(dek)
}

func unboxDEK(box *cryptox.Box, enc []byte) ([]byte, error) {
	if box == nil {
		return nil, ErrNoMaster
	}
	dek, err := box.Decrypt(enc)
	if err != nil {
		return nil, err
	}
	if len(dek) != dekSize {
		return nil, fmt.Errorf("dek_enc is the wrong size")
	}
	return dek, nil
}

// wrapRecovery boxes DEK || masterKey with Argon2id(passphrase).
func wrapRecovery(passphrase string, dek, master []byte, kdf KDFParams) ([]byte, error) {
	if len([]rune(passphrase)) < MinPassphrase {
		return nil, ErrPassphraseShort
	}
	if len(dek) != dekSize {
		return nil, fmt.Errorf("dek must be %d bytes", dekSize)
	}
	if len(kdf.Salt) == 0 {
		return nil, fmt.Errorf("kdf salt is required")
	}
	key := argon2.IDKey([]byte(passphrase), kdf.Salt, kdf.Time, kdf.Memory, kdf.Threads, dekSize)
	block, err := aes.NewCipher(key)
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
	plain := append(append([]byte{}, dek...), master...)
	sealed := gcm.Seal(nil, nonce, plain, []byte(wrapMagic))
	out := make([]byte, 6+len(nonce)+len(sealed))
	copy(out[:4], wrapMagic)
	out[4] = wrapVersion
	out[5] = byte(len(nonce))
	copy(out[6:6+len(nonce)], nonce)
	copy(out[6+len(nonce):], sealed)
	return out, nil
}

// unwrapRecovery returns DEK and master key from recovery.box.
func unwrapRecovery(passphrase string, box []byte, kdf KDFParams) (dek, master []byte, err error) {
	if len([]rune(passphrase)) < MinPassphrase {
		return nil, nil, ErrPassphraseShort
	}
	if len(box) < 6 {
		return nil, nil, ErrWrapCorrupt
	}
	if string(box[:4]) != wrapMagic || box[4] != wrapVersion {
		return nil, nil, ErrWrapCorrupt
	}
	ns := int(box[5])
	if ns < 12 || len(box) < 6+ns+16 {
		return nil, nil, ErrWrapCorrupt
	}
	nonce := box[6 : 6+ns]
	sealed := box[6+ns:]
	key := argon2.IDKey([]byte(passphrase), kdf.Salt, kdf.Time, kdf.Memory, kdf.Threads, dekSize)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	plain, err := gcm.Open(nil, nonce, sealed, []byte(wrapMagic))
	if err != nil {
		return nil, nil, ErrWrongPassphrase
	}
	if len(plain) < dekSize {
		return nil, nil, ErrWrapCorrupt
	}
	return append([]byte{}, plain[:dekSize]...), append([]byte{}, plain[dekSize:]...), nil
}
