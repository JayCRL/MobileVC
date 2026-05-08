package signaling

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"mobilevc/internal/official/auth"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 30 * time.Second
	pingPeriod     = 15 * time.Second
	maxMessageSize = 65536
)

type Handler struct {
	Hub     *Hub
	JWT     *auth.JWTService
	Upgrader websocket.Upgrader
}

func NewHandler(hub *Hub, jwtSvc *auth.JWTService) *Handler {
	return &Handler{
		Hub: hub,
		JWT: jwtSvc,
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authenticate via JWT in query parameter
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	claims, err := h.JWT.ParseAccessToken(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := h.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[signaling] upgrade error: %v", err)
		return
	}

	client := &Client{
		UserID:   claims.UserID,
		UserName: claims.Name,
		SendCh:   make(chan []byte, 64),
		hub:      h.Hub,
	}

	h.Hub.Register(client)
	defer h.Hub.Unregister(client)

	// Writer goroutine
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		defer conn.Close()

		for {
			select {
			case msg, ok := <-client.SendCh:
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				if !ok {
					conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// Reader loop
	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[signaling] read error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[signaling] invalid message: %v", err)
			continue
		}

		h.Hub.Route(client, msg)
	}
}
