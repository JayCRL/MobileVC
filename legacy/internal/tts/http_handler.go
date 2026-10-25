package tts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mobilevc/internal/logx"
)

type HTTPHandler struct {
	authToken string
	enabled   bool
	provider  string
	service   *Service
}

func NewHTTPHandler(authToken string, enabled bool, provider string, service *Service) *HTTPHandler {
	return &HTTPHandler{
		authToken: strings.TrimSpace(authToken),
		enabled:   enabled,
		provider:  strings.TrimSpace(provider),
		service:   service,
	}
}

func (h *HTTPHandler) HandleSynthesize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.enabled || h.service == nil {
		http.Error(w, "tts is disabled", http.StatusNotFound)
		return
	}

	defer r.Body.Close()
	var req SynthesizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	result, err := h.service.Synthesize(r.Context(), req)
	if err != nil {
		h.writeSynthesizeError(w, err)
		return
	}

	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", result.Filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result.Audio)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Audio)
}

func (h *HTTPHandler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.enabled || h.service == nil {
		http.Error(w, "tts is disabled", http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.service.HealthCheck(ctx); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ErrProviderTimeout) {
			status = http.StatusGatewayTimeout
		}
		logx.Warn("tts", "health check failed: provider=%s err=%v", h.provider, err)
		writeJSON(w, status, map[string]any{
			"provider": h.provider,
			"status":   "down",
			"error":    err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider": h.provider,
		"status":   "ok",
	})
}

func (h *HTTPHandler) authorized(r *http.Request) bool {
	queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if queryToken != "" && queryToken == h.authToken {
		return true
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		bearer := strings.TrimSpace(authHeader[7:])
		if bearer != "" && bearer == h.authToken {
			return true
		}
	}
	return false
}

func (h *HTTPHandler) writeSynthesizeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrEmptyText), errors.Is(err, ErrTextTooLong), errors.Is(err, ErrUnsupportedFormat):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrProviderTimeout):
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
	case errors.Is(err, ErrProviderUnavailable), errors.Is(err, ErrEmptyAudioResponse):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
