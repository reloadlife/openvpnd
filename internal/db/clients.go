package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const clientColumns = `
c.id, c.instance_id, c.common_name, c.name, c.notes, c.static_ip, c.push_routes,
c.suspended, c.traffic_limit_bytes, c.bandwidth_rx_bps, c.bandwidth_tx_bps, c.cert_ref,
c.client_cert_path, c.client_key_path,
c.rx_bytes_offset, c.tx_bytes_offset,
c.real_address, c.virtual_address, c.connected_since,
c.last_rx_bytes, c.last_tx_bytes, c.last_rx_bps, c.last_tx_bps, c.tags,
c.created_at, c.updated_at, i.name`

func scanClient(scanner interface {
	Scan(dest ...any) error
}) (Client, error) {
	var c Client
	var suspended int
	var pushRoutes, tags string
	var created, updated string
	err := scanner.Scan(
		&c.ID, &c.InstanceID, &c.CommonName, &c.Name, &c.Notes, &c.StaticIP, &pushRoutes,
		&suspended, &c.TrafficLimitBytes, &c.BandwidthRxBps, &c.BandwidthTxBps, &c.CertRef,
		&c.ClientCertPath, &c.ClientKeyPath,
		&c.RxBytesOffset, &c.TxBytesOffset,
		&c.RealAddress, &c.VirtualAddress, &c.ConnectedSince,
		&c.LastRxBytes, &c.LastTxBytes, &c.LastRxBps, &c.LastTxBps, &tags,
		&created, &updated, &c.InstanceName,
	)
	if err != nil {
		return Client{}, err
	}
	c.Suspended = suspended != 0
	c.PushRoutes = decodeJSONList(pushRoutes)
	c.Tags = decodeJSONList(tags)
	c.CreatedAt = parseTime(created)
	c.UpdatedAt = parseTime(updated)
	return c, nil
}

// CreateClient inserts a server client.
func (s *Store) CreateClient(ctx context.Context, instanceName string, c Client) (Client, error) {
	inst, err := s.GetInstance(ctx, instanceName)
	if err != nil {
		return Client{}, err
	}
	if inst.Role != "server" {
		return Client{}, fmt.Errorf("clients only apply to server instances")
	}
	c.CommonName = strings.TrimSpace(c.CommonName)
	if c.CommonName == "" {
		return Client{}, fmt.Errorf("common_name required")
	}
	now := nowRFC3339()
	res, err := s.db.ExecContext(ctx, `
INSERT INTO clients (
  instance_id, common_name, name, notes, static_ip, push_routes,
  suspended, traffic_limit_bytes, bandwidth_rx_bps, bandwidth_tx_bps, cert_ref,
  client_cert_path, client_key_path,
  rx_bytes_offset, tx_bytes_offset,
  real_address, virtual_address, connected_since,
  last_rx_bytes, last_tx_bytes, last_rx_bps, last_tx_bps, tags,
  created_at, updated_at
) VALUES (?,?,?,?,?,?, ?,?,?,?,?, ?,?, 0,0, '','','', 0,0,0,0, ?, ?,?)`,
		inst.ID, c.CommonName, c.Name, c.Notes, c.StaticIP, encodeJSONList(c.PushRoutes),
		boolToInt(c.Suspended), c.TrafficLimitBytes, c.BandwidthRxBps, c.BandwidthTxBps, c.CertRef,
		c.ClientCertPath, c.ClientKeyPath,
		encodeJSONList(c.Tags), now, now,
	)
	if err != nil {
		return Client{}, fmt.Errorf("insert client: %w", err)
	}
	id, _ := res.LastInsertId()
	out, err := s.GetClientByID(ctx, id)
	if err != nil {
		return Client{}, err
	}
	return *out, nil
}

// GetClient returns a client by instance name + CN.
func (s *Store) GetClient(ctx context.Context, instanceName, cn string) (*Client, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+clientColumns+`
FROM clients c JOIN instances i ON i.id = c.instance_id
WHERE i.name=? AND c.common_name=?`, instanceName, cn)
	c, err := scanClient(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("client %q not found on instance %q", cn, instanceName)
		}
		return nil, err
	}
	return &c, nil
}

