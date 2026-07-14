// Package pki provides managed CA and certificate issuance for OpenVPN mTLS.
package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Kind of issued certificate.
const (
	KindCA     = "ca"
	KindServer = "server"
	KindClient = "client"
)

// Manager writes PEMs under a root directory (mode 0700).
type Manager struct {
	Root string
}

// NewManager ensures the PKI root exists.
func NewManager(root string) (*Manager, error) {
	if root == "" {
		return nil, fmt.Errorf("pki root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(root, 0o700)
	return &Manager{Root: root}, nil
}

// CreateCAOptions for a new CA.
type CreateCAOptions struct {
	Name       string // filesystem-safe name (default from CN)
	CommonName string
	Org        string
	ValidDays  int // default 3650
	// KeyType: "ec" (P-256, default) or "rsa"
	KeyType string
	RSABits int // default 2048 when rsa
}

// CAMaterial is on-disk CA paths.
type CAMaterial struct {
	Name     string
	CN       string
	CertPath string
	KeyPath  string
	CertPEM  string
	NotAfter time.Time
	Serial   int64 // next serial to use after CA (CA used 1)
}

// IssuedCert is a signed leaf cert.
type IssuedCert struct {
	Kind        string
	CommonName  string
	CertPath    string
	KeyPath     string
	CertPEM     string
	NotBefore   time.Time
	NotAfter    time.Time
	Serial      int64
	Fingerprint string // SHA1 hex of DER
}

// CreateCA generates a self-signed CA and writes ca.crt / ca.key.
func (m *Manager) CreateCA(opts CreateCAOptions) (CAMaterial, error) {
	cn := strings.TrimSpace(opts.CommonName)
	if cn == "" {
		return CAMaterial{}, fmt.Errorf("common_name required")
	}
	name := sanitizeName(opts.Name)
	if name == "" {
		name = sanitizeName(cn)
	}
	if name == "" {
		name = "default"
	}
	days := opts.ValidDays
	if days <= 0 {
		days = 3650
	}

	dir := filepath.Join(m.Root, "cas", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CAMaterial{}, err
	}
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if fileExists(certPath) || fileExists(keyPath) {
		return CAMaterial{}, fmt.Errorf("CA %q already exists", name)
	}

	priv, pub, err := generateKey(opts.KeyType, opts.RSABits)
	if err != nil {
		return CAMaterial{}, err
	}

	serial := big.NewInt(1)
	now := time.Now().UTC().Add(-1 * time.Minute)
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: nonEmptySlice(opts.Org),
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(0, 0, days),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
		MaxPathLenZero:        false,
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		return CAMaterial{}, fmt.Errorf("create CA cert: %w", err)
	}
	certPEM, err := encodeCertPEM(der)
	if err != nil {
		return CAMaterial{}, err
	}
	keyPEM, err := encodeKeyPEM(priv)
	if err != nil {
		return CAMaterial{}, err
	}
	if err := writeFile0600(certPath, certPEM); err != nil {
		return CAMaterial{}, err
	}
	if err := writeFile0600(keyPath, keyPEM); err != nil {
		return CAMaterial{}, err
	}
	// serial counter file
	if err := writeFile0600(filepath.Join(dir, "serial"), []byte("2\n")); err != nil {
		return CAMaterial{}, err
	}

	return CAMaterial{
		Name:     name,
		CN:       cn,
		CertPath: certPath,
		KeyPath:  keyPath,
		CertPEM:  string(certPEM),
		NotAfter: tpl.NotAfter,
		Serial:   2,
	}, nil
}

// IssueOptions for a leaf certificate.
type IssueOptions struct {
	CAName     string
	Kind       string // server | client
	CommonName string
	ValidDays  int // default 825
	// DNSNames / IPs for server SAN
	DNSNames []string
	IPs      []string
	// Serial: 0 = auto from CA serial file
	Serial int64
	// KeyType for leaf (default ec)
	KeyType string
	RSABits int
}

// Issue signs a leaf certificate under an existing CA.
func (m *Manager) Issue(opts IssueOptions) (IssuedCert, error) {
	kind := strings.ToLower(strings.TrimSpace(opts.Kind))
	if kind != KindServer && kind != KindClient {
		return IssuedCert{}, fmt.Errorf("kind must be server or client")
	}
	cn := strings.TrimSpace(opts.CommonName)
	if cn == "" {
		return IssuedCert{}, fmt.Errorf("common_name required")
	}
	caName := sanitizeName(opts.CAName)
	if caName == "" {
		return IssuedCert{}, fmt.Errorf("ca_name required")
	}
	days := opts.ValidDays
	if days <= 0 {
		days = 825
	}

	caCert, caKey, err := m.loadCA(caName)
	if err != nil {
		return IssuedCert{}, err
	}

	serial := opts.Serial
	if serial <= 0 {
		serial, err = m.nextSerial(caName)
		if err != nil {
			return IssuedCert{}, err
		}
	}

	priv, pub, err := generateKey(opts.KeyType, opts.RSABits)
	if err != nil {
		return IssuedCert{}, err
	}

	now := time.Now().UTC().Add(-1 * time.Minute)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(0, 0, days),
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              opts.DNSNames,
	}
	for _, ip := range opts.IPs {
		if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, parsed)
		}
	}
	// For servers without explicit SAN, use CN as DNS if it looks like a name
	if kind == KindServer && len(tpl.DNSNames) == 0 && len(tpl.IPAddresses) == 0 {
		if net.ParseIP(cn) != nil {
			tpl.IPAddresses = []net.IP{net.ParseIP(cn)}
		} else {
			tpl.DNSNames = []string{cn}
		}
	}

	switch kind {
	case KindServer:
		tpl.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	case KindClient:
		tpl.KeyUsage = x509.KeyUsageDigitalSignature
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, pub, caKey)
	if err != nil {
		return IssuedCert{}, fmt.Errorf("sign cert: %w", err)
	}
	certPEM, err := encodeCertPEM(der)
	if err != nil {
		return IssuedCert{}, err
	}
	keyPEM, err := encodeKeyPEM(priv)
	if err != nil {
		return IssuedCert{}, err
	}

	safeCN := sanitizeName(cn)
	dir := filepath.Join(m.Root, "certs", caName, kind)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return IssuedCert{}, err
	}
	certPath := filepath.Join(dir, safeCN+".crt")
	keyPath := filepath.Join(dir, safeCN+".key")
	// allow re-issue by overwriting
	if err := writeFile0600(certPath, certPEM); err != nil {
		return IssuedCert{}, err
	}
	if err := writeFile0600(keyPath, keyPEM); err != nil {
		return IssuedCert{}, err
	}

	sum := sha1.Sum(der)
	return IssuedCert{
		Kind:        kind,
		CommonName:  cn,
		CertPath:    certPath,
		KeyPath:     keyPath,
		CertPEM:     string(certPEM),
		NotBefore:   tpl.NotBefore,
		NotAfter:    tpl.NotAfter,
		Serial:      serial,
		Fingerprint: hex.EncodeToString(sum[:]),
	}, nil
}

