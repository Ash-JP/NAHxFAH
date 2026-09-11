// Package repository provides database access for hubs.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nahxfah/wifi-hunter-server/internal/models"
)

// HubRepository handles database operations for hubs.
type HubRepository struct {
	pool *pgxpool.Pool
}

// NewHubRepository creates a new HubRepository.
func NewHubRepository(pool *pgxpool.Pool) *HubRepository {
	return &HubRepository{pool: pool}
}

// Upsert inserts a new hub or updates an existing one (by hub_id).
func (r *HubRepository) Upsert(ctx context.Context, hub *models.Hub) error {
	query := `
		INSERT INTO hubs (hub_id, device_type, platform, version, coordinate_system, status, last_seen, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (hub_id) DO UPDATE SET
			device_type       = EXCLUDED.device_type,
			platform          = EXCLUDED.platform,
			version           = EXCLUDED.version,
			status            = EXCLUDED.status,
			last_seen         = EXCLUDED.last_seen,
			updated_at        = NOW()
		RETURNING id, created_at, updated_at`

	return r.pool.QueryRow(ctx, query,
		hub.HubID,
		hub.DeviceType,
		hub.Platform,
		hub.Version,
		hub.CoordinateSystem,
		hub.Status,
		hub.LastSeen,
	).Scan(&hub.ID, &hub.CreatedAt, &hub.UpdatedAt)
}

// UpdatePosition updates the hub's position coordinates.
func (r *HubRepository) UpdatePosition(ctx context.Context, hubID string, x, y, z float64, coordinateSystem string) error {
	query := `
		UPDATE hubs
		SET x = $2, y = $3, z = $4, coordinate_system = $5, updated_at = NOW()
		WHERE hub_id = $1`

	result, err := r.pool.Exec(ctx, query, hubID, x, y, z, coordinateSystem)
	if err != nil {
		return fmt.Errorf("updating hub position: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("hub %q not found", hubID)
	}
	return nil
}

// UpdateStatus updates the hub's status and last_seen timestamp.
func (r *HubRepository) UpdateStatus(ctx context.Context, hubID string, status models.HubStatus, lastSeen time.Time) error {
	query := `
		UPDATE hubs
		SET status = $2, last_seen = $3, updated_at = NOW()
		WHERE hub_id = $1`

	_, err := r.pool.Exec(ctx, query, hubID, string(status), lastSeen)
	return err
}

// GetByID retrieves a hub by its hub_id.
func (r *HubRepository) GetByID(ctx context.Context, hubID string) (*models.Hub, error) {
	query := `
		SELECT id, hub_id, device_type, platform, version, coordinate_system,
		       x, y, z, latitude, longitude, altitude,
		       status, last_seen, created_at, updated_at
		FROM hubs
		WHERE hub_id = $1`

	hub := &models.Hub{}
	err := r.pool.QueryRow(ctx, query, hubID).Scan(
		&hub.ID, &hub.HubID, &hub.DeviceType, &hub.Platform, &hub.Version, &hub.CoordinateSystem,
		&hub.X, &hub.Y, &hub.Z, &hub.Latitude, &hub.Longitude, &hub.Altitude,
		&hub.Status, &hub.LastSeen, &hub.CreatedAt, &hub.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting hub %q: %w", hubID, err)
	}
	return hub, nil
}

// List returns all hubs ordered by last_seen descending.
func (r *HubRepository) List(ctx context.Context) ([]*models.Hub, error) {
	query := `
		SELECT id, hub_id, device_type, platform, version, coordinate_system,
		       x, y, z, latitude, longitude, altitude,
		       status, last_seen, created_at, updated_at
		FROM hubs
		ORDER BY last_seen DESC NULLS LAST`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing hubs: %w", err)
	}
	defer rows.Close()

	var hubs []*models.Hub
	for rows.Next() {
		hub := &models.Hub{}
		if err := rows.Scan(
			&hub.ID, &hub.HubID, &hub.DeviceType, &hub.Platform, &hub.Version, &hub.CoordinateSystem,
			&hub.X, &hub.Y, &hub.Z, &hub.Latitude, &hub.Longitude, &hub.Altitude,
			&hub.Status, &hub.LastSeen, &hub.CreatedAt, &hub.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning hub row: %w", err)
		}
		hubs = append(hubs, hub)
	}
	return hubs, rows.Err()
}

// Exists returns true if a hub with the given hub_id exists.
func (r *HubRepository) Exists(ctx context.Context, hubID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hubs WHERE hub_id = $1)`, hubID).Scan(&exists)
	return exists, err
}
