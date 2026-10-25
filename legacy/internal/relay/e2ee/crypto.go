package e2ee

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	Suite              = "p256-ecdsa+p256-ecdh+hkdf-sha256+aes-256-gcm"
	Version     uint16 = 1
	NonceLength        = 12
	KeyLength          = 32

	DirectionClientToAgent = "client_to_agent"
	DirectionAgentToClient = "agent_to_client"
)

var (
	ErrInvalidDirection = errors.New("invalid e2ee direction")
	ErrInvalidLength    = errors.New("invalid e2ee key length")
	ErrInvalidAADField  = errors.New("invalid e2ee aad field")
)

type FrameContext struct {
	SessionID   string
	ClientID    string
	HandshakeID string
	Direction   string
	StreamID    uint64
	Counter     uint64
}

func PublicKeyFromPrivate(privateScalar []byte) ([]byte, error) {
	privateKey, err := ecdh.P256().NewPrivateKey(privateScalar)
	if err != nil {
		return nil, fmt.Errorf("p256 private key: %w", err)
	}
	return privateKey.PublicKey().Bytes(), nil
}

func SharedSecret(privateScalar, remotePublicKey []byte) ([]byte, error) {
	privateKey, err := ecdh.P256().NewPrivateKey(privateScalar)
	if err != nil {
		return nil, fmt.Errorf("p256 private key: %w", err)
	}
	publicKey, err := ecdh.P256().NewPublicKey(remotePublicKey)
	if err != nil {
		return nil, fmt.Errorf("p256 public key: %w", err)
	}
	secret, err := privateKey.ECDH(publicKey)
	if err != nil {
		return nil, fmt.Errorf("p256 ecdh: %w", err)
	}
	return secret, nil
}

func Fingerprint(publicKey []byte) []byte {
	sum := sha256.Sum256(publicKey)
	return sum[:]
}

func ShortFingerprint(fingerprint []byte) string {
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(fingerprint)
	if len(encoded) > 20 {
		encoded = encoded[:20]
	}
	var groups []string
	for len(encoded) > 0 {
		n := min(4, len(encoded))
		groups = append(groups, encoded[:n])
		encoded = encoded[n:]
	}
	return strings.Join(groups, "-")
}

func DeriveTrafficKey(sharedSecret []byte, ctx FrameContext) ([]byte, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	salt := TrafficSalt(ctx)
	info := "mobilevc relay e2ee traffic v1|" + Suite + "|" + ctx.Direction
	key, err := hkdf.Key(sha256.New, sharedSecret, salt, info, KeyLength)
	if err != nil {
		return nil, fmt.Errorf("hkdf traffic key: %w", err)
	}
	return key, nil
}

func TrafficSalt(ctx FrameContext) []byte {
	sum := sha256.Sum256([]byte(ctx.SessionID + "|" + ctx.ClientID + "|" + ctx.HandshakeID))
	return sum[:]
}

func Nonce(ctx FrameContext) ([]byte, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	digest := sha256.New()
	digest.Write([]byte("mobilevc relay e2ee nonce v1"))
	digest.Write([]byte{0})
	digest.Write([]byte(ctx.HandshakeID))
	digest.Write([]byte{0})
	digest.Write([]byte(ctx.Direction))
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], ctx.StreamID)
	digest.Write(number[:])
	binary.BigEndian.PutUint64(number[:], ctx.Counter)
	digest.Write(number[:])
	return digest.Sum(nil)[:NonceLength], nil
}

func AAD(ctx FrameContext) ([]byte, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 128)
	out = append(out, 'M', 'V', 'C', 'E')
	out = appendUint16(out, Version)
	var err error
	for _, value := range []string{
		Suite,
		ctx.SessionID,
		ctx.ClientID,
		ctx.HandshakeID,
		ctx.Direction,
	} {
		out, err = appendString(out, value)
		if err != nil {
			return nil, err
		}
	}
	out = appendUint64(out, ctx.StreamID)
	out = appendUint64(out, ctx.Counter)
	return out, nil
}

func Encrypt(key, plaintext []byte, ctx FrameContext) ([]byte, error) {
	gcm, aad, nonce, err := cipherInputs(key, ctx)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nil
}

func Decrypt(key, sealed []byte, ctx FrameContext) ([]byte, error) {
	gcm, aad, nonce, err := cipherInputs(key, ctx)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, fmt.Errorf("aes-gcm decrypt: %w", err)
	}
	return plaintext, nil
}

func validateContext(ctx FrameContext) error {
	if ctx.Direction != DirectionClientToAgent && ctx.Direction != DirectionAgentToClient {
		return ErrInvalidDirection
	}
	return nil
}

func cipherInputs(key []byte, ctx FrameContext) (cipher.AEAD, []byte, []byte, error) {
	if len(key) != KeyLength {
		return nil, nil, nil, ErrInvalidLength
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aes-256 key: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aes-gcm: %w", err)
	}
	aad, err := AAD(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	nonce, err := Nonce(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return gcm, aad, nonce, nil
}

func appendString(out []byte, value string) ([]byte, error) {
	if len(value) > math.MaxUint16 {
		return nil, fmt.Errorf("%w: string exceeds uint16 length", ErrInvalidAADField)
	}
	out = appendUint16(out, uint16(len(value)))
	return append(out, value...), nil
}

func appendUint16(out []byte, value uint16) []byte {
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], value)
	return append(out, encoded[:]...)
}

func appendUint64(out []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(out, encoded[:]...)
}
