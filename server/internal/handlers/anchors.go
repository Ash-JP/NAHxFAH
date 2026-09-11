// Package handlers provides HTTP handlers for the WIFI HUNTER AR REST API.
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nahxfah/wifi-hunter-server/internal/models"
	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
	"github.com/nahxfah/wifi-hunter-server/internal/repository"
)

// AnchorsHandler provides REST endpoints for spatial anchors.
type AnchorsHandler struct {
	anchorRepo *repository.AnchorRepository
}

// NewAnchorsHandler creates an AnchorsHandler.
func NewAnchorsHandler(anchorRepo *repository.AnchorRepository) *AnchorsHandler {
	return &AnchorsHandler{anchorRepo: anchorRepo}
}

// List handles GET /api/anchors.
func (h *AnchorsHandler) List(w http.ResponseWriter, r *http.Request) {
	anchors, err := h.anchorRepo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list anchors")
		return
	}
	writeJSON(w, map[string]interface{}{
		"data":  anchors,
		"count": len(anchors),
	})
}

// Create handles POST /api/anchors.
func (h *AnchorsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAnchorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	// Validate required fields
	if strings.TrimSpace(req.AnchorID) == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "anchor_id is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
		return
	}
	if req.CoordinateSystem == "" {
		req.CoordinateSystem = "local"
	}
	if err := protocol.ValidateCoordinateSystem(req.CoordinateSystem); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if err := protocol.ValidateCoordinates(req.X, req.Y, req.Z); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	anchor := &models.Anchor{
		AnchorID:         req.AnchorID,
		Name:             req.Name,
		X:                req.X,
		Y:                req.Y,
		Z:                req.Z,
		CoordinateSystem: req.CoordinateSystem,
		Metadata:         req.Metadata,
	}

	if err := h.anchorRepo.Create(r.Context(), anchor); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create anchor")
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]interface{}{"data": anchor})
}

// --- Shared response helpers ---

// writeJSON writes a JSON response with 200 OK.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// writeError writes a structured error response.
func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
