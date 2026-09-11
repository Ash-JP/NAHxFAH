// Package handlers provides HTTP handlers for the WIFI HUNTER AR REST API.
package handlers

import (
	"net/http"

	"github.com/nahxfah/wifi-hunter-server/internal/repository"
)

// ObservationsHandler provides REST endpoints for raw observations.
type ObservationsHandler struct {
	obsRepo *repository.ObservationRepository
}

// NewObservationsHandler creates an ObservationsHandler.
func NewObservationsHandler(obsRepo *repository.ObservationRepository) *ObservationsHandler {
	return &ObservationsHandler{obsRepo: obsRepo}
}

// List handles GET /api/observations.
func (h *ObservationsHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)

	obs, err := h.obsRepo.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list observations")
		return
	}
	total, _ := h.obsRepo.Count(r.Context())
	writeJSON(w, map[string]interface{}{
		"data":  obs,
		"count": len(obs),
		"total": total,
	})
}
