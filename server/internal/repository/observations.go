// Package repository provides database access for observations.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nahxfah/wifi-hunter-server/internal/models"
)

// ObservationRepository handles database operations for raw observations.
type ObservationRepository struct {
	pool *pgxpool.Pool
}

// NewObservationRepository creates a new ObservationRepository.
func NewObservationRepository(pool *pgxpool.Pool) *ObservationRepository {
	return &ObservationRepository{pool: pool}
}

// Insert stores a single raw observation.
// Raw RSSI is stored as-is and never overwritten.
func (r *ObservationRepository) Insert(ctx context.Context, obs *models.InsertObservation) (int64, error) {
	query := `
		INSERT INTO observations
		    (hub_id, bssid, ssid, rssi_dbm, link_quality, frequency_mhz, channel, x, y, z, timestamp, ingested_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		RETURNING id`

	var id int64
	err := r.pool.QueryRow(ctx, query,
		obs.HubID,
		obs.BSSID,
		obs.SSID,
		obs.RSSIDbm,
		obs.LinkQuality,
		obs.FrequencyMHz,
		obs.Channel,
		obs.X, obs.Y, obs.Z,
		obs.Timestamp,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting observation: %w", err)
	}
	return id, nil
}

// BulkInsert stores multiple observations in a single transaction.
func (r *ObservationRepository) BulkInsert(ctx context.Context, observations []*models.InsertObservation) error {
	if len(observations) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO observations
		    (hub_id, bssid, ssid, rssi_dbm, link_quality, frequency_mhz, channel, x, y, z, timestamp, ingested_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())`

	for _, obs := range observations {
		_, err := tx.Exec(ctx, query,
			obs.HubID,
			obs.BSSID,
			obs.SSID,
			obs.RSSIDbm,
			obs.LinkQuality,
			obs.FrequencyMHz,
			obs.Channel,
			obs.X, obs.Y, obs.Z,
			obs.Timestamp,
		)
		if err != nil {
			return fmt.Errorf("bulk inserting observation: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetRecentByBSSID returns recent observations for a BSSID within the given window.
func (r *ObservationRepository) GetRecentByBSSID(ctx context.Context, bssid string, since time.Time) ([]*models.Observation, error) {
	query := `
		SELECT id, hub_id, bssid, ssid, rssi_dbm, link_quality, frequency_mhz, channel,
		       x, y, z, timestamp, ingested_at
		FROM observations
		WHERE bssid = $1 AND timestamp >= $2 AND rssi_dbm IS NOT NULL
		ORDER BY timestamp DESC`

	rows, err := r.pool.Query(ctx, query, bssid, since)
	if err != nil {
		return nil, fmt.Errorf("querying recent observations for %q: %w", bssid, err)
	}
	defer rows.Close()

	var obs []*models.Observation
	for rows.Next() {
		o := &models.Observation{}
		if err := rows.Scan(
			&o.ID, &o.HubID, &o.BSSID, &o.SSID, &o.RSSIDbm, &o.LinkQuality, &o.FrequencyMHz, &o.Channel,
			&o.X, &o.Y, &o.Z, &o.Timestamp, &o.IngestedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning observation row: %w", err)
		}
		obs = append(obs, o)
	}
	return obs, rows.Err()
}

// GetByBSSID returns paginated observations for an AP.
func (r *ObservationRepository) GetByBSSID(ctx context.Context, bssid string, limit, offset int) ([]*models.Observation, error) {
	query := `
		SELECT id, hub_id, bssid, ssid, rssi_dbm, link_quality, frequency_mhz, channel,
		       x, y, z, timestamp, ingested_at
		FROM observations
		WHERE bssid = $1
		ORDER BY timestamp DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, query, bssid, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("querying observations for %q: %w", bssid, err)
	}
	defer rows.Close()

	var obs []*models.Observation
	for rows.Next() {
		o := &models.Observation{}
		if err := rows.Scan(
			&o.ID, &o.HubID, &o.BSSID, &o.SSID, &o.RSSIDbm, &o.LinkQuality, &o.FrequencyMHz, &o.Channel,
			&o.X, &o.Y, &o.Z, &o.Timestamp, &o.IngestedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning observation row: %w", err)
		}
		obs = append(obs, o)
	}
	return obs, rows.Err()
}

// DeleteOlderThan removes observations older than the given cutoff time.
// AP state (access_points table) is never deleted by this function.
func (r *ObservationRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM observations WHERE ingested_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("deleting old observations: %w", err)
	}
	return result.RowsAffected(), nil
}

// Count returns the total number of stored observations.
func (r *ObservationRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM observations`).Scan(&count)
	return count, err
}

// List returns recent observations across all BSSIDs, paginated.
func (r *ObservationRepository) List(ctx context.Context, limit, offset int) ([]*models.Observation, error) {
	query := `
		SELECT id, hub_id, bssid, ssid, rssi_dbm, link_quality, frequency_mhz, channel,
		       x, y, z, timestamp, ingested_at
		FROM observations
		ORDER BY timestamp DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing observations: %w", err)
	}
	defer rows.Close()

	var obs []*models.Observation
	for rows.Next() {
		o := &models.Observation{}
		if err := rows.Scan(
			&o.ID, &o.HubID, &o.BSSID, &o.SSID, &o.RSSIDbm, &o.LinkQuality, &o.FrequencyMHz, &o.Channel,
			&o.X, &o.Y, &o.Z, &o.Timestamp, &o.IngestedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning observation row: %w", err)
		}
		obs = append(obs, o)
	}
	return obs, rows.Err()
}
