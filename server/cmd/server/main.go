// Command server is the WIFI HUNTER AR Go backend server.
//
// Startup sequence:
//  1. Load configuration from environment variables
//  2. Initialize structured logging
//  3. Connect to PostgreSQL
//  4. Run database migrations
//  5. Initialize repositories and services
//  6. Initialize WebSocket manager
//  7. Start background workers
//  8. Mount Chi router with all routes
//  9. Start HTTP server on configured address
// 10. Listen for SIGINT/SIGTERM → graceful shutdown
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/nahxfah/wifi-hunter-server/internal/config"
	"github.com/nahxfah/wifi-hunter-server/internal/database"
	"github.com/nahxfah/wifi-hunter-server/internal/handlers"
	"github.com/nahxfah/wifi-hunter-server/internal/models"
	"github.com/nahxfah/wifi-hunter-server/internal/protocol"
	"github.com/nahxfah/wifi-hunter-server/internal/repository"
	"github.com/nahxfah/wifi-hunter-server/internal/services"
	wshandler "github.com/nahxfah/wifi-hunter-server/internal/websocket"

	"embed"
)

// migrations are embedded from the sibling directory at build time.
// The Go embed directive uses paths relative to this source file.
//
//go:embed all:migrations
var migrationsFS embed.FS

//go:embed web/index.html
var dashboardHTML []byte

const version = "1.0.0"

