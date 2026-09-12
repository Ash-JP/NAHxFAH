// Package repository provides database access for anchors.
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nahxfah/wifi-hunter-server/internal/models"
)

// AnchorRepository handles database operations for spatial anchors.
type AnchorRepository struct {
	pool *pgxpool.Pool
}

// NewAnchorRepository creates a new AnchorRepository.
func NewAnchorRepository(pool *pgxpool.Pool) *AnchorRepository {
	return &AnchorRepository{pool: pool}
}

// Create inserts a new anchor or updates an existing anchor by anchor_id.
func (r *AnchorRepository) Create(ctx context.Context, anchor *models.Anchor) error {
	query := `
		INSERT INTO anchors (anchor_id, name, x, y, z, coordinate_system, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (anchor_id) DO UPDATE SET
			name = EXCLUDED.name,
			x = EXCLUDED.x,
			y = EXCLUDED.y,
			z = EXCLUDED.z,
			coordinate_system = EXCLUDED.coordinate_system,
			metadata = COALESCE(EXCLUDED.metadata, anchors.metadata)
		RETURNING id, created_at`

	return r.pool.QueryRow(ctx, query,
		anchor.AnchorID,
		anchor.Name,
		anchor.X,
		anchor.Y,
		anchor.Z,
		anchor.CoordinateSystem,
		anchor.Metadata,
	).Scan(&anchor.ID, &anchor.CreatedAt)
}

// GetByID retrieves an anchor by its anchor_id.
func (r *AnchorRepository) GetByID(ctx context.Context, anchorID string) (*models.Anchor, error) {
	query := `
		SELECT id, anchor_id, name, x, y, z, coordinate_system, metadata, created_at
		FROM anchors
		WHERE anchor_id = $1`

	anchor := &models.Anchor{}
	err := r.pool.QueryRow(ctx, query, anchorID).Scan(
		&anchor.ID, &anchor.AnchorID, &anchor.Name,
		&anchor.X, &anchor.Y, &anchor.Z,
		&anchor.CoordinateSystem, &anchor.Metadata, &anchor.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting anchor %q: %w", anchorID, err)
	}
	return anchor, nil
}

// List returns all anchors.
func (r *AnchorRepository) List(ctx context.Context) ([]*models.Anchor, error) {
	query := `
		SELECT id, anchor_id, name, x, y, z, coordinate_system, metadata, created_at
		FROM anchors
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing anchors: %w", err)
	}
	defer rows.Close()

	var anchors []*models.Anchor
	for rows.Next() {
		anchor := &models.Anchor{}
		if err := rows.Scan(
			&anchor.ID, &anchor.AnchorID, &anchor.Name,
			&anchor.X, &anchor.Y, &anchor.Z,
			&anchor.CoordinateSystem, &anchor.Metadata, &anchor.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning anchor row: %w", err)
		}
		anchors = append(anchors, anchor)
	}
	return anchors, rows.Err()
}
