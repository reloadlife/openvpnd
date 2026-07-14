package pki_test

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

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
