package signaling

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// Message represents a signaling message between peers.
type Message struct {
	Type   string          `json:"type"`
	NodeID string          `json:"nodeId,omitempty"`
	PeerID string          `json:"peerId,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Accept *bool           `json:"accept,omitempty"`
	Reason string          `json:"reason,omitempty"`
}

type Client struct {
	UserID      string
	NodeID      string // non-empty for desktop nodes
	PeerID      string // mobile client's peer identifier for routing
	UserName    string // from JWT claims
	IsNode      bool   // true = desktop node, false = mobile client
	ConnectedAt time.Time
	SendCh      chan []byte
	hub         *Hub
	mu          sync.Mutex
	closed      bool
}

// ClientInfo is a read-only snapshot for the admin dashboard.
type ClientInfo struct {
	UserID      string `json:"userId"`
	UserName    string `json:"userName"`
	NodeID      string `json:"nodeId,omitempty"`
	IsNode      bool   `json:"isNode"`
	ConnectedAt string `json:"connectedAt"`
}

// HubStats is returned to the admin dashboard.
type HubStats struct {
	TotalUsers     int          `json:"totalUsers"`
	TotalClients   int          `json:"totalClients"`
	NodeCount      int          `json:"nodeCount"`
	MobileCount    int          `json:"mobileCount"`
	Clients        []ClientInfo `json:"clients"`
}

func (c *Client) Send(msg Message) {
	if c.closed {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case c.SendCh <- data:
	default:
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.SendCh)
}

// Hub manages signaling connections grouped by user.
type Hub struct {
	mu    sync.RWMutex
	users map[string]map[*Client]bool // userID -> set of clients
}

func NewHub() *Hub {
	return &Hub{
		users: make(map[string]map[*Client]bool),
	}
}

func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	client.ConnectedAt = time.Now()
	if h.users[client.UserID] == nil {
		h.users[client.UserID] = make(map[*Client]bool)
	}
	h.users[client.UserID][client] = true
	log.Printf("[signaling] client registered: user=%s node=%s isNode=%v",
		client.UserID, client.NodeID, client.IsNode)
}

func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	clients, ok := h.users[client.UserID]
	if !ok {
		return
	}
	delete(clients, client)
	if len(clients) == 0 {
		delete(h.users, client.UserID)
	}

	// Notify peers about disconnection
	if client.IsNode {
		// Desktop node went offline — notify all mobile clients
		for c := range clients {
			c.Send(Message{
				Type:   "node_offline",
				NodeID: client.NodeID,
			})
		}
	} else {
		// Mobile client disconnected — notify its connected node
		if client.NodeID != "" {
			for c := range clients {
				if c.IsNode && c.NodeID == client.NodeID {
					c.Send(Message{
						Type:   "peer_disconnected",
						PeerID: client.PeerID,
					})
				}
			}
		}
	}

	log.Printf("[signaling] client unregistered: user=%s node=%s", client.UserID, client.NodeID)
}

func (h *Hub) Route(client *Client, msg Message) {
	log.Printf("[signaling] route: from user=%s node=%s type=%s target=%s",
		client.UserID, client.NodeID, msg.Type, msg.NodeID)

	switch msg.Type {
	case "node_online":
		client.IsNode = true
		client.NodeID = msg.NodeID
		log.Printf("[signaling] node online: user=%s node=%s", client.UserID, msg.NodeID)

	case "connect_request":
		// Mobile wants to connect to a specific desktop node
		client.PeerID = msg.PeerID
		h.forwardToNode(client.UserID, msg.NodeID, msg)

	case "connect_response":
		// Desktop node responds to a connect request
		h.forwardToPeer(client.UserID, msg.PeerID, msg)

	case "webrtc":
		// Forward WebRTC signaling (offer/answer/ICE candidates)
		if msg.NodeID != "" {
			h.forwardToNode(client.UserID, msg.NodeID, msg)
		} else if msg.PeerID != "" {
			h.forwardToPeer(client.UserID, msg.PeerID, msg)
		}

	default:
		log.Printf("[signaling] unknown message type: %s", msg.Type)
	}
}

func (h *Hub) forwardToNode(userID, nodeID string, msg Message) {
	if nodeID == "" {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.users[userID]
	if !ok {
		return
	}
	for c := range clients {
		if c.IsNode && c.NodeID == nodeID {
			c.Send(msg)
			return
		}
	}
}

func (h *Hub) forwardToPeer(userID, peerID string, msg Message) {
	if peerID == "" {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	// Try sender's userID first
	if clients, ok := h.users[userID]; ok {
		for c := range clients {
			if c.PeerID == peerID {
				c.Send(msg)
				return
			}
		}
	}
	// Fallback: search all users for matching peerID
	for _, clients := range h.users {
		for c := range clients {
			if c.PeerID == peerID {
				c.Send(msg)
				return
			}
		}
	}
}

// FindNode checks if a specific node is online for a user.
func (h *Hub) FindNode(userID, nodeID string) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.users[userID]
	if !ok {
		return nil
	}
	for c := range clients {
		if c.IsNode && c.NodeID == nodeID {
			return c
		}
	}
	return nil
}

// Stats returns current connection statistics for the admin dashboard.
func (h *Hub) Stats() HubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := HubStats{}
	nodes := 0
	mobiles := 0

	for _, clients := range h.users {
		stats.TotalUsers++
		for c := range clients {
			stats.TotalClients++
			info := ClientInfo{
				UserID:      c.UserID,
				UserName:    c.UserName,
				NodeID:      c.NodeID,
				IsNode:      c.IsNode,
				ConnectedAt: c.ConnectedAt.UTC().Format(time.RFC3339),
			}
			if c.IsNode {
				nodes++
			} else {
				mobiles++
			}
			stats.Clients = append(stats.Clients, info)
		}
	}
	stats.NodeCount = nodes
	stats.MobileCount = mobiles
	return stats
}
