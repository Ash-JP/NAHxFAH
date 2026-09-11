-- 000004_create_anchors.up.sql

CREATE TABLE IF NOT EXISTS anchors (
    id                BIGSERIAL PRIMARY KEY,
    anchor_id         TEXT        NOT NULL UNIQUE,
    name              TEXT        NOT NULL,
    x                 DOUBLE PRECISION NOT NULL,
    y                 DOUBLE PRECISION NOT NULL,
    z                 DOUBLE PRECISION NOT NULL,
    coordinate_system TEXT        NOT NULL DEFAULT 'local',
    metadata          JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_anchors_anchor_id ON anchors (anchor_id);

COMMENT ON TABLE anchors IS 'Named reference points in the local coordinate system. Primarily for future ARCore mobile integration.';
COMMENT ON COLUMN anchors.anchor_id         IS 'Unique identifier for this anchor point.';
COMMENT ON COLUMN anchors.coordinate_system IS 'Coordinate system: local, enu, gps.';
COMMENT ON COLUMN anchors.metadata          IS 'Arbitrary JSON metadata (floor, description, tags, etc).';
