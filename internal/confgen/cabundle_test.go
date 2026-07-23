package confgen_test

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

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
)

// selfSignedCAPEM returns a self-signed CA certificate PEM for testing.
func selfSignedCAPEM(t *testing.T, cn string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func baseServer() db.Instance {
	return db.Instance{
		Name: "ovpn0", Role: "server", DevType: "tun", Proto: "udp", Port: 1194,
		ServerNetwork: "10.8.0.0/24", Topology: "subnet", AuthMode: "pki",
		PKICaPath:   "/etc/openvpn/cas/main/ca.crt",
		PKICertPath: "/pki/server.crt", PKIKeyPath: "/pki/server.key",
	}
}

// (a) An instance with an extra client CA renders a `ca` file (inline bundle)
// containing BOTH the instance CA cert and the extra CA cert.
func TestRenderInstanceExtraClientCABundle(t *testing.T) {
	instCA := selfSignedCAPEM(t, "instance-ca")
	fleetCA := selfSignedCAPEM(t, "fleet-ca")

	inst := baseServer()
	inst.ExtraClientCAPems = []string{fleetCA}

	paths := confgen.Paths{ConfDir: "/etc/openvpnd/instances", RuntimeDir: "/run/openvpnd", Name: "ovpn0"}
	res, err := confgen.RenderInstanceOpts(inst, paths, nil, confgen.RenderOptions{InstanceCAPEM: instCA})
	require.NoError(t, err)

	require.Contains(t, res.Content, "<ca>")
	require.Contains(t, res.Content, "</ca>")
	require.Contains(t, res.Content, strings.TrimSpace(instCA), "bundle must contain the instance CA cert")
	require.Contains(t, res.Content, strings.TrimSpace(fleetCA), "bundle must contain the extra client CA cert")
	// Inline bundle replaces the `ca <path>` directive.
	require.NotContains(t, res.Content, "ca /etc/openvpn/cas/main/ca.crt")
}

// (b) Empty extra_client_ca_pems renders byte-identically to before: providing an
// InstanceCAPEM has no effect, and the `ca <path>` directive is unchanged.
func TestRenderInstanceNoExtraClientCAByteIdentical(t *testing.T) {
	instCA := selfSignedCAPEM(t, "instance-ca")
	inst := baseServer() // no ExtraClientCAPems

	paths := confgen.Paths{ConfDir: "/etc/openvpnd/instances", RuntimeDir: "/run/openvpnd", Name: "ovpn0"}

	base, err := confgen.RenderInstance(inst, paths, nil)
	require.NoError(t, err)
	withCA, err := confgen.RenderInstanceOpts(inst, paths, nil, confgen.RenderOptions{InstanceCAPEM: instCA})
	require.NoError(t, err)

	require.Equal(t, base.Content, withCA.Content, "empty extras must render byte-identically regardless of InstanceCAPEM")
	require.Equal(t, base.Hash, withCA.Hash)
	require.Contains(t, base.Content, "ca /etc/openvpn/cas/main/ca.crt")
	require.NotContains(t, base.Content, "<ca>")
}
