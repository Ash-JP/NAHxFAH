// Package handlers provides HTTP handlers for the WIFI HUNTER AR REST API.
package handlers

import (
	"encoding/json"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
	"github.com/nahxfah/wifi-hunter-server/internal/repository"
	"github.com/nahxfah/wifi-hunter-server/internal/services"
)

// HubsHandler provides REST endpoints for hub management.
type HubsHandler struct {
	hubRepo    *repository.HubRepository
	hubManager *services.HubManager
}

// NewHubsHandler creates a HubsHandler.
func NewHubsHandler(hubRepo *repository.HubRepository, hubManager *services.HubManager) *HubsHandler {
	return &HubsHandler{hubRepo: hubRepo, hubManager: hubManager}
}

// List handles GET /api/hubs.
func (h *HubsHandler) List(w http.ResponseWriter, r *http.Request) {
	hubs, err := h.hubRepo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list hubs")
		return
	}
	writeJSON(w, map[string]interface{}{
		"data":  hubs,
		"count": len(hubs),
	})
}

// GetByID handles GET /api/hubs/{hub_id}.
func (h *HubsHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	hubID := chi.URLParam(r, "hub_id")
	hub, err := h.hubRepo.GetByID(r.Context(), hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to get hub")
		return
	}
	if hub == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Hub not found: "+hubID)
		return
	}
	writeJSON(w, map[string]interface{}{"data": hub})
}

// UpdatePosition handles POST /api/hubs/{hub_id}/position.
func (h *HubsHandler) UpdatePosition(w http.ResponseWriter, r *http.Request) {
	hubID := chi.URLParam(r, "hub_id")

	var body struct {
		CoordinateSystem string  `json:"coordinate_system"`
		X                float64 `json:"x"`
		Y                float64 `json:"y"`
		Z                float64 `json:"z"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	// Validate
	if err := protocol.ValidateCoordinateSystem(body.CoordinateSystem); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if err := protocol.ValidateCoordinates(body.X, body.Y, body.Z); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if math.IsInf(body.X, 0) || math.IsInf(body.Y, 0) || math.IsInf(body.Z, 0) {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Coordinates must be finite numbers")
		return
	}

	// Verify hub exists
	exists, err := h.hubRepo.Exists(r.Context(), hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to check hub")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Hub not found: "+hubID)
		return
	}

	if err := h.hubManager.UpdatePosition(r.Context(), hubID, body.X, body.Y, body.Z, body.CoordinateSystem); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update position")
		return
	}

	writeJSON(w, map[string]interface{}{
		"data": map[string]interface{}{
			"hub_id":            hubID,
			"coordinate_system": body.CoordinateSystem,
			"x":                 body.X,
			"y":                 body.Y,
			"z":                 body.Z,
		},
	})
}