// GetClientByID returns a client by id.
func (s *Store) GetClientByID(ctx context.Context, id int64) (*Client, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+clientColumns+`
FROM clients c JOIN instances i ON i.id = c.instance_id
WHERE c.id=?`, id)
	c, err := scanClient(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("client id %d not found", id)
		}
		return nil, err
	}
	return &c, nil
}

// ListClientsByInstance lists clients for an instance.
func (s *Store) ListClientsByInstance(ctx context.Context, instanceName string) ([]Client, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+clientColumns+`
FROM clients c JOIN instances i ON i.id = c.instance_id
WHERE i.name=? ORDER BY c.common_name`, instanceName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListAllClients returns all clients.
func (s *Store) ListAllClients(ctx context.Context) ([]Client, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+clientColumns+`
FROM clients c JOIN instances i ON i.id = c.instance_id
ORDER BY i.name, c.common_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateClient replaces mutable client fields.
func (s *Store) UpdateClient(ctx context.Context, instanceName, cn string, c Client) (Client, error) {
	existing, err := s.GetClient(ctx, instanceName, cn)
	if err != nil {
		return Client{}, err
	}
	now := nowRFC3339()
	_, err = s.db.ExecContext(ctx, `
UPDATE clients SET
  name=?, notes=?, static_ip=?, push_routes=?,
  suspended=?, traffic_limit_bytes=?, bandwidth_rx_bps=?, bandwidth_tx_bps=?,
  cert_ref=?, client_cert_path=?, client_key_path=?, tags=?, updated_at=?
WHERE id=?`,
		c.Name, c.Notes, c.StaticIP, encodeJSONList(c.PushRoutes),
		boolToInt(c.Suspended), c.TrafficLimitBytes, c.BandwidthRxBps, c.BandwidthTxBps,
		c.CertRef, c.ClientCertPath, c.ClientKeyPath, encodeJSONList(c.Tags), now, existing.ID,
	)
	if err != nil {
		return Client{}, err
	}
	out, err := s.GetClientByID(ctx, existing.ID)
	if err != nil {
		return Client{}, err
	}
	return *out, nil
}

// DeleteClient removes a client.
func (s *Store) DeleteClient(ctx context.Context, instanceName, cn string) error {
	existing, err := s.GetClient(ctx, instanceName, cn)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM clients WHERE id=?`, existing.ID)
	return err
}

// SetClientSuspended sets suspended flag.
func (s *Store) SetClientSuspended(ctx context.Context, id int64, suspended bool) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE clients SET suspended=?, updated_at=? WHERE id=?`,
		boolToInt(suspended), nowRFC3339(), id)
	return err
}

// UpdateClientRuntime stores observed connection stats.
func (s *Store) UpdateClientRuntime(ctx context.Context, id int64, realAddr, virtAddr, connectedSince string, rx, tx int64, rxBps, txBps float64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE clients SET
  real_address=?, virtual_address=?, connected_since=?,
  last_rx_bytes=?, last_tx_bytes=?, last_rx_bps=?, last_tx_bps=?,
  updated_at=?
WHERE id=?`,
		realAddr, virtAddr, connectedSince, rx, tx, rxBps, txBps, nowRFC3339(), id)
	return err
}

// ResetClientTraffic soft-resets counters via offsets.
func (s *Store) ResetClientTraffic(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE clients SET
  rx_bytes_offset = last_rx_bytes,
  tx_bytes_offset = last_tx_bytes,
  updated_at=?
WHERE id=?`, nowRFC3339(), id)
	return err
}

// ListUsedStaticIPs returns static IPs already assigned on an instance.
func (s *Store) ListUsedStaticIPs(ctx context.Context, instanceName string) ([]string, error) {
	clients, err := s.ListClientsByInstance(ctx, instanceName)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range clients {
		if c.StaticIP != "" {
			out = append(out, c.StaticIP)
		}
	}
	return out, nil
}
