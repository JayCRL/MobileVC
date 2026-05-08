package api

import (
	"encoding/json"
	"net/http"

	"mobilevc/internal/official/auth"
	"mobilevc/internal/official/db"
	"mobilevc/internal/official/signaling"
)

type AdminHandler struct {
	DB              *db.DB
	Hub             *signaling.Hub
	JWT             *auth.JWTService
	RefreshTokenTTL int
}

func (h *AdminHandler) Dashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStats, err := h.DB.Stats()
		if err != nil {
			dbStats = db.DBStats{}
		}

		hubStats := h.Hub.Stats()

		users, _ := h.DB.ListAllUsers()
		if users == nil {
			users = []db.User{}
		}

		nodes, _ := h.DB.ListAllNodes()
		if nodes == nil {
			nodes = []db.Node{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"db":    dbStats,
			"hub":   hubStats,
			"users": users,
			"nodes": nodes,
		})
	}
}

func (h *AdminHandler) GenerateNodeToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Allow empty body
		}
		if req.Name == "" {
			req.Name = "admin-node"
		}

		// Upsert admin user
		user, err := h.DB.UpsertUser("admin", "admin", "", req.Name, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create admin user failed")
			return
		}

		// Generate access token
		accessToken, err := h.JWT.GenerateAccessToken(user.ID, user.Provider, user.Name, user.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate token failed")
			return
		}

		// Generate refresh token
		_, refreshToken, err := h.DB.CreateRefreshToken(user.ID, h.RefreshTokenTTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate refresh token failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"accessToken":  accessToken,
			"refreshToken": refreshToken,
			"userId":       user.ID,
			"name":         user.Name,
			"expiresIn":    h.JWT.AccessTokenTTLMinutes() * 60,
		})
	}
}
