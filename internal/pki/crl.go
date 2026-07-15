package pki

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RevokedEntry is one certificate on a CRL.
type RevokedEntry struct {
	Serial    *big.Int
	RevokedAt time.Time
	Reason    string
}

// RebuildCRL builds and writes a PEM-encoded CRL for the named CA.
// crlNumber is the CRL number to embed; if <= 0 it is read from cas/<name>/crl_number
// (default 1), used, then the file is incremented for the next rebuild.
// Returns the written path and the CRL number that was embedded.
func (m *Manager) RebuildCRL(caName string, revoked []RevokedEntry) (crlPath string, crlNumber int64, err error) {
	caName = sanitizeName(caName)
	if caName == "" {
		return "", 0, fmt.Errorf("ca_name required")
	}
	caCert, caKey, err := m.loadCA(caName)
	if err != nil {
		return "", 0, err
	}

	dir := filepath.Join(m.Root, "cas", caName)
	numPath := filepath.Join(dir, "crl_number")
	n, err := readCRLNumber(numPath)
	if err != nil {
		return "", 0, err
	}
	crlNumber = n

	now := time.Now().UTC()
	entries := make([]x509.RevocationListEntry, 0, len(revoked))
	for _, e := range revoked {
		if e.Serial == nil || e.Serial.Sign() <= 0 {
			continue
		}
		revAt := e.RevokedAt
		if revAt.IsZero() {
			revAt = now
		}
		entries = append(entries, x509.RevocationListEntry{
			SerialNumber:   new(big.Int).Set(e.Serial),
			RevocationTime: revAt.UTC(),
			ReasonCode:     reasonCode(e.Reason),
		})
	}

	tpl := &x509.RevocationList{
		Number:                    big.NewInt(crlNumber),
		ThisUpdate:                now,
		NextUpdate:                now.Add(7 * 24 * time.Hour),
		RevokedCertificateEntries: entries,
	}
	der, err := x509.CreateRevocationList(rand.Reader, tpl, caCert, caKey)
	if err != nil {
		return "", 0, fmt.Errorf("create CRL: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der})
	crlPath = filepath.Join(dir, "ca.crl")
	if err := writeFile0600(crlPath, pemBytes); err != nil {
		return "", 0, err
	}
	// bump counter for next rebuild
	if err := writeFile0600(numPath, []byte(fmt.Sprintf("%d\n", crlNumber+1))); err != nil {
		return "", 0, err
	}
	return crlPath, crlNumber, nil
}

// CRLPath returns the on-disk CRL path for a CA (may not exist yet).
func (m *Manager) CRLPath(caName string) string {
	caName = sanitizeName(caName)
	return filepath.Join(m.Root, "cas", caName, "ca.crl")
}

func readCRLNumber(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	var n int64
	_, _ = fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &n)
	if n < 1 {
		n = 1
	}
	return n, nil
}

// reasonCode maps a free-form reason string to RFC 5280 CRL reason codes.
// Unknown / empty → 0 (omit extension).
func reasonCode(reason string) int {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", "unspecified":
		return 0
	case "keycompromise", "key_compromise", "key compromise":
		return 1 // CRLReasonKeyCompromise
	case "cacompromise", "ca_compromise", "ca compromise":
		return 2
	case "affiliationchanged", "affiliation_changed", "affiliation changed":
		return 3
	case "superseded":
		return 4
	case "cessationofoperation", "cessation_of_operation", "cessation of operation":
		return 5
	case "certificatehold", "certificate_hold", "hold":
		return 6
	case "removefromcrl", "remove_from_crl":
		return 8
	case "privilegewithdrawn", "privilege_withdrawn":
		return 9
	case "aacompromise", "aa_compromise":
		return 10
	default:
		return 0
	}
}
