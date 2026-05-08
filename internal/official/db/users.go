package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	ProviderID string `json:"providerId"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatarUrl"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

func (db *DB) UpsertUser(provider, providerID, email, name, avatarURL string) (*User, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	existing, err := db.GetUserByProvider(provider, providerID)
	if err == nil && existing != nil {
		existing.Email = email
		existing.Name = name
		existing.AvatarURL = avatarURL
		existing.UpdatedAt = now
		_, err := db.conn.Exec(
			`UPDATE users SET email=?, name=?, avatar_url=?, updated_at=? WHERE id=?`,
			email, name, avatarURL, now, existing.ID,
		)
		return existing, err
	}

	id := uuid.New().String()
	_, err = db.conn.Exec(
		`INSERT INTO users (id, provider, provider_id, email, name, avatar_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, provider, providerID, email, name, avatarURL, now, now,
	)
	if err != nil {
		return nil, err
	}

	return &User{
		ID: id, Provider: provider, ProviderID: providerID,
		Email: email, Name: name, AvatarURL: avatarURL,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) GetUserByProvider(provider, providerID string) (*User, error) {
	var u User
	err := db.conn.QueryRow(
		`SELECT id, provider, provider_id, email, name, avatar_url, created_at, updated_at
		 FROM users WHERE provider=? AND provider_id=?`,
		provider, providerID,
	).Scan(&u.ID, &u.Provider, &u.ProviderID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *DB) GetUserByID(id string) (*User, error) {
	var u User
	err := db.conn.QueryRow(
		`SELECT id, provider, provider_id, email, name, avatar_url, created_at, updated_at
		 FROM users WHERE id=?`, id,
	).Scan(&u.ID, &u.Provider, &u.ProviderID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
