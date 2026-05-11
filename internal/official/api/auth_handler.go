package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"mobilevc/internal/official/auth"
	"mobilevc/internal/official/db"
)

type AuthHandler struct {
	DB             *db.DB
	JWT            *auth.JWTService
	BaseURL        string
	GitHub         *auth.GitHubOAuth
	GoogleEnabled  bool
	GoogleClientID string
	GoogleSecret   string
}

var (
	stateMu    sync.Mutex
	stateCache = map[string]string{}
)

func (h *AuthHandler) GitHubLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := generateState()
		stateMu.Lock()
		stateCache[state] = "github"
		stateMu.Unlock()
		http.Redirect(w, r, h.GitHub.AuthorizeURL(state), http.StatusFound)
	}
}

func (h *AuthHandler) GoogleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := generateState()
		stateMu.Lock()
		stateCache[state] = "google"
		stateMu.Unlock()
		redirectURI := h.BaseURL + "/api/auth/oauth/google/callback"
		http.Redirect(w, r,
			auth.GoogleLoginURL(h.GoogleClientID, redirectURI, state),
			http.StatusFound)
	}
}

func (h *AuthHandler) GitHubCallback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		stateMu.Lock()
		expected := stateCache[state]
		delete(stateCache, state)
		stateMu.Unlock()

		if state == "" || expected != "github" {
			writeError(w, http.StatusBadRequest, "invalid state parameter")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			writeError(w, http.StatusBadRequest, "missing code")
			return
		}

		ghUser, err := h.GitHub.ExchangeCode(code)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("github auth failed: %v", err))
			return
		}

		user, err := h.DB.UpsertUser("github", fmt.Sprintf("%d", ghUser.ID),
			ghUser.Email, ghUser.Name, ghUser.AvatarURL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("save user: %v", err))
			return
		}

		h.issueTokensAndRespond(w, r, user)
	}
}

func (h *AuthHandler) GoogleCallback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		stateMu.Lock()
		expected := stateCache[state]
		delete(stateCache, state)
		stateMu.Unlock()

		if state == "" || expected != "google" {
			writeError(w, http.StatusBadRequest, "invalid state parameter")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			writeError(w, http.StatusBadRequest, "missing code")
			return
		}

		gOAuth := auth.NewGoogleOAuth(h.GoogleClientID, h.GoogleSecret,
			h.BaseURL+"/api/auth/oauth/google/callback")

		googleUser, err := gOAuth.ExchangeCode(code)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("google auth failed: %v", err))
			return
		}

		name := googleUser.Name
		if name == "" {
			name = googleUser.Email
		}

		user, err := h.DB.UpsertUser("google", googleUser.ID,
			googleUser.Email, name, googleUser.Picture)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("save user: %v", err))
			return
		}

		h.issueTokensAndRespond(w, r, user)
	}
}

func (h *AuthHandler) RefreshToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.RefreshToken == "" {
			writeError(w, http.StatusBadRequest, "refreshToken is required")
			return
		}

		userID, err := h.DB.ValidateAndConsumeRefreshToken(req.RefreshToken)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}

		user, err := h.DB.GetUserByID(userID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, "user not found")
			return
		}

		h.issueTokensAndRespond(w, r, user)
	}
}

func (h *AuthHandler) Me() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"userId":   claims.UserID,
			"provider": claims.Provider,
			"name":     claims.Name,
			"email":    claims.Email,
		})
	}
}

func (h *AuthHandler) MobileCallback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		refresh := r.URL.Query().Get("refresh")
		expiresIn := r.URL.Query().Get("expires_in")

		customScheme := "mobilevc://oauth-callback?" +
			"access_token=" + url.QueryEscape(token) +
			"&refresh_token=" + url.QueryEscape(refresh) +
			"&expires_in=" + url.QueryEscape(expiresIn)

		// iOS Safari doesn't reliably follow 302 redirects to custom schemes.
		// Use an HTML page with JS + manual tap link as fallback.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<script>window.location.href = '%[1]s';</script>
