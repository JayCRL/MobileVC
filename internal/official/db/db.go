package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

type DBStats struct {
	TotalUsers    int `json:"totalUsers"`
	OnlineNodes   int `json:"onlineNodes"`
	OfflineNodes  int `json:"offlineNodes"`
	TotalSessions int `json:"totalSessions"`
}

func (db *DB) Stats() (DBStats, error) {
	var s DBStats
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&s.TotalUsers); err != nil {
		return s, err
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM nodes WHERE status='online'`).Scan(&s.OnlineNodes); err != nil {
		return s, err
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM nodes WHERE status='offline'`).Scan(&s.OfflineNodes); err != nil {
		return s, err
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM refresh_tokens`).Scan(&s.TotalSessions); err != nil {
		return s, err
	}
	return s, nil
}

func (db *DB) ListAllUsers() ([]User, error) {
	rows, err := db.conn.Query(
		`SELECT id, provider, provider_id, email, name, avatar_url, created_at, updated_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Provider, &u.ProviderID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (db *DB) ListAllNodes() ([]Node, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, name, version, status, stun_host, turn_port, turn_user, turn_pass, last_seen, created_at, updated_at
		 FROM nodes ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.UserID, &n.Name, &n.Version, &n.Status,
			&n.StunHost, &n.TurnPort, &n.TurnUser, &n.TurnPass,
			&n.LastSeen, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func runMigrations(conn *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(provider, provider_id)
		)`,
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'online',
			stun_host TEXT NOT NULL DEFAULT '',
			turn_port TEXT NOT NULL DEFAULT '3478',
			turn_user TEXT NOT NULL DEFAULT '',
			turn_pass TEXT NOT NULL DEFAULT '',
			last_seen TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_user_id ON nodes(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens(token_hash)`,
	}

	for _, m := range migrations {
		if _, err := conn.Exec(m); err != nil {
			return fmt.Errorf("exec %q: %w", m[:40], err)
		}
	}
	return nil
}
