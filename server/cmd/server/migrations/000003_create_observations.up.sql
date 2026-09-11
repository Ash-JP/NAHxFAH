-- 000003_create_observations.up.sql

CREATE TABLE IF NOT EXISTS observations (
    id            BIGSERIAL PRIMARY KEY,
    hub_id        TEXT             NOT NULL,
    bssid         TEXT             NOT NULL,
    ssid          TEXT,
    rssi_dbm      INT,
    link_quality  INT,
    frequency_mhz INT,
    channel       INT,
    x             DOUBLE PRECISION,
    y             DOUBLE PRECISION,
    z             DOUBLE PRECISION,
    timestamp     TIMESTAMPTZ      NOT NULL,
    ingested_at   TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_obs_bssid       ON observations (bssid);
CREATE INDEX IF NOT EXISTS idx_obs_hub_id      ON observations (hub_id);
CREATE INDEX IF NOT EXISTS idx_obs_timestamp   ON observations (timestamp);
CREATE INDEX IF NOT EXISTS idx_obs_ingested_at ON observations (ingested_at);

-- Composite index for localization window queries
CREATE INDEX IF NOT EXISTS idx_obs_bssid_ts ON observations (bssid, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_obs_hub_bssid ON observations (hub_id, bssid, timestamp DESC);

COMMENT ON TABLE observations IS 'Raw Wi-Fi scan observations. Raw RSSI is never overwritten with processed values.';
COMMENT ON COLUMN observations.hub_id       IS 'Hub that captured this observation.';
COMMENT ON COLUMN observations.bssid        IS 'AP BSSID (normalized uppercase or hashed).';
COMMENT ON COLUMN observations.rssi_dbm     IS 'Raw RSSI in dBm as reported by the Windows WLAN API. NULL if unavailable.';
COMMENT ON COLUMN observations.link_quality IS 'Windows link quality 0–100. Separate from RSSI.';
COMMENT ON COLUMN observations.x            IS 'Hub East position when observation was captured.';
COMMENT ON COLUMN observations.y            IS 'Hub North position when observation was captured.';
COMMENT ON COLUMN observations.z            IS 'Hub Up position when observation was captured.';
