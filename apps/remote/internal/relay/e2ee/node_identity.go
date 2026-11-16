package e2ee

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	NodeIdentityFileName = "node_identity.json"
	nodeIdentityVersion  = 1
	nodeIdentityCurve    = "P-256"
)

type NodeIdentity struct {
	PrivateKey  *ecdsa.PrivateKey
	PublicKey   []byte
	Fingerprint []byte
}

type NodeIdentityStore struct {
	path     string
	mu       sync.Mutex
	identity *NodeIdentity
}

type nodeIdentityFile struct {
	Version          int    `json:"version"`
	Curve            string `json:"curve"`
	PrivateKeyBase64 string `json:"privateKeyBase64"`
	PublicKeyBase64  string `json:"publicKeyBase64"`
	FingerprintHex   string `json:"fingerprintHex"`
}

func DefaultNodeIdentityPath(homeDir string) string {
	return filepath.Join(homeDir, ".mobilevc", "relay", NodeIdentityFileName)
}

func LoadOrCreateNodeIdentityStore(path string) (*NodeIdentityStore, error) {
	identity, err := LoadOrCreateNodeIdentity(path)
	if err != nil {
		return nil, err
	}
	return &NodeIdentityStore{
		path:     strings.TrimSpace(path),
		identity: identity,
	}, nil
}

func (s *NodeIdentityStore) Current() (*NodeIdentity, error) {
	if s == nil {
		return nil, errors.New("node identity store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.identity == nil {
		identity, err := LoadOrCreateNodeIdentity(s.path)
		if err != nil {
			return nil, err
		}
		s.identity = identity
	}
	return s.identity, nil
}

func (s *NodeIdentityStore) Rotate() (*NodeIdentity, error) {
	if s == nil {
		return nil, errors.New("node identity store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := GenerateNodeIdentity()
	if err != nil {
		return nil, err
	}
	if err := SaveNodeIdentity(s.path, identity); err != nil {
		return nil, err
	}
	s.identity = identity
	return identity, nil
}

func LoadOrCreateNodeIdentity(path string) (*NodeIdentity, error) {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return nil, errors.New("node identity path is required")
	}
	identity, err := LoadNodeIdentity(normalized)
	if err == nil {
		return identity, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	identity, err = GenerateNodeIdentity()
	if err != nil {
		return nil, err
	}
	if err := SaveNodeIdentity(normalized, identity); err != nil {
		return nil, err
	}
	return identity, nil
}

func GenerateNodeIdentity() (*NodeIdentity, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate node identity: %w", err)
	}
	return newNodeIdentity(privateKey)
}

func LoadNodeIdentity(path string) (*NodeIdentity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read node identity: %w", err)
	}
	var encoded nodeIdentityFile
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("parse node identity: %w", err)
	}
	if encoded.Version != nodeIdentityVersion || encoded.Curve != nodeIdentityCurve {
		return nil, fmt.Errorf("unsupported node identity format")
	}
	privateBytes, err := base64.RawStdEncoding.DecodeString(encoded.PrivateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode node private key: %w", err)
	}
	privateKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), privateBytes)
	if err != nil {
		return nil, fmt.Errorf("parse node private key: %w", err)
	}
	identity, err := newNodeIdentity(privateKey)
	if err != nil {
		return nil, err
	}
	if encoded.PublicKeyBase64 != base64.RawStdEncoding.EncodeToString(identity.PublicKey) ||
		encoded.FingerprintHex != fmt.Sprintf("%x", identity.Fingerprint) {
		return nil, fmt.Errorf("node identity public metadata mismatch")
	}
	return identity, nil
}

func SaveNodeIdentity(path string, identity *NodeIdentity) error {
	if identity == nil || identity.PrivateKey == nil {
		return errors.New("node identity private key is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create node identity directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("secure node identity directory: %w", err)
	}
	privateBytes, err := identity.PrivateKey.Bytes()
	if err != nil {
		return fmt.Errorf("encode node private key: %w", err)
	}
	encoded := nodeIdentityFile{
		Version:          nodeIdentityVersion,
		Curve:            nodeIdentityCurve,
		PrivateKeyBase64: base64.RawStdEncoding.EncodeToString(privateBytes),
		PublicKeyBase64:  base64.RawStdEncoding.EncodeToString(identity.PublicKey),
		FingerprintHex:   fmt.Sprintf("%x", identity.Fingerprint),
	}
	raw, err := json.MarshalIndent(encoded, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal node identity: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write node identity: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure node identity file: %w", err)
	}
	return nil
}

func (n *NodeIdentity) SignTranscript(transcript []byte) ([]byte, error) {
	if n == nil || n.PrivateKey == nil {
		return nil, errors.New("node identity private key is required")
	}
	digest := sha256.Sum256(transcript)
	r, s, err := ecdsa.Sign(rand.Reader, n.PrivateKey, digest[:])
	if err != nil {
		return nil, fmt.Errorf("sign node identity transcript: %w", err)
	}
	return encodeP256Signature(r, s), nil
}

func VerifyNodeSignature(publicKey []byte, transcript []byte, signature []byte) (bool, error) {
	parsed, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), publicKey)
	if err != nil {
		return false, fmt.Errorf("parse node public key: %w", err)
	}
	r, s, err := decodeP256Signature(signature)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(transcript)
	return ecdsa.Verify(parsed, digest[:], r, s), nil
}

func newNodeIdentity(privateKey *ecdsa.PrivateKey) (*NodeIdentity, error) {
	publicKey, err := privateKey.PublicKey.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode node public key: %w", err)
	}
	return &NodeIdentity{
		PrivateKey:  privateKey,
		PublicKey:   publicKey,
		Fingerprint: Fingerprint(publicKey),
	}, nil
}

func encodeP256Signature(r *big.Int, s *big.Int) []byte {
	out := make([]byte, KeyLength*2)
	r.FillBytes(out[:KeyLength])
	s.FillBytes(out[KeyLength:])
	return out
}

func decodeP256Signature(signature []byte) (*big.Int, *big.Int, error) {
	if len(signature) != KeyLength*2 {
		return nil, nil, fmt.Errorf("invalid p256 signature length")
	}
	r := new(big.Int).SetBytes(signature[:KeyLength])
	s := new(big.Int).SetBytes(signature[KeyLength:])
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return nil, nil, fmt.Errorf("invalid p256 signature scalar")
	}
	return r, s, nil
}
