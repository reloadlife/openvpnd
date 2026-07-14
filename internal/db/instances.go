package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const instanceColumns = `
id, name, role, enabled, binary_name, binary_path,
dev_type, device, proto, local_bind, port, remotes,
server_network, topology, pool_start, pool_end, auth_mode,
cipher, data_ciphers, auth_digest,
push_routes, push_dns, push_domain, redirect_gateway,
pki_ca_path, pki_cert_path, pki_key_path, pki_tls_crypt_path, pki_dh_path,
static_key_path, extra_directives,
pre_up, post_up, pre_down, post_down,
conf_hash, pid, last_error,
last_rx_bytes, last_tx_bytes, last_rx_bps, last_tx_bps, connected_clients,
public_endpoint,
created_at, updated_at`

func scanInstance(scanner interface {
	Scan(dest ...any) error
}) (Instance, error) {
	var i Instance
	var enabled, redirect int
	var remotes, pushRoutes, pushDNS string
	var created, updated string
	err := scanner.Scan(
		&i.ID, &i.Name, &i.Role, &enabled, &i.BinaryName, &i.BinaryPath,
		&i.DevType, &i.Device, &i.Proto, &i.LocalBind, &i.Port, &remotes,
		&i.ServerNetwork, &i.Topology, &i.PoolStart, &i.PoolEnd, &i.AuthMode,
		&i.Cipher, &i.DataCiphers, &i.AuthDigest,
		&pushRoutes, &pushDNS, &i.PushDomain, &redirect,
		&i.PKICaPath, &i.PKICertPath, &i.PKIKeyPath, &i.PKITLSCryptPath, &i.PKIDHPath,
		&i.StaticKeyPath, &i.ExtraDirectives,
		&i.PreUp, &i.PostUp, &i.PreDown, &i.PostDown,
		&i.ConfHash, &i.PID, &i.LastError,
		&i.LastRxBytes, &i.LastTxBytes, &i.LastRxBps, &i.LastTxBps, &i.ConnectedClients,
		&i.PublicEndpoint,
		&created, &updated,
	)
	if err != nil {
		return Instance{}, err
	}
	i.Enabled = enabled != 0
	i.RedirectGateway = redirect != 0
	i.Remotes = decodeRemotes(remotes)
	i.PushRoutes = decodeJSONList(pushRoutes)
	i.PushDNS = decodeJSONList(pushDNS)
	i.CreatedAt = parseTime(created)
	i.UpdatedAt = parseTime(updated)
	return i, nil
}

// CreateInstance inserts a new instance.
func (s *Store) CreateInstance(ctx context.Context, i Instance) (Instance, error) {
	if i.Name == "" {
		return Instance{}, fmt.Errorf("instance name required")
	}
	role := strings.ToLower(strings.TrimSpace(i.Role))
	if role != "server" && role != "client" {
		return Instance{}, fmt.Errorf("role must be server or client")
	}
	i.Role = role
	if i.BinaryName == "" {
		i.BinaryName = "default"
	}
	if i.DevType == "" {
		i.DevType = "tun"
	}
	if i.Proto == "" {
		i.Proto = "udp"
	}
	if i.AuthMode == "" {
		i.AuthMode = "pki"
	}
	if i.Topology == "" {
		i.Topology = "subnet"
	}
	if i.Port == 0 && role == "server" {
		i.Port = 1194
	}
	now := nowRFC3339()
	res, err := s.db.ExecContext(ctx, `
INSERT INTO instances (
  name, role, enabled, binary_name, binary_path,
  dev_type, device, proto, local_bind, port, remotes,
  server_network, topology, pool_start, pool_end, auth_mode,
  cipher, data_ciphers, auth_digest,
  push_routes, push_dns, push_domain, redirect_gateway,
  pki_ca_path, pki_cert_path, pki_key_path, pki_tls_crypt_path, pki_dh_path,
  static_key_path, extra_directives,
  pre_up, post_up, pre_down, post_down,
  conf_hash, pid, last_error,
  last_rx_bytes, last_tx_bytes, last_rx_bps, last_tx_bps, connected_clients,
  public_endpoint,
  created_at, updated_at
) VALUES (
  ?,?,?,?,?, ?,?,?,?,?,?, ?,?,?,?,?, ?,?,?, ?,?,?,?,
  ?,?,?,?,?, ?,?, ?,?,?,?,
  '', 0, '', 0,0,0,0,0, ?, ?,?
)`,
		i.Name, i.Role, boolToInt(i.Enabled), i.BinaryName, i.BinaryPath,
		i.DevType, i.Device, i.Proto, i.LocalBind, i.Port, encodeRemotes(i.Remotes),
		i.ServerNetwork, i.Topology, i.PoolStart, i.PoolEnd, i.AuthMode,
		i.Cipher, i.DataCiphers, i.AuthDigest,
		encodeJSONList(i.PushRoutes), encodeJSONList(i.PushDNS), i.PushDomain, boolToInt(i.RedirectGateway),
		i.PKICaPath, i.PKICertPath, i.PKIKeyPath, i.PKITLSCryptPath, i.PKIDHPath,
		i.StaticKeyPath, i.ExtraDirectives,
		i.PreUp, i.PostUp, i.PreDown, i.PostDown,
		i.PublicEndpoint,
		now, now,
	)
	if err != nil {
		return Instance{}, fmt.Errorf("insert instance: %w", err)
	}
	id, _ := res.LastInsertId()
	out, err := s.GetInstanceByID(ctx, id)
	if err != nil {
		return Instance{}, err
	}
	return *out, nil
}

