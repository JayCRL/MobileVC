package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatTTSHTTPProvider struct {
	baseURL string
	client  *http.Client
}

func NewChatTTSHTTPProvider(baseURL string, timeout time.Duration) *ChatTTSHTTPProvider {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &ChatTTSHTTPProvider{
		baseURL: trimmedBaseURL,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (p *ChatTTSHTTPProvider) Synthesize(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return ProviderResult{}, fmt.Errorf("marshal synthesize request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/synthesize", bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{}, fmt.Errorf("build synthesize request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "audio/wav")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if isTimeoutError(err) {
			return ProviderResult{}, ErrProviderTimeout
		}
		return ProviderResult{}, fmt.Errorf("call python service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return ProviderResult{}, fmt.Errorf("python service returned %d: %s", resp.StatusCode, message)
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "audio/wav"
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "audio/") {
		return ProviderResult{}, fmt.Errorf("python service returned unsupported content type: %s", contentType)
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		if isTimeoutError(err) {
			return ProviderResult{}, ErrProviderTimeout
		}
		return ProviderResult{}, fmt.Errorf("read synthesize response: %w", err)
	}
	if len(audio) == 0 {
		return ProviderResult{}, ErrEmptyAudioResponse
	}

	return ProviderResult{ContentType: contentType, Audio: audio}, nil
}

func (p *ChatTTSHTTPProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("build health check request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if isTimeoutError(err) {
			return ErrProviderTimeout
		}
		return fmt.Errorf("call python healthz: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("python healthz returned %d: %s", resp.StatusCode, message)
	}
	return nil
}

func isTimeoutError(err error) bool {
	type timeout interface {
		Timeout() bool
	}
	if te, ok := err.(timeout); ok && te.Timeout() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded")
}
