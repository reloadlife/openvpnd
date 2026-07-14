package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CA is metadata for a managed certificate authority.
type CA struct {
	Name       string    `json:"name"`
	CommonName string    `json:"common_name"`
	Org        string    `json:"org,omitempty"`
	CertPath   string    `json:"cert_path"`
	KeyPath    string    `json:"key_path"`
	NotAfter   string    `json:"not_after,omitempty"`
	SerialNext int64     `json:"serial_next"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Certificate is a managed leaf cert record.
type Certificate struct {
	ID           int64     `json:"id"`
	CAName       string    `json:"ca_name"`
	Kind         string    `json:"kind"`
	CommonName   string    `json:"common_name"`
	CertPath     string    `json:"cert_path"`
	KeyPath      string    `json:"key_path"`
	NotBefore    string    `json:"not_before,omitempty"`
	NotAfter     string    `json:"not_after,omitempty"`
	Serial       int64     `json:"serial"`
	Fingerprint  string    `json:"fingerprint,omitempty"`
	Revoked      bool      `json:"revoked"`
	InstanceName string    `json:"instance_name,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// TLSCryptKey is a named OpenVPN static key.
type TLSCryptKey struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

// UpsertCA inserts or updates CA metadata.
func (s *Store) UpsertCA(ctx context.Context, ca CA) (CA, error) {
	if ca.Name == "" {
		return CA{}, fmt.Errorf("ca name required")
	}
	now := nowRFC3339()
	existing, err := s.GetCA(ctx, ca.Name)
	if err == nil && existing != nil {
		_, err = s.db.ExecContext(ctx, `
UPDATE cas SET common_name=?, org=?, cert_path=?, key_path=?, not_after=?, serial_next=?, updated_at=?
WHERE name=?`,
			ca.CommonName, ca.Org, ca.CertPath, ca.KeyPath, ca.NotAfter, ca.SerialNext, now, ca.Name)
		if err != nil {
			return CA{}, err
		}
		out, err := s.GetCA(ctx, ca.Name)
		if err != nil {
			return CA{}, err
		}
		return *out, nil
	}
	if ca.SerialNext <= 0 {
		ca.SerialNext = 2
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO cas (name, common_name, org, cert_path, key_path, not_after, serial_next, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?)`,
		ca.Name, ca.CommonName, ca.Org, ca.CertPath, ca.KeyPath, ca.NotAfter, ca.SerialNext, now, now)
	if err != nil {
		return CA{}, fmt.Errorf("insert ca: %w", err)
	}
	out, err := s.GetCA(ctx, ca.Name)
	if err != nil {
		return CA{}, err
	}
	return *out, nil
}

// GetCA returns a CA by name.
func (s *Store) GetCA(ctx context.Context, name string) (*CA, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT name, common_name, org, cert_path, key_path, not_after, serial_next, created_at, updated_at
FROM cas WHERE name=?`, name)
	var ca CA
	var created, updated string
	if err := row.Scan(&ca.Name, &ca.CommonName, &ca.Org, &ca.CertPath, &ca.KeyPath, &ca.NotAfter, &ca.SerialNext, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("CA %q not found", name)
		}
		return nil, err
	}
	ca.CreatedAt = parseTime(created)
	ca.UpdatedAt = parseTime(updated)
	return &ca, nil
}

// ListCAs returns all CAs.
func (s *Store) ListCAs(ctx context.Context) ([]CA, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, common_name, org, cert_path, key_path, not_after, serial_next, created_at, updated_at
FROM cas ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CA
	for rows.Next() {
		var ca CA
		var created, updated string
		if err := rows.Scan(&ca.Name, &ca.CommonName, &ca.Org, &ca.CertPath, &ca.KeyPath, &ca.NotAfter, &ca.SerialNext, &created, &updated); err != nil {
			return nil, err
		}
		ca.CreatedAt = parseTime(created)
		ca.UpdatedAt = parseTime(updated)
		out = append(out, ca)
	}
	return out, rows.Err()
}

// DeleteCA removes CA metadata (files left on disk).
func (s *Store) DeleteCA(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM cas WHERE name=?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("CA %q not found", name)
	}
	return nil
}

