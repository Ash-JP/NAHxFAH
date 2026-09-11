// Package handlers provides HTTP handlers for the WIFI HUNTER AR REST API.
package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/nahxfah/wifi-hunter-server/internal/repository"
)

// AccessPointsHandler provides REST endpoints for access point data.
type AccessPointsHandler struct {
	apRepo  *repository.AccessPointRepository
	obsRepo *repository.ObservationRepository
}

// NewAccessPointsHandler creates an AccessPointsHandler.
func NewAccessPointsHandler(apRepo *repository.AccessPointRepository, obsRepo *repository.ObservationRepository) *AccessPointsHandler {
	return &AccessPointsHandler{apRepo: apRepo, obsRepo: obsRepo}
}

// List handles GET /api/access-points.
func (h *AccessPointsHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 100)
	offset := parseIntQuery(r, "offset", 0)

	aps, err := h.apRepo.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list access points")
		return
	}
	total, _ := h.apRepo.Count(r.Context())
	writeJSON(w, map[string]interface{}{
		"data":  aps,
		"count": len(aps),
		"total": total,
	})
}

// GetByBSSID handles GET /api/access-points/{bssid}.
func (h *AccessPointsHandler) GetByBSSID(w http.ResponseWriter, r *http.Request) {
	bssid := chi.URLParam(r, "bssid")
	ap, err := h.apRepo.GetByBSSID(r.Context(), bssid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to get access point")
		return
	}
	if ap == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Access point not found: "+bssid)
		return
	}
	writeJSON(w, map[string]interface{}{"data": ap})
}

// GetObservations handles GET /api/access-points/{bssid}/observations.
func (h *AccessPointsHandler) GetObservations(w http.ResponseWriter, r *http.Request) {
	bssid := chi.URLParam(r, "bssid")
	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)

	obs, err := h.obsRepo.GetByBSSID(r.Context(), bssid, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to get observations")
		return
	}
	writeJSON(w, map[string]interface{}{
		"data":  obs,
		"count": len(obs),
	})
}

func parseIntQuery(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}
