package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type TrafficStat struct {
	ID            string `json:"id"`
	NodeID        string `json:"nodeId"`
	PeerID        string `json:"peerId"`
	BytesSent     int64  `json:"bytesSent"`
	BytesReceived int64  `json:"bytesReceived"`
	StartedAt     string `json:"startedAt"`
	UpdatedAt     string `json:"updatedAt"`
}

func (db *DB) UpsertTraffic(nodeID, peerID string, bytesSent, bytesReceived int64) error {
	// UPDATE: increment existing counters so periodic reports accumulate
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.conn.Exec(
		`UPDATE traffic_stats SET bytes_sent = bytes_sent + ?, bytes_received = bytes_received + ?, updated_at = ?
		 WHERE node_id = ? AND peer_id = ?`,
		bytesSent, bytesReceived, now, nodeID, peerID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}

	// INSERT: first report for this peer
	id := uuid.New().String()
	_, err = db.conn.Exec(
		`INSERT INTO traffic_stats (id, node_id, peer_id, bytes_sent, bytes_received, started_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, nodeID, peerID, bytesSent, bytesReceived, now, now,
	)
	return err
}

func (db *DB) TotalTraffic() (sent int64, received int64, err error) {
	err = db.conn.QueryRow(`SELECT COALESCE(SUM(bytes_sent), 0), COALESCE(SUM(bytes_received), 0) FROM traffic_stats`).Scan(&sent, &received)
	return
}

// NodeTraffic returns aggregated traffic for a specific node.
func (db *DB) NodeTraffic(nodeID string) (sent int64, received int64, err error) {
	err = db.conn.QueryRow(
		`SELECT COALESCE(SUM(bytes_sent), 0), COALESCE(SUM(bytes_received), 0) FROM traffic_stats WHERE node_id = ?`,
		nodeID,
	).Scan(&sent, &received)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return
}

// AllNodeTraffic returns traffic summary keyed by node_id.
func (db *DB) AllNodeTraffic() (map[string][2]int64, error) {
	rows, err := db.conn.Query(
		`SELECT node_id, COALESCE(SUM(bytes_sent), 0), COALESCE(SUM(bytes_received), 0)
		 FROM traffic_stats GROUP BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string][2]int64)
	for rows.Next() {
		var nodeID string
		var sent, recv int64
		if err := rows.Scan(&nodeID, &sent, &recv); err != nil {
			return nil, err
		}
		m[nodeID] = [2]int64{sent, recv}
	}
	return m, rows.Err()
}