// GenerateTLSCrypt writes an OpenVPN static key (tls-crypt compatible).
func (m *Manager) GenerateTLSCrypt(name string) (path string, err error) {
	name = sanitizeName(name)
	if name == "" {
		name = "default"
	}
	dir := filepath.Join(m.Root, "tls-crypt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path = filepath.Join(dir, name+".key")
	// OpenVPN static key: 2048 bits = 256 bytes hex lines
	raw := make([]byte, 256)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("-----BEGIN OpenVPN Static key V1-----\n")
	for i := 0; i < len(raw); i += 16 {
		end := i + 16
		if end > len(raw) {
			end = len(raw)
		}
		b.WriteString(hex.EncodeToString(raw[i:end]))
		b.WriteByte('\n')
	}
	b.WriteString("-----END OpenVPN Static key V1-----\n")
	if err := writeFile0600(path, []byte(b.String())); err != nil {
		return "", err
	}
	return path, nil
}

// CAPaths returns cert/key paths for a named CA.
func (m *Manager) CAPaths(name string) (certPath, keyPath string, err error) {
	name = sanitizeName(name)
	certPath = filepath.Join(m.Root, "cas", name, "ca.crt")
	keyPath = filepath.Join(m.Root, "cas", name, "ca.key")
	if !fileExists(certPath) || !fileExists(keyPath) {
		return "", "", fmt.Errorf("CA %q not found", name)
	}
	return certPath, keyPath, nil
}

// ListCANames returns CA directory names.
func (m *Manager) ListCANames() ([]string, error) {
	dir := filepath.Join(m.Root, "cas")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && fileExists(filepath.Join(dir, e.Name(), "ca.crt")) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func (m *Manager) loadCA(name string) (*x509.Certificate, crypto.Signer, error) {
	certPath, keyPath, err := m.CAPaths(name)
	if err != nil {
		return nil, nil, err
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA cert: %w", err)
	}
	key, err := parseKeyPEM(keyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}
	return cert, key, nil
}

func (m *Manager) nextSerial(caName string) (int64, error) {
	path := filepath.Join(m.Root, "cas", caName, "serial")
	b, err := os.ReadFile(path)
	if err != nil {
		// start at 2
		_ = writeFile0600(path, []byte("3\n"))
		return 2, nil
	}
	var n int64
	_, _ = fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &n)
	if n < 2 {
		n = 2
	}
	if err := writeFile0600(path, []byte(fmt.Sprintf("%d\n", n+1))); err != nil {
		return 0, err
	}
	return n, nil
}

func generateKey(keyType string, rsaBits int) (crypto.Signer, crypto.PublicKey, error) {
	kt := strings.ToLower(strings.TrimSpace(keyType))
	if kt == "" || kt == "ec" || kt == "ecdsa" {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		return k, &k.PublicKey, nil
	}
	if kt == "rsa" {
		if rsaBits < 2048 {
			rsaBits = 2048
		}
		k, err := rsa.GenerateKey(rand.Reader, rsaBits)
		if err != nil {
			return nil, nil, err
		}
		return k, &k.PublicKey, nil
	}
	return nil, nil, fmt.Errorf("key_type must be ec or rsa")
}

func encodeCertPEM(der []byte) ([]byte, error) {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func encodeKeyPEM(key crypto.Signer) ([]byte, error) {
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		b, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), nil
	case *rsa.PrivateKey:
		b := x509.MarshalPKCS1PrivateKey(k)
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: b}), nil
	default:
		// PKCS#8 fallback
		b, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}), nil
	}
}

func parseCertPEM(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKeyPEM(pemBytes []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		s, ok := k.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("not a signer key")
		}
		return s, nil
	default:
		// try PKCS8
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("unsupported key type %q", block.Type)
		}
		s, ok := k.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("not a signer key")
		}
		return s, nil
	}
}

func writeFile0600(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	s = strings.ReplaceAll(s, " ", "_")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func nonEmptySlice(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return []string{s}
}
