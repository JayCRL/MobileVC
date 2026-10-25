package tts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrEmptyText           = errors.New("text is required")
	ErrTextTooLong         = errors.New("text exceeds maximum length")
	ErrUnsupportedFormat   = errors.New("format is not supported")
	ErrProviderUnavailable = errors.New("tts provider unavailable")
	ErrProviderTimeout     = errors.New("tts provider timeout")
	ErrEmptyAudioResponse  = errors.New("tts provider returned empty audio")
)

type Service struct {
	provider      Provider
	maxTextLength int
	defaultFormat string
}

func NewService(provider Provider, maxTextLength int, defaultFormat string) *Service {
	if maxTextLength <= 0 {
		maxTextLength = 200
	}
	defaultFormat = normalizeFormat(defaultFormat)
	if defaultFormat == "" {
		defaultFormat = "wav"
	}
	return &Service{
		provider:      provider,
		maxTextLength: maxTextLength,
		defaultFormat: defaultFormat,
	}
}

func (s *Service) Synthesize(ctx context.Context, req SynthesizeRequest) (SynthesizeResult, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return SynthesizeResult{}, ErrEmptyText
	}
	if len([]rune(text)) > s.maxTextLength {
		return SynthesizeResult{}, fmt.Errorf("%w: max=%d", ErrTextTooLong, s.maxTextLength)
	}

	format := normalizeFormat(req.Format)
	if format == "" {
		format = s.defaultFormat
	}
	if format != "wav" {
		return SynthesizeResult{}, ErrUnsupportedFormat
	}

	if s.provider == nil {
		return SynthesizeResult{}, ErrProviderUnavailable
	}

	providerResult, err := s.provider.Synthesize(ctx, ProviderRequest{Text: text, Format: format})
	if err != nil {
		if errors.Is(err, ErrProviderTimeout) {
			return SynthesizeResult{}, err
		}
		return SynthesizeResult{}, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	if len(providerResult.Audio) == 0 {
		return SynthesizeResult{}, ErrEmptyAudioResponse
	}

	contentType := strings.TrimSpace(providerResult.ContentType)
	if contentType == "" {
		contentType = "audio/wav"
	}

	return SynthesizeResult{
		ContentType: contentType,
		Filename:    fmt.Sprintf("tts-%d.%s", time.Now().UnixMilli(), format),
		Audio:       providerResult.Audio,
	}, nil
}

func (s *Service) HealthCheck(ctx context.Context) error {
	if s.provider == nil {
		return ErrProviderUnavailable
	}
	return s.provider.HealthCheck(ctx)
}

func normalizeFormat(format string) string {
	return strings.ToLower(strings.TrimSpace(format))
}
