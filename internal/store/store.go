package store

import (
	"context"

	"mobilevc/internal/session"
)

type Store interface {
	SaveSession(ctx context.Context, sess session.Session) error
	GetSession(ctx context.Context, sessionID string) (session.Session, error)
}
