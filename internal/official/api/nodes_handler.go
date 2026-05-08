package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"mobilevc/internal/official/db"
)

type NodesHandler struct {
	DB *db.DB
}

func (h *NodesHandler) ListNodes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}

		nodes, err := h.DB.ListAllNodes()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list nodes")
			return
		}
		if nodes == nil {
			nodes = []db.Node{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	}
}

func (h *NodesHandler) RegisterNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}

		var req struct {
			NodeID   string `json:"nodeId"`
			Name     string `json:"name"`
			Version  string `json:"version"`
			StunHost string `json:"stunHost"`
			TurnPort string `json:"turnPort"`
			TurnUser string `json:"turnUser"`
			TurnPass string `json:"turnPass"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		nodeID := strings.TrimSpace(req.NodeID)
		if nodeID == "" {
			nodeID = h.DB.GenerateNodeID()
		}

		node, err := h.DB.UpsertNode(nodeID, claims.UserID,
			req.Name, req.Version,
			req.StunHost, req.TurnPort, req.TurnUser, req.TurnPass)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to register node")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"node": node})
	}
}

func (h *NodesHandler) DeregisterNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}

		nodeID := r.PathValue("id")

		node, err := h.DB.GetNode(nodeID)
		if err != nil || node == nil || node.UserID != claims.UserID {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		if err := h.DB.DeleteNode(nodeID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to deregister node")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (h *NodesHandler) Heartbeat() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}

		var req struct {
			NodeID string `json:"nodeId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := h.DB.UpdateNodeHeartbeat(req.NodeID); err != nil {
			writeError(w, http.StatusInternalServerError, "heartbeat failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
