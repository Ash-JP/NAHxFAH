// Package repository provides database access for access points.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nahxfah/wifi-hunter-server/internal/models"
)

// AccessPointRepository handles database operations for access points.
type AccessPointRepository struct {
	pool *pgxpool.Pool
}

// NewAccessPointRepository creates a new AccessPointRepository.
func NewAccessPointRepository(pool *pgxpool.Pool) *AccessPointRepository {
	return &AccessPointRepository{pool: pool}
}

// Upsert inserts or updates an access point by BSSID.
// Increments observation_count on update.
func (r *AccessPointRepository) Upsert(ctx context.Context, ap *models.AccessPoint) error {
	query := `
		INSERT INTO access_points (bssid, ssid, coordinate_system, observation_count, hub_count,
		                           status, first_seen, last_seen, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7, NOW(), NOW())
		ON CONFLICT (bssid) DO UPDATE SET
			ssid              = COALESCE(EXCLUDED.ssid, access_points.ssid),
			observation_count = access_points.observation_count + EXCLUDED.observation_count,
			last_seen         = EXCLUDED.last_seen,
			updated_at        = NOW()
		RETURNING id, created_at, updated_at, first_seen`

	return r.pool.QueryRow(ctx, query,
		ap.BSSID,
		ap.SSID,
		ap.CoordinateSystem,
		ap.ObservationCount,
		ap.HubCount,
		string(ap.Status),
		ap.LastSeen,
	).Scan(&ap.ID, &ap.CreatedAt, &ap.UpdatedAt, &ap.FirstSeen)
}

// UpdateLocalization updates the localization result for an AP.
func (r *AccessPointRepository) UpdateLocalization(
	ctx context.Context,
	bssid string,
	x, y, z float64,
	coordinateSystem string,
	confidence float64,
	errorRadiusM float64,
	hubCount int,
	status models.APStatus,
) error {
	query := `
		UPDATE access_points
		SET estimated_x       = $2,
		    estimated_y       = $3,
		    estimated_z       = $4,
		    coordinate_system = $5,
		    confidence        = $6,
		    error_radius_m    = $7,
		    hub_count         = $8,
		    status            = $9,
		    updated_at        = NOW()
		WHERE bssid = $1`

	_, err := r.pool.Exec(ctx, query,
		bssid, x, y, z, coordinateSystem, confidence, errorRadiusM, hubCount, string(status),
	)
	return err
}

// UpdateObservationMeta updates observation count and last_seen.
func (r *AccessPointRepository) UpdateObservationMeta(ctx context.Context, bssid string, lastSeen time.Time, hubCount int) error {
	query := `
		UPDATE access_points
		SET observation_count = observation_count + 1,
		    hub_count         = GREATEST(hub_count, $2),
		    last_seen         = $3,
		    updated_at        = NOW()
		WHERE bssid = $1`

	_, err := r.pool.Exec(ctx, query, bssid, hubCount, lastSeen)
	return err
}

// GetByBSSID retrieves an access point by BSSID.
func (r *AccessPointRepository) GetByBSSID(ctx context.Context, bssid string) (*models.AccessPoint, error) {
	query := `
		SELECT id, bssid, ssid, estimated_x, estimated_y, estimated_z, coordinate_system,
		       confidence, error_radius_m, observation_count, hub_count, status,
		       first_seen, last_seen, created_at, updated_at
		FROM access_points
		WHERE bssid = $1`

	ap := &models.AccessPoint{}
	err := r.pool.QueryRow(ctx, query, bssid).Scan(
		&ap.ID, &ap.BSSID, &ap.SSID, &ap.EstimatedX, &ap.EstimatedY, &ap.EstimatedZ, &ap.CoordinateSystem,
		&ap.Confidence, &ap.ErrorRadiusM, &ap.ObservationCount, &ap.HubCount, &ap.Status,
		&ap.FirstSeen, &ap.LastSeen, &ap.CreatedAt, &ap.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting AP %q: %w", bssid, err)
	}
	return ap, nil
}

// List returns all access points ordered by observation_count descending.
func (r *AccessPointRepository) List(ctx context.Context, limit, offset int) ([]*models.AccessPoint, error) {
	query := `
		SELECT id, bssid, ssid, estimated_x, estimated_y, estimated_z, coordinate_system,
		       confidence, error_radius_m, observation_count, hub_count, status,
		       first_seen, last_seen, created_at, updated_at
		FROM access_points
		ORDER BY observation_count DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing access points: %w", err)
	}
	defer rows.Close()

	var aps []*models.AccessPoint
	for rows.Next() {
		ap := &models.AccessPoint{}
		if err := rows.Scan(
			&ap.ID, &ap.BSSID, &ap.SSID, &ap.EstimatedX, &ap.EstimatedY, &ap.EstimatedZ, &ap.CoordinateSystem,
			&ap.Confidence, &ap.ErrorRadiusM, &ap.ObservationCount, &ap.HubCount, &ap.Status,
			&ap.FirstSeen, &ap.LastSeen, &ap.CreatedAt, &ap.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning AP row: %w", err)
		}
		aps = append(aps, ap)
	}
	return aps, rows.Err()
}

// Count returns the total number of access points with optional status filter.
func (r *AccessPointRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_points`).Scan(&count)
	return count, err
}

// CountLocalized returns the count of APs with "localized" status.
func (r *AccessPointRepository) CountLocalized(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_points WHERE status = 'localized'`).Scan(&count)
	return count, err
}

// EnsureExists creates the AP record if it doesn't exist yet.
func (r *AccessPointRepository) EnsureExists(ctx context.Context, bssid string, ssid *string) error {
	query := `
		INSERT INTO access_points (bssid, ssid, coordinate_system, status, first_seen, last_seen)
		VALUES ($1, $2, 'local', 'unknown', NOW(), NOW())
		ON CONFLICT (bssid) DO NOTHING`

	_, err := r.pool.Exec(ctx, query, bssid, ssid)
	return err
}
