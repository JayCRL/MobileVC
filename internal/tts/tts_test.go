package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubProvider struct {
	synthesizeFn func(ctx context.Context, req ProviderRequest) (ProviderResult, error)
	healthFn     func(ctx context.Context) error
}

func (s stubProvider) Synthesize(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if s.synthesizeFn != nil {
		return s.synthesizeFn(ctx, req)
	}
	return ProviderResult{}, nil
}

func (s stubProvider) HealthCheck(ctx context.Context) error {
	if s.healthFn != nil {
		return s.healthFn(ctx)
	}
	return nil
}

func TestServiceSynthesize(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		service := NewService(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				if req.Text != "你好" {
					t.Fatalf("unexpected text: %q", req.Text)
				}
				if req.Format != "wav" {
					t.Fatalf("unexpected format: %q", req.Format)
				}
				return ProviderResult{ContentType: "audio/wav", Audio: []byte("wav-bytes")}, nil
			},
		}, 10, "wav")

		result, err := service.Synthesize(context.Background(), SynthesizeRequest{Text: "  你好  "})
		if err != nil {
			t.Fatalf("Synthesize returned error: %v", err)
		}
		if result.ContentType != "audio/wav" {
			t.Fatalf("unexpected content type: %q", result.ContentType)
		}
		if string(result.Audio) != "wav-bytes" {
			t.Fatalf("unexpected audio: %q", string(result.Audio))
		}
		if !strings.HasPrefix(result.Filename, "tts-") || !strings.HasSuffix(result.Filename, ".wav") {
			t.Fatalf("unexpected filename: %q", result.Filename)
		}
	})

	t.Run("empty text", func(t *testing.T) {
		service := NewService(stubProvider{}, 10, "wav")
		_, err := service.Synthesize(context.Background(), SynthesizeRequest{Text: "   "})
		if !errors.Is(err, ErrEmptyText) {
			t.Fatalf("expected ErrEmptyText, got %v", err)
		}
	})

	t.Run("text too long", func(t *testing.T) {
		service := NewService(stubProvider{}, 2, "wav")
		_, err := service.Synthesize(context.Background(), SynthesizeRequest{Text: "你好啊"})
		if !errors.Is(err, ErrTextTooLong) {
			t.Fatalf("expected ErrTextTooLong, got %v", err)
		}
	})

	t.Run("unsupported format", func(t *testing.T) {
		service := NewService(stubProvider{}, 10, "wav")
		_, err := service.Synthesize(context.Background(), SynthesizeRequest{Text: "你好", Format: "mp3"})
		if !errors.Is(err, ErrUnsupportedFormat) {
			t.Fatalf("expected ErrUnsupportedFormat, got %v", err)
		}
	})

	t.Run("provider timeout", func(t *testing.T) {
		service := NewService(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				return ProviderResult{}, ErrProviderTimeout
			},
		}, 10, "wav")
		_, err := service.Synthesize(context.Background(), SynthesizeRequest{Text: "你好"})
		if !errors.Is(err, ErrProviderTimeout) {
			t.Fatalf("expected ErrProviderTimeout, got %v", err)
		}
	})

	t.Run("provider unavailable", func(t *testing.T) {
		service := NewService(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				return ProviderResult{}, errors.New("dial failed")
			},
		}, 10, "wav")
		_, err := service.Synthesize(context.Background(), SynthesizeRequest{Text: "你好"})
		if !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("expected ErrProviderUnavailable, got %v", err)
		}
	})

	t.Run("empty audio", func(t *testing.T) {
		service := NewService(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				return ProviderResult{ContentType: "audio/wav", Audio: nil}, nil
			},
		}, 10, "wav")
		_, err := service.Synthesize(context.Background(), SynthesizeRequest{Text: "你好"})
		if !errors.Is(err, ErrEmptyAudioResponse) {
			t.Fatalf("expected ErrEmptyAudioResponse, got %v", err)
		}
	})
}

func TestChatTTSHTTPProvider(t *testing.T) {
	t.Run("synthesize success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/synthesize" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if got := r.Header.Get("Accept"); got != "audio/wav" {
				t.Fatalf("unexpected accept header: %q", got)
			}
			var req ProviderRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Text != "你好" || req.Format != "wav" {
				t.Fatalf("unexpected request: %#v", req)
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write([]byte("wav-data"))
		}))
		defer server.Close()

		provider := NewChatTTSHTTPProvider(server.URL, time.Second)
		result, err := provider.Synthesize(context.Background(), ProviderRequest{Text: "你好", Format: "wav"})
		if err != nil {
			t.Fatalf("Synthesize returned error: %v", err)
		}
		if result.ContentType != "audio/wav" {
			t.Fatalf("unexpected content type: %q", result.ContentType)
		}
		if string(result.Audio) != "wav-data" {
			t.Fatalf("unexpected audio: %q", string(result.Audio))
		}
	})

	t.Run("synthesize non 2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusBadGateway)
		}))
		defer server.Close()

		provider := NewChatTTSHTTPProvider(server.URL, time.Second)
		_, err := provider.Synthesize(context.Background(), ProviderRequest{Text: "你好", Format: "wav"})
		if err == nil || !strings.Contains(err.Error(), "python service returned 502") {
			t.Fatalf("expected 502 error, got %v", err)
		}
	})

	t.Run("synthesize unsupported content type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()

		provider := NewChatTTSHTTPProvider(server.URL, time.Second)
		_, err := provider.Synthesize(context.Background(), ProviderRequest{Text: "你好", Format: "wav"})
		if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
			t.Fatalf("expected content type error, got %v", err)
		}
	})

	t.Run("synthesize empty body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/wav")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		provider := NewChatTTSHTTPProvider(server.URL, time.Second)
		_, err := provider.Synthesize(context.Background(), ProviderRequest{Text: "你好", Format: "wav"})
		if !errors.Is(err, ErrEmptyAudioResponse) {
			t.Fatalf("expected ErrEmptyAudioResponse, got %v", err)
		}
	})

	t.Run("health check success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		provider := NewChatTTSHTTPProvider(server.URL, time.Second)
		if err := provider.HealthCheck(context.Background()); err != nil {
			t.Fatalf("HealthCheck returned error: %v", err)
		}
	})
}

