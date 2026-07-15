package pki_test

import (
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/pki"
)

func TestCreateCAAndIssue(t *testing.T) {
	dir := t.TempDir()
	m, err := pki.NewManager(dir)
	require.NoError(t, err)

	ca, err := m.CreateCA(pki.CreateCAOptions{
		Name: "main", CommonName: "OpenVPNd Test CA", Org: "test", ValidDays: 365,
	})
	require.NoError(t, err)
	require.FileExists(t, ca.CertPath)
	require.FileExists(t, ca.KeyPath)

	// duplicate fails
	_, err = m.CreateCA(pki.CreateCAOptions{Name: "main", CommonName: "x"})
	require.Error(t, err)

	srv, err := m.Issue(pki.IssueOptions{
		CAName: "main", Kind: pki.KindServer, CommonName: "vpn.example.com",
		DNSNames: []string{"vpn.example.com"}, ValidDays: 90,
	})
	require.NoError(t, err)
	require.FileExists(t, srv.CertPath)
	require.Equal(t, pki.KindServer, srv.Kind)
	require.NotEmpty(t, srv.Fingerprint)

	cli, err := m.Issue(pki.IssueOptions{
		CAName: "main", Kind: pki.KindClient, CommonName: "alice", ValidDays: 90,
	})
	require.NoError(t, err)
	require.FileExists(t, cli.KeyPath)

	// verify chain
	caPEM, _ := os.ReadFile(ca.CertPath)
	cliPEM, _ := os.ReadFile(cli.CertPath)
	block, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	block, _ = pem.Decode(cliPEM)
	cliCert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	_, err = cliCert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	require.NoError(t, err)

	tc, err := m.GenerateTLSCrypt("default")
	require.NoError(t, err)
	require.FileExists(t, tc)
	b, _ := os.ReadFile(tc)
	require.Contains(t, string(b), "BEGIN OpenVPN Static key")

	names, err := m.ListCANames()
	require.NoError(t, err)
	require.Contains(t, names, "main")

	// paths
	cp, kp, err := m.CAPaths("main")
	require.NoError(t, err)
	require.Equal(t, ca.CertPath, cp)
	require.Equal(t, ca.KeyPath, kp)

	// re-issue overwrites
	cli2, err := m.Issue(pki.IssueOptions{CAName: "main", Kind: pki.KindClient, CommonName: "alice"})
	require.NoError(t, err)
	require.Equal(t, filepath.Dir(cli.CertPath), filepath.Dir(cli2.CertPath))
}

func TestRebuildCRL(t *testing.T) {
	dir := t.TempDir()
	m, err := pki.NewManager(dir)
	require.NoError(t, err)

	_, err = m.CreateCA(pki.CreateCAOptions{Name: "main", CommonName: "CRL CA", ValidDays: 365})
	require.NoError(t, err)

	cli, err := m.Issue(pki.IssueOptions{CAName: "main", Kind: pki.KindClient, CommonName: "bob", ValidDays: 30})
	require.NoError(t, err)

	path, num, err := m.RebuildCRL("main", []pki.RevokedEntry{
		{Serial: big.NewInt(cli.Serial), RevokedAt: time.Now().UTC(), Reason: "keyCompromise"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), num)
	require.FileExists(t, path)
	require.Equal(t, m.CRLPath("main"), path)

	pemBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(pemBytes), "X509 CRL")
	block, _ := pem.Decode(pemBytes)
	require.NotNil(t, block)
	rl, err := x509.ParseRevocationList(block.Bytes)
	require.NoError(t, err)
	require.Len(t, rl.RevokedCertificateEntries, 1)
	require.Equal(t, 0, big.NewInt(cli.Serial).Cmp(rl.RevokedCertificateEntries[0].SerialNumber))

	// second rebuild increments number
	path2, num2, err := m.RebuildCRL("main", nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), num2)
	require.Equal(t, path, path2)
	pemBytes, _ = os.ReadFile(path2)
	block, _ = pem.Decode(pemBytes)
	rl, err = x509.ParseRevocationList(block.Bytes)
	require.NoError(t, err)
	require.Empty(t, rl.RevokedCertificateEntries)
}