// GetInstance returns an instance by name.
func (s *Store) GetInstance(ctx context.Context, name string) (*Instance, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+instanceColumns+` FROM instances WHERE name=?`, name)
	i, err := scanInstance(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("instance %q not found", name)
		}
		return nil, err
	}
	return &i, nil
}

// GetInstanceByID returns an instance by id.
func (s *Store) GetInstanceByID(ctx context.Context, id int64) (*Instance, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+instanceColumns+` FROM instances WHERE id=?`, id)
	i, err := scanInstance(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("instance id %d not found", id)
		}
		return nil, err
	}
	return &i, nil
}

// ListInstances returns all instances.
func (s *Store) ListInstances(ctx context.Context) ([]Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+instanceColumns+` FROM instances ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Instance
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UpdateInstance patches non-zero fields from the provided instance (full replace of mutable fields).
func (s *Store) UpdateInstance(ctx context.Context, i Instance) (Instance, error) {
	existing, err := s.GetInstance(ctx, i.Name)
	if err != nil {
		return Instance{}, err
	}
	i.ID = existing.ID
	if i.Role == "" {
		i.Role = existing.Role
	}
	now := nowRFC3339()
	_, err = s.db.ExecContext(ctx, `
UPDATE instances SET
  role=?, enabled=?, binary_name=?, binary_path=?,
  dev_type=?, device=?, proto=?, local_bind=?, port=?, remotes=?,
  server_network=?, topology=?, pool_start=?, pool_end=?, auth_mode=?,
  cipher=?, data_ciphers=?, auth_digest=?,
  push_routes=?, push_dns=?, push_domain=?, redirect_gateway=?,
  pki_ca_path=?, pki_cert_path=?, pki_key_path=?, pki_tls_crypt_path=?, pki_dh_path=?,
  static_key_path=?, extra_directives=?,
  pre_up=?, post_up=?, pre_down=?, post_down=?,
  conf_hash=?, public_endpoint=?, updated_at=?
WHERE name=?`,
		i.Role, boolToInt(i.Enabled), i.BinaryName, i.BinaryPath,
		i.DevType, i.Device, i.Proto, i.LocalBind, i.Port, encodeRemotes(i.Remotes),
		i.ServerNetwork, i.Topology, i.PoolStart, i.PoolEnd, i.AuthMode,
		i.Cipher, i.DataCiphers, i.AuthDigest,
		encodeJSONList(i.PushRoutes), encodeJSONList(i.PushDNS), i.PushDomain, boolToInt(i.RedirectGateway),
		i.PKICaPath, i.PKICertPath, i.PKIKeyPath, i.PKITLSCryptPath, i.PKIDHPath,
		i.StaticKeyPath, i.ExtraDirectives,
		i.PreUp, i.PostUp, i.PreDown, i.PostDown,
		i.ConfHash, i.PublicEndpoint, now, i.Name,
	)
	if err != nil {
		return Instance{}, err
	}
	out, err := s.GetInstance(ctx, i.Name)
	if err != nil {
		return Instance{}, err
	}
	return *out, nil
}

// DeleteInstance removes an instance by name.
func (s *Store) DeleteInstance(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM instances WHERE name=?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("instance %q not found", name)
	}
	return nil
}

// SetInstanceEnabled sets enabled flag.
func (s *Store) SetInstanceEnabled(ctx context.Context, name string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE instances SET enabled=?, updated_at=? WHERE name=?`,
		boolToInt(enabled), nowRFC3339(), name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("instance %q not found", name)
	}
	return nil
}

// UpdateInstanceRuntime stores observed runtime fields.
func (s *Store) UpdateInstanceRuntime(ctx context.Context, name string, pid int, confHash, lastErr string, rx, tx int64, rxBps, txBps float64, clients int) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE instances SET
  pid=?, conf_hash=?, last_error=?,
  last_rx_bytes=?, last_tx_bytes=?, last_rx_bps=?, last_tx_bps=?,
  connected_clients=?, updated_at=?
WHERE name=?`,
		pid, confHash, lastErr, rx, tx, rxBps, txBps, clients, nowRFC3339(), name)
	return err
}
