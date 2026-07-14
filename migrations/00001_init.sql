-- +goose Up
CREATE TABLE IF NOT EXISTS binaries (
    name TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL, -- server | client
    enabled INTEGER NOT NULL DEFAULT 1,
    binary_name TEXT NOT NULL DEFAULT 'default',
    binary_path TEXT NOT NULL DEFAULT '',
    dev_type TEXT NOT NULL DEFAULT 'tun',
    device TEXT NOT NULL DEFAULT '',
    proto TEXT NOT NULL DEFAULT 'udp',
    local_bind TEXT NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 1194,
    remotes TEXT NOT NULL DEFAULT '[]', -- JSON [{host,port,proto}]
    server_network TEXT NOT NULL DEFAULT '',
    topology TEXT NOT NULL DEFAULT 'subnet',
    pool_start TEXT NOT NULL DEFAULT '',
    pool_end TEXT NOT NULL DEFAULT '',
    auth_mode TEXT NOT NULL DEFAULT 'pki', -- pki | static_key
    cipher TEXT NOT NULL DEFAULT '',
    data_ciphers TEXT NOT NULL DEFAULT '',
    auth_digest TEXT NOT NULL DEFAULT '',
    push_routes TEXT NOT NULL DEFAULT '[]',
    push_dns TEXT NOT NULL DEFAULT '[]',
    push_domain TEXT NOT NULL DEFAULT '',
    redirect_gateway INTEGER NOT NULL DEFAULT 0,
    pki_ca_path TEXT NOT NULL DEFAULT '',
    pki_cert_path TEXT NOT NULL DEFAULT '',
    pki_key_path TEXT NOT NULL DEFAULT '',
    pki_tls_crypt_path TEXT NOT NULL DEFAULT '',
    pki_dh_path TEXT NOT NULL DEFAULT '',
    static_key_path TEXT NOT NULL DEFAULT '',
    extra_directives TEXT NOT NULL DEFAULT '',
    pre_up TEXT NOT NULL DEFAULT '',
    post_up TEXT NOT NULL DEFAULT '',
    pre_down TEXT NOT NULL DEFAULT '',
    post_down TEXT NOT NULL DEFAULT '',
    conf_hash TEXT NOT NULL DEFAULT '',
    pid INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    last_rx_bytes INTEGER NOT NULL DEFAULT 0,
    last_tx_bytes INTEGER NOT NULL DEFAULT 0,
    last_rx_bps REAL NOT NULL DEFAULT 0,
    last_tx_bps REAL NOT NULL DEFAULT 0,
    connected_clients INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_instances_role ON instances(role);
CREATE INDEX IF NOT EXISTS idx_instances_enabled ON instances(enabled);

CREATE TABLE IF NOT EXISTS clients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    common_name TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    static_ip TEXT NOT NULL DEFAULT '',
    push_routes TEXT NOT NULL DEFAULT '[]',
    suspended INTEGER NOT NULL DEFAULT 0,
    traffic_limit_bytes INTEGER NOT NULL DEFAULT 0,
    bandwidth_rx_bps INTEGER NOT NULL DEFAULT 0,
    bandwidth_tx_bps INTEGER NOT NULL DEFAULT 0,
    cert_ref TEXT NOT NULL DEFAULT '',
    rx_bytes_offset INTEGER NOT NULL DEFAULT 0,
    tx_bytes_offset INTEGER NOT NULL DEFAULT 0,
    real_address TEXT NOT NULL DEFAULT '',
    virtual_address TEXT NOT NULL DEFAULT '',
    connected_since TEXT NOT NULL DEFAULT '',
    last_rx_bytes INTEGER NOT NULL DEFAULT 0,
    last_tx_bytes INTEGER NOT NULL DEFAULT 0,
    last_rx_bps REAL NOT NULL DEFAULT 0,
    last_tx_bps REAL NOT NULL DEFAULT 0,
    tags TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(instance_id, common_name)
);

CREATE INDEX IF NOT EXISTS idx_clients_instance ON clients(instance_id);
CREATE INDEX IF NOT EXISTS idx_clients_suspended ON clients(suspended);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    kind TEXT NOT NULL DEFAULT 'system',
    instance TEXT NOT NULL DEFAULT '',
    client_cn TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL,
    meta TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts DESC);

-- +goose Down
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS clients;
DROP TABLE IF EXISTS instances;
DROP TABLE IF EXISTS binaries;