func main() {
	// 1. Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize structured logging
	logLevel := new(slog.LevelVar)
	logLevel.Set(cfg.LogLevelValue())
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("WIFI HUNTER AR server starting", "version", version, "addr", cfg.Addr())

	// 3. Connect to PostgreSQL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	// 4. Run migrations
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		slog.Error("failed to run migrations", "error", err.Error())
		os.Exit(1)
	}
	slog.Info("database migrations applied")

	// 5. Initialize repositories
	hubRepo := repository.NewHubRepository(pool)
	apRepo := repository.NewAccessPointRepository(pool)
	obsRepo := repository.NewObservationRepository(pool)
	anchorRepo := repository.NewAnchorRepository(pool)

	// 6. Initialize WebSocket manager
	wsMgr := wshandler.NewManager()

	// 7. Initialize services
	sigProc := services.NewSignalProcessor(cfg.RSSISmoothingAlpha)

	locCfg := services.LocalizationConfig{
		MinHubs:               cfg.LocalizationMinHubs,
		WindowSeconds:         cfg.LocalizationWindowSeconds,
		GridResolution:        cfg.LocalizationGridResolution,
		GridPaddingM:          5.0,
		PositionAlpha:         cfg.PositionSmoothingAlpha,
		PathLossA:             cfg.PathLossA,
		PathLossN:             cfg.PathLossN,
		RSSISigma:             6.0,
		InstabilityThresholdM: 8.0,
		OutlierMADThreshold:   3.5,
	}
	localizer := services.NewLocalizationEngine(locCfg, sigProc)

	// broadcast sends AP updates to mobile + dashboard clients
	broadcast := func(msg *protocol.APUpdateMessage) {
		wsMgr.BroadcastToAll(msg)
	}

	hubManager := services.NewHubManager(
		hubRepo,
		cfg.HubStaleThresholdSeconds,
		cfg.HubOfflineThresholdSeconds,
		func(hubID string, status models.HubStatus) {
			statusMsg := map[string]interface{}{
				"type":   string(protocol.MsgHubStatus),
				"hub_id": hubID,
				"status": string(status),
			}
			wsMgr.BroadcastToDashboard(statusMsg)
		},
	)

	obsSvcCfg := services.ObservationServiceConfig{
		BSSIDHashing:  cfg.BSSIDHashing,
		ServerSecret:  cfg.ServerSecret,
		WindowSeconds: cfg.LocalizationWindowSeconds,
	}

	obsSvc := services.NewObservationService(
		hubManager, sigProc, localizer,
		obsRepo, apRepo,
		obsSvcCfg, broadcast,
	)

	// 8. Start background workers
	go hubManager.MonitorStatus(ctx, 5*time.Second)

	// Observation cleanup worker (runs hourly)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				deleted, err := obsSvc.CleanupOldObservations(ctx, cfg.ObservationRetentionHours)
				if err != nil {
					slog.Error("observation cleanup failed", "error", err.Error())
				} else if deleted > 0 {
					slog.Info("observation cleanup completed", "deleted", deleted)
				}
			}
		}
	}()

	// 9. Build HTTP handlers
	healthHandler := handlers.NewHealthHandler(pool, version)
	hubsHandler := handlers.NewHubsHandler(hubRepo, hubManager)
	apsHandler := handlers.NewAccessPointsHandler(apRepo, obsRepo)
	obsHandler := handlers.NewObservationsHandler(obsRepo)
	anchorsHandler := handlers.NewAnchorsHandler(anchorRepo)

	// Build WebSocket handlers
	hubWSHandler := wshandler.NewHubHandler(wsMgr, hubManager, obsSvc, cfg.HubAPIKey)
	mobileWSHandler := wshandler.NewMobileHandler(wsMgr, cfg.HubAPIKey)
	dashWSHandler := wshandler.NewDashboardHandler(wsMgr, dashboardHTML)
	universalWSHandler := wshandler.NewUniversalHandler(wsMgr, hubManager, obsSvc, apRepo, anchorRepo, cfg.HubAPIKey, dashboardHTML)

	// 10. Mount Chi router
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)

	// Health check
	r.Get("/health", healthHandler.ServeHTTP)

	// System status
	r.Get("/api/system/status", func(w http.ResponseWriter, req *http.Request) {
		dbStatus := "connected"
		if err := database.Ping(req.Context(), pool); err != nil {
			dbStatus = "disconnected"
		}
		obsCount, _ := obsRepo.Count(req.Context())
		apCount, _ := apRepo.Count(req.Context())
		apLocalized, _ := apRepo.CountLocalized(req.Context())
		uptime := int64(time.Since(handlers.StartTime()).Seconds())

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"uptime_seconds":    uptime,
				"version":           version,
				"connected_hubs":    hubManager.ConnectedCount(),
				"active_aps":        apCount,
				"localized_aps":     apLocalized,
				"observation_count": obsCount,
				"database_status":   dbStatus,
				"websocket_clients": map[string]int{
					"hubs":      wsMgr.HubCount(),
					"mobile":    wsMgr.MobileCount(),
					"dashboard": wsMgr.DashCount(),
				},
			},
		})
	})

	// Hub REST API
	r.Get("/api/hubs", hubsHandler.List)
	r.Get("/api/hubs/{hub_id}", hubsHandler.GetByID)
	r.Post("/api/hubs/{hub_id}/position", hubsHandler.UpdatePosition)

	// Access Point REST API
	r.Get("/api/access-points", apsHandler.List)
	r.Get("/api/access-points/{bssid}", apsHandler.GetByBSSID)
	r.Get("/api/access-points/{bssid}/observations", apsHandler.GetObservations)

	// Observations REST API
	r.Get("/api/observations", obsHandler.List)

	// Anchors REST API
	r.Get("/api/anchors", anchorsHandler.List)
	r.Post("/api/anchors", anchorsHandler.Create)

	// OpenAPI docs
	r.Get("/docs", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "OpenAPI spec: /docs/openapi.yaml — see the docs/ directory in the repository.")
	})

	// Web dashboard
	serveDashboard := func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboardHTML)
	}
	r.Get("/", serveDashboard)
	r.Get("/dashboard", serveDashboard)

	// WebSocket endpoints
	r.Get("/ws", universalWSHandler.ServeHTTP)
	r.Get("/ws/hub", hubWSHandler.ServeHTTP)
	r.Get("/ws/mobile", mobileWSHandler.ServeHTTP)
	r.Get("/ws/dashboard", dashWSHandler.ServeHTTP)

	// 11. Start HTTP server
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "addr", cfg.Addr())
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// 12. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("server error", "error", err.Error())
	case sig := <-quit:
		slog.Info("shutdown signal received", "signal", sig.String())
	}

	slog.Info("shutting down server...")
	cancel() // signal background workers to stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err.Error())
	}

	slog.Info("server stopped cleanly")
}

// runMigrations applies pending SQL migrations embedded in the binary.
func runMigrations(databaseURL string) error {
	srcDrv, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("creating migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", srcDrv, databaseURL)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// requestLogger is a Chi-compatible structured request logger.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}
