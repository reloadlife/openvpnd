package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"
)

// ProfileToken is a time-limited (optionally single-use) download token for a client .ovpn.
type ProfileToken struct {
	ID         int64     `json:"id"`
	Token      string    `json:"token"`
	InstanceID int64     `json:"instance_id"`
	ClientID   int64     `json:"client_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	MaxUses    int       `json:"max_uses"`
	UseCount   int       `json:"use_count"`
	Revoked    bool      `json:"revoked"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	// Joined
	InstanceName string `json:"instance_name,omitempty"`
	CommonName   string `json:"common_name,omitempty"`
}

// GenerateProfileToken creates a cryptographically random URL-safe token string.
func GenerateProfileToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateProfileToken inserts a new share token.
func (s *Store) CreateProfileToken(ctx context.Context, clientID int64, instanceID int64, ttl time.Duration, maxUses int, note string) (ProfileToken, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if maxUses < 0 {
		maxUses = 0
	}
	tok, err := GenerateProfileToken()
	if err != nil {
		return ProfileToken{}, err
	}
	now := time.Now().UTC()
	exp := now.Add(ttl)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO profile_tokens (token, instance_id, client_id, expires_at, max_uses, use_count, revoked, note, created_at)
VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?)`,
		tok, instanceID, clientID, exp.Format(time.RFC3339Nano), maxUses, note, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return ProfileToken{}, fmt.Errorf("insert profile token: %w", err)
	}
	return s.GetProfileToken(ctx, tok)
}

// GetProfileToken loads a token by value.
func (s *Store) GetProfileToken(ctx context.Context, token string) (ProfileToken, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT t.id, t.token, t.instance_id, t.client_id, t.expires_at, t.max_uses, t.use_count, t.revoked, t.note, t.created_at,
       i.name, c.common_name
FROM profile_tokens t
JOIN instances i ON i.id = t.instance_id
JOIN clients c ON c.id = t.client_id
WHERE t.token=?`, token)
	var pt ProfileToken
	var exp, created string
	var revoked int
	if err := row.Scan(
		&pt.ID, &pt.Token, &pt.InstanceID, &pt.ClientID, &exp, &pt.MaxUses, &pt.UseCount, &revoked, &pt.Note, &created,
		&pt.InstanceName, &pt.CommonName,
	); err != nil {
		if err == sql.ErrNoRows {
			return ProfileToken{}, fmt.Errorf("token not found")
		}
		return ProfileToken{}, err
	}
	pt.Revoked = revoked != 0
	pt.ExpiresAt = parseTime(exp)
	pt.CreatedAt = parseTime(created)
	return pt, nil
}

// ConsumeProfileToken validates and increments use_count. Returns error if invalid.
func (s *Store) ConsumeProfileToken(ctx context.Context, token string) (ProfileToken, error) {
	pt, err := s.GetProfileToken(ctx, token)
	if err != nil {
		return ProfileToken{}, err
	}
	if pt.Revoked {
		return ProfileToken{}, fmt.Errorf("token revoked")
	}
	if !pt.ExpiresAt.IsZero() && time.Now().UTC().After(pt.ExpiresAt) {
		return ProfileToken{}, fmt.Errorf("token expired")
	}
	if pt.MaxUses > 0 && pt.UseCount >= pt.MaxUses {
		return ProfileToken{}, fmt.Errorf("token use limit reached")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE profile_tokens SET use_count = use_count + 1 WHERE id=?`, pt.ID)
	if err != nil {
		return ProfileToken{}, err
	}
	pt.UseCount++
	return pt, nil
}

// RevokeProfileToken marks a token revoked.
func (s *Store) RevokeProfileToken(ctx context.Context, token string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE profile_tokens SET revoked=1 WHERE token=?`, token)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// ListProfileTokensByClient lists tokens for a client.
func (s *Store) ListProfileTokensByClient(ctx context.Context, clientID int64) ([]ProfileToken, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.token, t.instance_id, t.client_id, t.expires_at, t.max_uses, t.use_count, t.revoked, t.note, t.created_at,
       i.name, c.common_name
FROM profile_tokens t
JOIN instances i ON i.id = t.instance_id
JOIN clients c ON c.id = t.client_id
WHERE t.client_id=? ORDER BY t.id DESC`, clientID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ProfileToken
	for rows.Next() {
		var pt ProfileToken
		var exp, created string
		var revoked int
		if err := rows.Scan(
			&pt.ID, &pt.Token, &pt.InstanceID, &pt.ClientID, &exp, &pt.MaxUses, &pt.UseCount, &revoked, &pt.Note, &created,
			&pt.InstanceName, &pt.CommonName,
		); err != nil {
			return nil, err
		}
		pt.Revoked = revoked != 0
		pt.ExpiresAt = parseTime(exp)
		pt.CreatedAt = parseTime(created)
		out = append(out, pt)
	}
	return out, rows.Err()
}
