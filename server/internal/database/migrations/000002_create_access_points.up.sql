-- 000002_create_access_points.up.sql

CREATE TABLE IF NOT EXISTS access_points (
    id                BIGSERIAL PRIMARY KEY,
    bssid             TEXT        NOT NULL UNIQUE,
    ssid              TEXT,
    estimated_x       DOUBLE PRECISION,
    estimated_y       DOUBLE PRECISION,
    estimated_z       DOUBLE PRECISION,
    coordinate_system TEXT        NOT NULL DEFAULT 'local',
    confidence        DOUBLE PRECISION,
    error_radius_m    DOUBLE PRECISION,
    observation_count BIGINT      NOT NULL DEFAULT 0,
    hub_count         INT         NOT NULL DEFAULT 0,
    status            TEXT        NOT NULL DEFAULT 'unknown',
    first_seen        TIMESTAMPTZ,
    last_seen         TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ap_bssid     ON access_points (bssid);
CREATE INDEX IF NOT EXISTS idx_ap_status    ON access_points (status);
CREATE INDEX IF NOT EXISTS idx_ap_last_seen ON access_points (last_seen);

COMMENT ON TABLE access_points IS 'Aggregated state of each observed Wi-Fi access point.';
COMMENT ON COLUMN access_points.bssid          IS 'MAC address in uppercase AA:BB:CC:DD:EE:FF (may be SHA-256 hash when BSSID_HASHING=true).';
COMMENT ON COLUMN access_points.estimated_x    IS 'Estimated AP East position in local coordinates (metres).';
COMMENT ON COLUMN access_points.estimated_y    IS 'Estimated AP North position in local coordinates (metres).';
COMMENT ON COLUMN access_points.estimated_z    IS 'Estimated AP Up position in local coordinates (metres).';
COMMENT ON COLUMN access_points.confidence     IS 'Localization confidence 0.0–1.0.';
COMMENT ON COLUMN access_points.error_radius_m IS 'Estimated position uncertainty radius in metres.';
COMMENT ON COLUMN access_points.status         IS 'unknown, insufficient_data, localized, unstable, stale.';