// UpsertCertificate inserts or updates a cert record.
func (s *Store) UpsertCertificate(ctx context.Context, c Certificate) (Certificate, error) {
	now := nowRFC3339()
	existing, err := s.GetCertificateByCN(ctx, c.CAName, c.Kind, c.CommonName)
	if err == nil && existing != nil {
		_, err = s.db.ExecContext(ctx, `
UPDATE certificates SET cert_path=?, key_path=?, not_before=?, not_after=?, serial=?, fingerprint=?,
  revoked=?, instance_name=?, notes=?
WHERE id=?`,
			c.CertPath, c.KeyPath, c.NotBefore, c.NotAfter, c.Serial, c.Fingerprint,
			boolToInt(c.Revoked), c.InstanceName, c.Notes, existing.ID)
		if err != nil {
			return Certificate{}, err
		}
		out, err := s.GetCertificate(ctx, existing.ID)
		if err != nil {
			return Certificate{}, err
		}
		return *out, nil
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO certificates (ca_name, kind, common_name, cert_path, key_path, not_before, not_after, serial, fingerprint, revoked, instance_name, notes, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.CAName, c.Kind, c.CommonName, c.CertPath, c.KeyPath, c.NotBefore, c.NotAfter, c.Serial, c.Fingerprint,
		boolToInt(c.Revoked), c.InstanceName, c.Notes, now)
	if err != nil {
		return Certificate{}, fmt.Errorf("insert cert: %w", err)
	}
	id, _ := res.LastInsertId()
	out, err := s.GetCertificate(ctx, id)
	if err != nil {
		return Certificate{}, err
	}
	return *out, nil
}

// GetCertificate by id.
func (s *Store) GetCertificate(ctx context.Context, id int64) (*Certificate, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, ca_name, kind, common_name, cert_path, key_path, not_before, not_after, serial, fingerprint, revoked, instance_name, notes, created_at
FROM certificates WHERE id=?`, id)
	return scanCert(row)
}

// GetCertificateByCN looks up by CA+kind+CN.
func (s *Store) GetCertificateByCN(ctx context.Context, ca, kind, cn string) (*Certificate, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, ca_name, kind, common_name, cert_path, key_path, not_before, not_after, serial, fingerprint, revoked, instance_name, notes, created_at
FROM certificates WHERE ca_name=? AND kind=? AND common_name=?`, ca, kind, cn)
	c, err := scanCert(row)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func scanCert(scanner interface {
	Scan(dest ...any) error
}) (*Certificate, error) {
	var c Certificate
	var revoked int
	var created string
	if err := scanner.Scan(&c.ID, &c.CAName, &c.Kind, &c.CommonName, &c.CertPath, &c.KeyPath,
		&c.NotBefore, &c.NotAfter, &c.Serial, &c.Fingerprint, &revoked, &c.InstanceName, &c.Notes, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("certificate not found")
		}
		return nil, err
	}
	c.Revoked = revoked != 0
	c.CreatedAt = parseTime(created)
	return &c, nil
}

// ListCertificates lists certs, optional ca filter.
func (s *Store) ListCertificates(ctx context.Context, caName string) ([]Certificate, error) {
	var rows *sql.Rows
	var err error
	if caName == "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, ca_name, kind, common_name, cert_path, key_path, not_before, not_after, serial, fingerprint, revoked, instance_name, notes, created_at
FROM certificates ORDER BY ca_name, kind, common_name`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, ca_name, kind, common_name, cert_path, key_path, not_before, not_after, serial, fingerprint, revoked, instance_name, notes, created_at
FROM certificates WHERE ca_name=? ORDER BY kind, common_name`, caName)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Certificate
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// UpsertTLSCrypt stores a tls-crypt key record.
func (s *Store) UpsertTLSCrypt(ctx context.Context, name, path string) (TLSCryptKey, error) {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tls_crypt_keys (name, path, created_at) VALUES (?,?,?)
ON CONFLICT(name) DO UPDATE SET path=excluded.path`, name, path, now)
	if err != nil {
		return TLSCryptKey{}, err
	}
	return s.GetTLSCrypt(ctx, name)
}

// GetTLSCrypt returns a named key.
func (s *Store) GetTLSCrypt(ctx context.Context, name string) (TLSCryptKey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT name, path, created_at FROM tls_crypt_keys WHERE name=?`, name)
	var k TLSCryptKey
	var created string
	if err := row.Scan(&k.Name, &k.Path, &created); err != nil {
		if err == sql.ErrNoRows {
			return TLSCryptKey{}, fmt.Errorf("tls-crypt %q not found", name)
		}
		return TLSCryptKey{}, err
	}
	k.CreatedAt = parseTime(created)
	return k, nil
}

// ListTLSCrypt lists keys.
func (s *Store) ListTLSCrypt(ctx context.Context) ([]TLSCryptKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, path, created_at FROM tls_crypt_keys ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TLSCryptKey
	for rows.Next() {
		var k TLSCryptKey
		var created string
		if err := rows.Scan(&k.Name, &k.Path, &created); err != nil {
			return nil, err
		}
		k.CreatedAt = parseTime(created)
		out = append(out, k)
	}
	return out, rows.Err()
}
