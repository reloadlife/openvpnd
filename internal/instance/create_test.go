package instance_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/instance"
)

func genSelfSignedCA(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
}

func TestPrepareServerAuto(t *testing.T) {
	res, err := instance.Prepare(instance.CreateInput{
		Role: "server",
		// name, network, port empty → auto
	}, instance.Context{
		ExistingNames: map[string]struct{}{"ovpn0": {}},
		UsedPorts:     map[int]struct{}{1194: {}},
		UsedNetworks:  []string{"10.8.0.0/24"},
		DefaultBinary: "default",
		BinaryNames:   map[string]struct{}{"default": {}},
		HasCA:         true,
		DefaultCA:     "main",
	})
	require.NoError(t, err)
	require.Equal(t, "ovpn1", res.Instance.Name)
	require.Equal(t, 1195, res.Instance.Port)
	require.Equal(t, "10.9.0.0/24", res.Instance.ServerNetwork)
	require.Equal(t, "subnet", res.Instance.Topology)
	require.True(t, res.IssueServerCert)
	require.True(t, res.GenerateTLSCrypt)
	require.Equal(t, "main", res.CAName)
	require.NotEmpty(t, res.Auto)
}

func TestPrepareClientRequiresRemote(t *testing.T) {
	_, err := instance.Prepare(instance.CreateInput{
		Name: "home", Role: "client",
	}, instance.Context{BinaryNames: map[string]struct{}{"default": {}}, DefaultBinary: "default"})
	require.Error(t, err)

	res, err := instance.Prepare(instance.CreateInput{
		Name: "home", Role: "client",
		Remotes: []db.Remote{{Host: "vpn.example.com"}},
	}, instance.Context{BinaryNames: map[string]struct{}{"default": {}}, DefaultBinary: "default"})
	require.NoError(t, err)
	require.Equal(t, 1194, res.Instance.Remotes[0].Port)
}

func TestPrepareRejectsBadName(t *testing.T) {
	_, err := instance.Prepare(instance.CreateInput{
		Name: "1bad", Role: "server", ServerNetwork: "10.8.0.0/24",
	}, instance.Context{BinaryNames: map[string]struct{}{"default": {}}, DefaultBinary: "default", HasCA: true})
	require.Error(t, err)
}

func TestPrepareOverlap(t *testing.T) {
	_, err := instance.Prepare(instance.CreateInput{
		Name: "x", Role: "server", ServerNetwork: "10.8.0.0/24",
	}, instance.Context{
		UsedNetworks:  []string{"10.8.0.0/24"},
		BinaryNames:   map[string]struct{}{"default": {}},
		DefaultBinary: "default",
		HasCA:         true,
	})
	require.Error(t, err)
}

func TestValidateClientCAPEMs(t *testing.T) {
	// A real (self-signed) CA cert passes; the trimmed PEM is returned.
	certPEM := `-----BEGIN CERTIFICATE-----
MIIBfzCCASWgAwIBAgIUE9Yg2fqExample0000000000000wCgYIKoZIzj0EAwIw
-----END CERTIFICATE-----`
	// Use a genuinely parseable cert instead of the placeholder above.
	real := genSelfSignedCA(t)
	out, err := instance.ValidateClientCAPEMs([]string{"  " + real + "  \n"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, real, out[0])

	// Empty / nil is valid.
	out, err = instance.ValidateClientCAPEMs(nil)
	require.NoError(t, err)
	require.Empty(t, out)

	// A private key is rejected (trust material only).
	_, err = instance.ValidateClientCAPEMs([]string{"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----"})
	require.Error(t, err)

	// Non-PEM garbage is rejected.
	_, err = instance.ValidateClientCAPEMs([]string{"not a pem"})
	require.Error(t, err)

	// A syntactically valid PEM CERTIFICATE block with non-cert bytes is rejected.
	_, err = instance.ValidateClientCAPEMs([]string{certPEM})
	require.Error(t, err)
}

func TestParseRemoteCSV(t *testing.T) {
	r, err := instance.ParseRemoteCSV("vpn.example.com:1194, backup.example.com:443:tcp")
	require.NoError(t, err)
	require.Len(t, r, 2)
	require.Equal(t, 1194, r[0].Port)
}
