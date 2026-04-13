package ws

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mobilevc/internal/protocol"
)

const temporaryPermissionGrantTTL = 60 * time.Second

type permissionGrant struct {
	sessionID           string
	targetPath          string
	permissionRequestID string
	createdAt           time.Time
	expiresAt           time.Time
	signature           string
}

type permissionGrantStore struct {
	mu     sync.Mutex
	items  map[string]permissionGrant
	secret []byte
	now    func() time.Time
}

func newPermissionGrantStore() *permissionGrantStore {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		secret = []byte(time.Now().UTC().Format(time.RFC3339Nano))
	}
	return &permissionGrantStore{
		items:  make(map[string]permissionGrant),
		secret: secret,
		now:    time.Now,
	}
}

func (s *permissionGrantStore) Issue(sessionID, targetPath, permissionRequestID string, ttl time.Duration) bool {
	if s == nil {
		return false
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	normalizedTargetPath := normalizePermissionGrantPath(targetPath)
	normalizedRequestID := strings.TrimSpace(permissionRequestID)
	if normalizedSessionID == "" || normalizedTargetPath == "" {
		return false
	}
	now := s.now()
	if ttl <= 0 {
		ttl = temporaryPermissionGrantTTL
	}
	grant := permissionGrant{
		sessionID:           normalizedSessionID,
		targetPath:          normalizedTargetPath,
		permissionRequestID: normalizedRequestID,
		createdAt:           now,
		expiresAt:           now.Add(ttl),
	}
	grant.signature = s.sign(grant)
	key := permissionGrantKey(normalizedSessionID, normalizedTargetPath)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
	s.items[key] = grant
	return true
}

func (s *permissionGrantStore) ConsumeIfValid(sessionID, targetPath, permissionRequestID string) bool {
	if s == nil {
		return false
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	normalizedTargetPath := normalizePermissionGrantPath(targetPath)
	normalizedRequestID := strings.TrimSpace(permissionRequestID)
	if normalizedSessionID == "" || normalizedTargetPath == "" {
		return false
	}
	key := permissionGrantKey(normalizedSessionID, normalizedTargetPath)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
	grant, ok := s.items[key]
	if !ok {
		return false
	}
	if grant.expiresAt.Before(now) || !s.isValidLocked(grant) {
		delete(s.items, key)
		return false
	}
	if normalizedRequestID != "" && grant.permissionRequestID != "" && grant.permissionRequestID != normalizedRequestID {
		return false
	}
	delete(s.items, key)
	return true
}

func (s *permissionGrantStore) Revoke(sessionID, targetPath string) {
	if s == nil {
		return
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	normalizedTargetPath := normalizePermissionGrantPath(targetPath)
	if normalizedSessionID == "" || normalizedTargetPath == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, permissionGrantKey(normalizedSessionID, normalizedTargetPath))
}

func (s *permissionGrantStore) sign(grant permissionGrant) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(grant.sessionID))
	mac.Write([]byte("\n"))
	mac.Write([]byte(grant.targetPath))
	mac.Write([]byte("\n"))
	mac.Write([]byte(grant.permissionRequestID))
	mac.Write([]byte("\n"))
	mac.Write([]byte(grant.expiresAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *permissionGrantStore) isValidLocked(grant permissionGrant) bool {
	expected := s.sign(permissionGrant{
		sessionID:           grant.sessionID,
		targetPath:          grant.targetPath,
		permissionRequestID: grant.permissionRequestID,
		createdAt:           grant.createdAt,
		expiresAt:           grant.expiresAt,
	})
	return hmac.Equal([]byte(expected), []byte(grant.signature))
}

func (s *permissionGrantStore) cleanupExpiredLocked(now time.Time) {
	for key, grant := range s.items {
		if grant.expiresAt.Before(now) || !s.isValidLocked(grant) {
			delete(s.items, key)
		}
	}
}

func permissionGrantKey(sessionID, targetPath string) string {
	return sessionID + "\n" + targetPath
}

func normalizePermissionGrantPath(targetPath string) string {
	trimmed := strings.TrimSpace(targetPath)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func permissionGrantTargetPathFromEvent(event any) string {
	switch e := event.(type) {
	case protocol.PromptRequestEvent:
		return normalizePermissionGrantPath(e.RuntimeMeta.TargetPath)
	case protocol.InteractionRequestEvent:
		return normalizePermissionGrantPath(firstNonEmptyString(e.TargetPath, e.RuntimeMeta.TargetPath))
	default:
		return ""
	}
}
