package db

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

type RefreshToken struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	TokenHash string `json:"-"`
	ExpiresAt string `json:"expiresAt"`
	CreatedAt string `json:"createdAt"`
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (db *DB) CreateRefreshToken(userID string, ttlDays int) (tokenID, rawToken string, err error) {
	rawToken = uuid.New().String() + uuid.New().String()
	tokenHash := HashToken(rawToken)
	id := uuid.New().String()
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(ttlDays) * 24 * time.Hour).Format(time.RFC3339)
	createdAt := now.Format(time.RFC3339)

	_, err = db.conn.Exec(
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, userID, tokenHash, expiresAt, createdAt,
	)
	if err != nil {
		return "", "", err
	}
	return id, rawToken, nil
}

func (db *DB) ValidateAndConsumeRefreshToken(rawToken string) (userID string, err error) {
	tokenHash := HashToken(rawToken)

	var id, uid, expiresAt string
	err = db.conn.QueryRow(
		`SELECT id, user_id, expires_at FROM refresh_tokens WHERE token_hash=?`,
		tokenHash,
	).Scan(&id, &uid, &expiresAt)
	if err != nil {
		return "", err
	}

	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().UTC().After(expiry) {
		db.conn.Exec(`DELETE FROM refresh_tokens WHERE id=?`, id)
		return "", err
	}

	// Rotation: delete old token
	db.conn.Exec(`DELETE FROM refresh_tokens WHERE id=?`, id)

	return uid, nil
}

func (db *DB) RevokeUserTokens(userID string) error {
	_, err := db.conn.Exec(`DELETE FROM refresh_tokens WHERE user_id=?`, userID)
	return err
}