</head><body style="display:flex;align-items:center;justify-content:center;height:100vh;margin:0;font-family:-apple-system,sans-serif;background:%[3]s;color:%[4]s">
<div style="text-align:center;padding:20px">
<p>登录成功，正在返回 App...</p>
<a href="%[2]s" style="display:inline-block;margin-top:16px;padding:12px 24px;background:%[4]s;color:%[3]s;border-radius:8px;text-decoration:none;font-weight:500">没自动跳转？点这里</a>
</div>
</body></html>`,
			customScheme, htmlEscape(customScheme), "#1a1d27", "#e1e4eb")
	}
	}

func (h *AuthHandler) issueTokensAndRespond(w http.ResponseWriter, r *http.Request, user *db.User) {
	accessToken, err := h.JWT.GenerateAccessToken(user.ID, user.Provider, user.Name, user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate access token failed")
		return
	}

	_, refreshToken, err := h.DB.CreateRefreshToken(user.ID, h.JWT.RefreshTokenTTLDays())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate refresh token failed")
		return
	}

	// Browsers get redirected through mobile-callback; API clients get JSON.
	if r != nil && strings.Contains(r.Header.Get("Accept"), "text/html") {
		redirectURL := fmt.Sprintf("%s/api/auth/mobile-callback?token=%s&refresh=%s&expires_in=%d",
			h.BaseURL,
			url.QueryEscape(accessToken),
			url.QueryEscape(refreshToken),
			h.JWT.AccessTokenTTLMinutes()*60,
		)
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
		"expiresIn":    h.JWT.AccessTokenTTLMinutes() * 60,
		"user": map[string]any{
			"id":        user.ID,
			"provider":  user.Provider,
			"name":      user.Name,
			"email":     user.Email,
			"avatarUrl": user.AvatarURL,
		},
	})
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func (h *AuthHandler) EmailRegister() http.HandlerFunc {
	type reqBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(body.Email))
		password := strings.TrimSpace(body.Password)
		name := strings.TrimSpace(body.Name)
		if email == "" || password == "" {
			writeError(w, http.StatusBadRequest, "email and password are required")
			return
		}
		if len(password) < 6 {
			writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
			return
		}
		if name == "" {
			name = email[:strings.IndexByte(email, '@')]
		}

		existing, _ := h.DB.GetUserByEmail(email)
		if existing != nil {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}

		user, err := h.DB.CreateEmailUser(email, name, string(hash))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create user: "+err.Error())
			return
		}

		accessToken, err := h.JWT.GenerateAccessToken(user.ID, "email", user.Name, user.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate token failed")
			return
		}
		_, refreshToken, err := h.DB.CreateRefreshToken(user.ID, h.JWT.RefreshTokenTTLDays())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate refresh token failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"accessToken":  accessToken,
			"refreshToken": refreshToken,
			"expiresIn":    h.JWT.AccessTokenTTLMinutes() * 60,
			"user":         user,
		})
	}
}

func (h *AuthHandler) EmailLogin() http.HandlerFunc {
	type reqBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(body.Email))
		password := strings.TrimSpace(body.Password)
		if email == "" || password == "" {
			writeError(w, http.StatusBadRequest, "email and password are required")
			return
		}

		user, err := h.DB.GetUserByEmail(email)
		if err != nil || user == nil {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}

		hash, err := h.DB.GetUserPasswordHash(user.ID)
		if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}

		accessToken, err := h.JWT.GenerateAccessToken(user.ID, "email", user.Name, user.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate token failed")
			return
		}
		_, refreshToken, err := h.DB.CreateRefreshToken(user.ID, h.JWT.RefreshTokenTTLDays())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate refresh token failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"accessToken":  accessToken,
			"refreshToken": refreshToken,
			"expiresIn":    h.JWT.AccessTokenTTLMinutes() * 60,
			"user":         user,
		})
	}
}

func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
