package db

import (
	"time"

	"github.com/google/uuid"
)

type Node struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	StunHost  string `json:"stunHost"`
	TurnPort  string `json:"turnPort"`
	TurnUser  string `json:"turnUser"`
	TurnPass  string `json:"turnPass"`
	LastSeen  string `json:"lastSeen"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (db *DB) UpsertNode(nodeID, userID, name, version, stunHost, turnPort, turnUser, turnPass string) (*Node, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		`INSERT INTO nodes (id, user_id, name, version, status, stun_host, turn_port, turn_user, turn_pass, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'online', ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name, version=excluded.version, status='online',
		   stun_host=excluded.stun_host, turn_port=excluded.turn_port,
		   turn_user=excluded.turn_user, turn_pass=excluded.turn_pass,
		   last_seen=excluded.last_seen, updated_at=excluded.updated_at`,
		nodeID, userID, name, version, stunHost, turnPort, turnUser, turnPass, now, now, now,
	)
	if err != nil {
		return nil, err
	}

	return &Node{
		ID: nodeID, UserID: userID, Name: name, Version: version,
		Status: "online", StunHost: stunHost, TurnPort: turnPort,
		TurnUser: turnUser, TurnPass: turnPass,
		LastSeen: now, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) UpdateNodeStatus(nodeID, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`UPDATE nodes SET status=?, last_seen=?, updated_at=? WHERE id=?`,
		status, now, now, nodeID,
	)
	return err
}

func (db *DB) UpdateNodeHeartbeat(nodeID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`UPDATE nodes SET status='online', last_seen=?, updated_at=? WHERE id=?`,
		now, now, nodeID,
	)
	return err
}

func (db *DB) ListNodesByUser(userID string) ([]Node, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, name, version, status, stun_host, turn_port, turn_user, turn_pass, last_seen, created_at, updated_at
		 FROM nodes WHERE user_id=? ORDER BY last_seen DESC`, userID,
	)
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

func (db *DB) GetNode(nodeID string) (*Node, error) {
	var n Node
	err := db.conn.QueryRow(
		`SELECT id, user_id, name, version, status, stun_host, turn_port, turn_user, turn_pass, last_seen, created_at, updated_at
		 FROM nodes WHERE id=?`, nodeID,
	).Scan(&n.ID, &n.UserID, &n.Name, &n.Version, &n.Status,
		&n.StunHost, &n.TurnPort, &n.TurnUser, &n.TurnPass,
		&n.LastSeen, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (db *DB) DeleteNode(nodeID string) error {
	_, err := db.conn.Exec(`DELETE FROM nodes WHERE id=?`, nodeID)
	return err
}

func (db *DB) GenerateNodeID() string {
	return uuid.New().String()
}

// MarkStaleNodesOffline marks nodes offline that haven't sent a heartbeat within the given duration.
func (db *DB) MarkStaleNodesOffline(staleAfterSeconds int) (int, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(staleAfterSeconds) * time.Second).Format(time.RFC3339)
	result, err := db.conn.Exec(
		`UPDATE nodes SET status='offline', updated_at=? WHERE status='online' AND last_seen < ?`,
		time.Now().UTC().Format(time.RFC3339), cutoff,
	)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
