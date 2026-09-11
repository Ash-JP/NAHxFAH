-- 000001_create_hubs.up.sql

CREATE TABLE IF NOT EXISTS hubs (
    id                BIGSERIAL PRIMARY KEY,
    hub_id            TEXT        NOT NULL UNIQUE,
    device_type       TEXT        NOT NULL DEFAULT 'windows_laptop',
    platform          TEXT        NOT NULL DEFAULT 'windows',
    version           TEXT        NOT NULL DEFAULT '1.0.0',
    coordinate_system TEXT        NOT NULL DEFAULT 'local',
    x                 DOUBLE PRECISION,
    y                 DOUBLE PRECISION,
    z                 DOUBLE PRECISION,
    latitude          DOUBLE PRECISION,
    longitude         DOUBLE PRECISION,
    altitude          DOUBLE PRECISION,
    status            TEXT        NOT NULL DEFAULT 'offline',
    last_seen         TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hubs_hub_id    ON hubs (hub_id);
CREATE INDEX IF NOT EXISTS idx_hubs_status    ON hubs (status);
CREATE INDEX IF NOT EXISTS idx_hubs_last_seen ON hubs (last_seen);

COMMENT ON TABLE hubs IS 'Registered Wi-Fi scanning hubs (Windows laptops or future Android devices).';
COMMENT ON COLUMN hubs.hub_id            IS 'Unique human-readable identifier e.g. HUB-A7F32C.';
COMMENT ON COLUMN hubs.coordinate_system IS 'Coordinate system for position: local, enu, gps.';
COMMENT ON COLUMN hubs.x                 IS 'East/right in local coordinates (metres).';
COMMENT ON COLUMN hubs.y                 IS 'North/forward in local coordinates (metres).';
COMMENT ON COLUMN hubs.z                 IS 'Up in local coordinates (metres).';
COMMENT ON COLUMN hubs.status            IS 'Connection status: online, stale, offline.';