func TestHTTPHandlerHandleSynthesize(t *testing.T) {
	newHandler := func(provider stubProvider) *HTTPHandler {
		service := NewService(provider, 10, "wav")
		return NewHTTPHandler("test-token", true, "chattts-http", service)
	}

	t.Run("success with query token", func(t *testing.T) {
		handler := newHandler(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				return ProviderResult{ContentType: "audio/wav", Audio: []byte("wav-data")}, nil
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/synthesize?token=test-token", bytes.NewBufferString(`{"text":"你好"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}
		if got := resp.Header().Get("Content-Type"); got != "audio/wav" {
			t.Fatalf("unexpected content type: %q", got)
		}
		if !strings.Contains(resp.Header().Get("Content-Disposition"), "inline;") {
			t.Fatalf("unexpected content disposition: %q", resp.Header().Get("Content-Disposition"))
		}
		if body := resp.Body.String(); body != "wav-data" {
			t.Fatalf("unexpected body: %q", body)
		}
	})

	t.Run("success with bearer token", func(t *testing.T) {
		handler := newHandler(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				return ProviderResult{ContentType: "audio/wav", Audio: []byte("wav-data")}, nil
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/synthesize", bytes.NewBufferString(`{"text":"你好"}`))
		req.Header.Set("Authorization", "Bearer test-token")
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		handler := newHandler(stubProvider{})
		req := httptest.NewRequest(http.MethodGet, "/api/tts/synthesize?token=test-token", nil)
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", resp.Code)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		handler := newHandler(stubProvider{})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/synthesize", bytes.NewBufferString(`{"text":"你好"}`))
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.Code)
		}
	})

	t.Run("invalid json body", func(t *testing.T) {
		handler := newHandler(stubProvider{})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/synthesize?token=test-token", bytes.NewBufferString(`{"text":`))
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.Code)
		}
	})

	t.Run("bad request", func(t *testing.T) {
		handler := newHandler(stubProvider{})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/synthesize?token=test-token", bytes.NewBufferString(`{"text":""}`))
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.Code)
		}
	})

	t.Run("provider unavailable", func(t *testing.T) {
		handler := newHandler(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				return ProviderResult{}, errors.New("connection refused")
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/synthesize?token=test-token", bytes.NewBufferString(`{"text":"你好"}`))
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", resp.Code)
		}
	})

	t.Run("provider timeout", func(t *testing.T) {
		handler := newHandler(stubProvider{
			synthesizeFn: func(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
				return ProviderResult{}, ErrProviderTimeout
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/synthesize?token=test-token", bytes.NewBufferString(`{"text":"你好"}`))
		resp := httptest.NewRecorder()

		handler.HandleSynthesize(resp, req)

		if resp.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected 504, got %d", resp.Code)
		}
	})
}

func TestHTTPHandlerHandleHealthz(t *testing.T) {
	newHandler := func(provider stubProvider) *HTTPHandler {
		service := NewService(provider, 10, "wav")
		return NewHTTPHandler("test-token", true, "chattts-http", service)
	}

	t.Run("method not allowed", func(t *testing.T) {
		handler := newHandler(stubProvider{})
		req := httptest.NewRequest(http.MethodPost, "/api/tts/healthz?token=test-token", nil)
		resp := httptest.NewRecorder()

		handler.HandleHealthz(resp, req)

		if resp.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", resp.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		handler := newHandler(stubProvider{
			healthFn: func(ctx context.Context) error { return nil },
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tts/healthz?token=test-token", nil)
		resp := httptest.NewRecorder()

		handler.HandleHealthz(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}
		if got := resp.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Fatalf("unexpected content type: %q", got)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"status":"ok"`) {
			t.Fatalf("unexpected body: %s", string(body))
		}
	})

	t.Run("timeout", func(t *testing.T) {
		handler := newHandler(stubProvider{
			healthFn: func(ctx context.Context) error { return ErrProviderTimeout },
		})
		req := httptest.NewRequest(http.MethodGet, "/api/tts/healthz?token=test-token", nil)
		resp := httptest.NewRecorder()

		handler.HandleHealthz(resp, req)

		if resp.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected 504, got %d", resp.Code)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		handler := newHandler(stubProvider{})
		req := httptest.NewRequest(http.MethodGet, "/api/tts/healthz", nil)
		resp := httptest.NewRecorder()

		handler.HandleHealthz(resp, req)

		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.Code)
		}
	})
}
