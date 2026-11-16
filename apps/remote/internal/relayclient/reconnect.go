package relayclient

import (
	"context"
	"errors"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/gateway"
)

type Handler interface {
	ServeClientConn(context.Context, gateway.ClientConn)
}

func serveWithReconnect(ctx context.Context, cfg Config, handler Handler, conn *websocket.Conn, sessionID string, pairingSecret string, reconnectSecret string) error {
	for {
		nodeIdentity, err := currentNodeIdentity(cfg)
		if err != nil {
			return err
		}
		e2eeHandler := newAgentE2EEHandshakeHandlerWithDeviceTrust(
			sessionID,
			pairingSecret,
			relayClientCapabilities(cfg),
			nodeIdentity,
			cfg.DeviceTrust,
		)
		gatewayConn := newGatewayConnWithPolicy(conn, sessionID, e2eeHandler, cfg.DownloadRoots, selectedRoutePolicy(cfg))
		if err := gatewayConn.waitReadyForHandler(); err != nil {
			if errors.Is(gatewayConn.closeReason(), errRelaySessionRotated) {
				return errRelaySessionRotated
			}
			_ = gatewayConn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			nextConn, err := reconnectUntilAccepted(ctx, cfg, sessionID, reconnectSecret)
			if err != nil {
				return err
			}
			conn = nextConn
			continue
		}
		handler.ServeClientConn(ctx, gatewayConn)
		if errors.Is(gatewayConn.closeReason(), errRelaySessionRotated) {
			return errRelaySessionRotated
		}
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		nextConn, err := reconnectUntilAccepted(ctx, cfg, sessionID, reconnectSecret)
		if err != nil {
			return err
		}
		conn = nextConn
	}
}

func reconnectUntilAccepted(ctx context.Context, cfg Config, sessionID string, reconnectSecret string) (*websocket.Conn, error) {
	backoff := normalizedBackoff(cfg.ReconnectBackoff)
	for {
		conn, err := dialAgent(ctx, cfg.RelayURL)
		if err == nil {
			req := agentReconnectRequest{SessionID: sessionID, ReconnectSecret: reconnectSecret}
			if err = reconnectAgent(conn, req); err == nil {
				return conn, nil
			}
			_ = conn.Close()
			if errors.Is(err, errAgentReconnectRejected) {
				return nil, err
			}
		}
		if err := sleepBackoff(ctx, backoff.Initial); err != nil {
			return nil, err
		}
		backoff.Initial = nextBackoff(backoff)
	}
}

func reconnectDeadline(grace time.Duration) time.Time {
	return time.Now().Add(grace)
}

func normalizedBackoff(backoff ReconnectBackoff) ReconnectBackoff {
	if backoff.Initial <= 0 {
		backoff.Initial = 500 * time.Millisecond
	}
	if backoff.Max <= 0 {
		backoff.Max = 5 * time.Second
	}
	return backoff
}

func sleepBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextBackoff(backoff ReconnectBackoff) time.Duration {
	next := backoff.Initial * 2
	if next > backoff.Max {
		return backoff.Max
	}
	return next
}
