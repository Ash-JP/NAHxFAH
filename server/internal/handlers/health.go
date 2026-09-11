// Package handlers provides HTTP handlers for the WIFI HUNTER AR REST API.
package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nahxfah/wifi-hunter-server/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

var startTime = time.Now()

// StartTime returns when the server started (for uptime calculation).
func StartTime() time.Time { return startTime }

// HealthHandler returns server and database health status.
type HealthHandler struct {
	pool    *pgxpool.Pool
	version string
}

// NewHealthHandler creates a HealthHandler.
func NewHealthHandler(pool *pgxpool.Pool, version string) *HealthHandler {
	return &HealthHandler{pool: pool, version: version}
}

// ServeHTTP handles GET /health.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dbStatus := "connected"
	statusCode := http.StatusOK

	if err := database.Ping(r.Context(), h.pool); err != nil {
		dbStatus = "disconnected"
		statusCode = http.StatusServiceUnavailable
	}

	uptime := int64(time.Since(startTime).Seconds())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          boolToStatus(statusCode == http.StatusOK),
		"service":         "wifi-hunter-server",
		"version":         h.version,
		"database":        dbStatus,
		"uptime_seconds":  uptime,
	})
}

func boolToStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "degraded"
}
