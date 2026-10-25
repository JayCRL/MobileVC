package tts

import "context"

type Provider interface {
	Synthesize(ctx context.Context, req ProviderRequest) (ProviderResult, error)
	HealthCheck(ctx context.Context) error
}
