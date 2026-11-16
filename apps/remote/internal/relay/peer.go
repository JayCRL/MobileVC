package relay

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type peerRole string

const (
	roleAgent  peerRole = "agent"
	roleClient peerRole = "client"

	peerWriteTimeout = 10 * time.Second
)

type peerConn struct {
	conn        *websocket.Conn
	role        peerRole
	remote      string
	userAgent   string
	system      string
	deviceName  string
	connectedAt time.Time
	queue       chan any
	stop        chan struct{}
	done        chan struct{}
	stopOnce    sync.Once
	writeMu     sync.Mutex
}

func newPeerConn(conn *websocket.Conn, role peerRole, remote string, queueSize int, userAgent string) *peerConn {
	return &peerConn{
		conn:        conn,
		role:        role,
		remote:      remote,
		userAgent:   userAgent,
		system:      inferSystem(userAgent, ""),
		connectedAt: time.Now().UTC(),
		queue:       make(chan any, queueSize),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
}

func (p *peerConn) ReadJSON(v any) error {
	return p.conn.ReadJSON(v)
}

func (p *peerConn) ReadRawFrame(limit int64) ([]byte, error) {
	_, reader, err := p.conn.NextReader()
	if err != nil {
		return nil, err
	}
	return readLimitedFrame(reader, limit)
}

func (p *peerConn) Enqueue(v any) error {
	select {
	case p.queue <- v:
		return nil
	default:
		return errors.New("relay forward queue full")
	}
}

func (p *peerConn) WriteJSON(v any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if err := p.conn.SetWriteDeadline(time.Now().Add(peerWriteTimeout)); err != nil {
		return err
	}
	if err := p.conn.WriteJSON(v); err != nil {
		return err
	}
	return p.conn.SetWriteDeadline(time.Time{})
}

func (p *peerConn) Close() error {
	return p.conn.Close()
}

func (p *peerConn) StartWriter(interval time.Duration) {
	defer close(p.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case msg, ok := <-p.queue:
			if !ok {
				return
			}
			if err := p.WriteJSON(msg); err != nil {
				_ = p.Close()
				return
			}
		case <-ticker.C:
			deadline := time.Now().Add(peerWriteTimeout)
			if err := p.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				_ = p.Close()
				return
			}
		}
	}
}

func (p *peerConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.conn.WriteControl(messageType, data, deadline)
}

func (p *peerConn) Stop() {
	p.stopOnce.Do(func() {
		close(p.stop)
	})
	_ = p.conn.Close()
	<-p.done
}

func (p *peerConn) ConfigurePong(timeout time.Duration) {
	_ = p.conn.SetReadDeadline(time.Now().Add(timeout))
	p.conn.SetPongHandler(func(string) error {
		return p.conn.SetReadDeadline(time.Now().Add(timeout))
	})
}
