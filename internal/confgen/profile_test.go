package confgen_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
)

func TestRenderClientProfileInline(t *testing.T) {
	inst := db.Instance{
		Name:            "ovpn0",
		Role:            "server",
		Proto:           "udp",
		Port:            1194,
		PublicEndpoint:  "vpn.example.com:1194",
		DevType:         "tun",
		PKICaPath:       "/pki/ca.crt",
		PKITLSCryptPath: "/pki/tc.key",
	}
	cli := db.Client{
		CommonName:     "alice",
		ClientCertPath: "/pki/alice.crt",
		ClientKeyPath:  "/pki/alice.key",
	}
	mat := confgen.ProfileMaterial{
		CA:       "-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----",
		Cert:     "-----BEGIN CERTIFICATE-----\nCERT\n-----END CERTIFICATE-----",
		Key:      "-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----",
		TLSCrypt: "-----BEGIN OpenVPN Static key V1-----\nTC\n-----END OpenVPN Static key V1-----",
	}
	out, err := confgen.RenderClientProfile(inst, cli, mat, confgen.ProfileOptions{Inline: true})
	require.NoError(t, err)
	require.Contains(t, out, "client")
	require.Contains(t, out, "remote vpn.example.com 1194")
	require.Contains(t, out, "<ca>")
	require.Contains(t, out, "<cert>")
	require.Contains(t, out, "<key>")
	require.Contains(t, out, "<tls-crypt>")
	require.Contains(t, out, "explicit-exit-notify 1")
	require.Contains(t, out, "auth-nocache")
	require.True(t, strings.Contains(out, "alice") || strings.Contains(out, "cn=alice"))
}

func TestRenderClientProfileRequiresEndpoint(t *testing.T) {
	_, err := confgen.RenderClientProfile(db.Instance{}, db.Client{ClientCertPath: "c", ClientKeyPath: "k"}, confgen.ProfileMaterial{
		CA: "ca", Cert: "c", Key: "k",
	}, confgen.ProfileOptions{Inline: true})
	require.Error(t, err)
}
