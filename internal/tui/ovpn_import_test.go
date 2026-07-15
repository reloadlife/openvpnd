package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseClientProfileInline(t *testing.T) {
	content := `
client
dev tun
proto udp
remote vpn.example.com 1194
remote backup.example.com 1194 udp
cipher AES-256-GCM
auth SHA256
<ca>
-----BEGIN CERTIFICATE-----
CA
-----END CERTIFICATE-----
</ca>
<cert>
-----BEGIN CERTIFICATE-----
CERT
-----END CERTIFICATE-----
</cert>
<key>
-----BEGIN PRIVATE KEY-----
KEY
-----END PRIVATE KEY-----
</key>
<tls-crypt>
-----BEGIN OpenVPN Static key V1-----
TC
-----END OpenVPN Static key V1-----
</tls-crypt>
explicit-exit-notify 1
`
	dir := t.TempDir()
	p, err := parseClientProfile(content, dir, filepath.Join(dir, "out"))
	require.NoError(t, err)
	require.Equal(t, "vpn.example.com:1194,backup.example.com:1194:udp", p.Remotes)
	require.Equal(t, "udp", p.Proto)
	require.Equal(t, "tun", p.DevType)
	require.Equal(t, "AES-256-GCM", p.Cipher)
	require.FileExists(t, p.CAPath)
	require.FileExists(t, p.CertPath)
	require.FileExists(t, p.KeyPath)
	require.FileExists(t, p.TLSCrypt)
	require.Contains(t, p.Extra, "explicit-exit-notify")
}

func TestParseClientProfilePaths(t *testing.T) {
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(ca, []byte("x"), 0o600))
	content := "remote 1.2.3.4 443 tcp\nca ca.crt\ncert client.crt\nkey client.key\n"
	p, err := parseClientProfile(content, dir, filepath.Join(dir, "out"))
	require.NoError(t, err)
	require.Equal(t, "1.2.3.4:443:tcp", p.Remotes)
	require.Equal(t, ca, p.CAPath)
	require.Equal(t, filepath.Join(dir, "client.crt"), p.CertPath)
}
