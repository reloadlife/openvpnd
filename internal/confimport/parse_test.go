package confimport_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/confimport"
)

const serverFixture = `
# example server conf
port 1194
proto udp
dev tun
topology subnet
server 10.8.0.0 255.255.255.0
server-ipv6 fd00:1194:0::/64
ca /etc/openvpn/ca.crt
cert /etc/openvpn/server.crt
key /etc/openvpn/server.key
dh /etc/openvpn/dh.pem
tls-crypt /etc/openvpn/tls-crypt.key
cipher AES-256-GCM
data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305
auth SHA256
tls-version-min 1.2
tun-mtu 1400
sndbuf 393216
rcvbuf 393216
max-clients 100
push "redirect-gateway def1 bypass-dhcp"
push "dhcp-option DNS 1.1.1.1"
push "dhcp-option DNS 8.8.8.8"
push "dhcp-option DOMAIN corp.example"
push "route 10.0.0.0 255.255.0.0"
plugin /usr/lib/openvpn/plugins/openvpn-plugin-auth-pam.so login
keepalive 10 120
persist-key
persist-tun
status /var/run/openvpn/status.log 1
writepid /var/run/openvpn/server.pid
management /var/run/openvpn/mgmt.sock unix
verb 3
client-to-client
explicit-exit-notify 1
`

const clientFixture = `
client
dev tun
proto udp
remote vpn.example.com 1194
remote backup.example.com 1194 tcp
resolv-retry infinite
nobind
persist-key
persist-tun
remote-cert-tls server
cipher AES-256-GCM
auth SHA256
data-ciphers AES-256-GCM:AES-128-GCM
ca /etc/openvpn/client/ca.crt
cert /etc/openvpn/client/client.crt
key /etc/openvpn/client/client.key
tls-crypt /etc/openvpn/client/tls-crypt.key
verb 3
explicit-exit-notify 1
`

func TestParseServer(t *testing.T) {
	r, err := confimport.Parse(serverFixture)
	require.NoError(t, err)
	require.Equal(t, "server", r.Role)
	require.Equal(t, "udp", r.Proto)
	require.Equal(t, 1194, r.Port)
	require.Equal(t, "tun", r.DevType)
	require.Equal(t, "10.8.0.0/24", r.ServerNetwork)
	require.Equal(t, "subnet", r.Topology)
	require.Equal(t, "pki", r.AuthMode)
	require.Equal(t, "/etc/openvpn/ca.crt", r.PKICaPath)
	require.Equal(t, "/etc/openvpn/server.crt", r.PKICertPath)
	require.Equal(t, "/etc/openvpn/server.key", r.PKIKeyPath)
	require.Equal(t, "/etc/openvpn/dh.pem", r.PKIDHPath)
	require.Equal(t, "/etc/openvpn/tls-crypt.key", r.TLSCryptPath)
	require.Equal(t, "AES-256-GCM", r.Cipher)
	require.Equal(t, "AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305", r.DataCiphers)
	require.Equal(t, "SHA256", r.AuthDigest)
	require.True(t, r.RedirectGateway)
	require.Equal(t, []string{"1.1.1.1", "8.8.8.8"}, r.PushDNS)
	require.Equal(t, "corp.example", r.PushDomain)
	require.Equal(t, []string{"10.0.0.0/16"}, r.PushRoutes)
	require.Equal(t, 100, r.MaxClients)
	require.Equal(t, "1.2", r.TLSVersionMin)
	require.Equal(t, 1400, r.TunMTU)
	require.Equal(t, 393216, r.Sndbuf)
	require.Equal(t, 393216, r.Rcvbuf)
	require.Equal(t, "fd00:1194:0::/64", r.ServerIPv6)
	require.Len(t, r.Plugins, 1)
	require.Equal(t, "/usr/lib/openvpn/plugins/openvpn-plugin-auth-pam.so", r.Plugins[0].Path)
	require.Equal(t, []string{"login"}, r.Plugins[0].Args)
	// control plane ignored
	require.NotContains(t, r.ExtraDirectives, "management")
	require.NotContains(t, r.ExtraDirectives, "writepid")
	require.NotContains(t, r.ExtraDirectives, "status")
	require.NotContains(t, r.ExtraDirectives, "verb ")
	require.NotContains(t, r.ExtraDirectives, "persist-key")
	require.NotContains(t, r.ExtraDirectives, "keepalive")
	// useful long-tail preserved
	require.Contains(t, r.ExtraDirectives, "client-to-client")
	require.Contains(t, r.ExtraDirectives, "explicit-exit-notify")
}

func TestParseClient(t *testing.T) {
	r, err := confimport.Parse(clientFixture)
	require.NoError(t, err)
	require.Equal(t, "client", r.Role)
	require.Equal(t, "udp", r.Proto)
	require.Equal(t, "tun", r.DevType)
	require.Len(t, r.Remotes, 2)
	require.Equal(t, "vpn.example.com", r.Remotes[0].Host)
	require.Equal(t, 1194, r.Remotes[0].Port)
	require.Equal(t, "backup.example.com", r.Remotes[1].Host)
	require.Equal(t, "tcp", r.Remotes[1].Proto)
	require.Equal(t, "/etc/openvpn/client/ca.crt", r.PKICaPath)
	require.Equal(t, "AES-256-GCM", r.Cipher)
	require.Equal(t, "SHA256", r.AuthDigest)
	require.Contains(t, r.ExtraDirectives, "explicit-exit-notify")
	require.Contains(t, r.ExtraDirectives, "remote-cert-tls")
}

func TestParseInlineWarns(t *testing.T) {
	content := `
client
remote 1.2.3.4 443
<ca>
-----BEGIN CERTIFICATE-----
CA
-----END CERTIFICATE-----
</ca>
`
	r, err := confimport.Parse(content)
	require.NoError(t, err)
	require.Equal(t, "client", r.Role)
	require.NotEmpty(t, r.Warnings)
	require.Contains(t, r.Warnings[0], "inline <ca>")
	require.True(t, r.HasInline())
	require.Contains(t, r.Inline, "ca")
	// Inline PEMs must not leak into extra directives.
	require.NotContains(t, r.ExtraDirectives, "BEGIN CERTIFICATE")
	require.NotContains(t, r.ExtraDirectives, "<ca>")
}

const inlineClientFixture = `
client
dev tun
proto udp
remote vpn.example.com 1194
cipher AES-256-GCM
auth SHA256
<ca>
-----BEGIN CERTIFICATE-----
CA_BODY
-----END CERTIFICATE-----
</ca>
<cert>
-----BEGIN CERTIFICATE-----
CERT_BODY
-----END CERTIFICATE-----
</cert>
<key>
-----BEGIN PRIVATE KEY-----
KEY_BODY
-----END PRIVATE KEY-----
</key>
<tls-crypt>
-----BEGIN OpenVPN Static key V1-----
TC_BODY
-----END OpenVPN Static key V1-----
</tls-crypt>
explicit-exit-notify 1
`

func TestMaterializeInlinePEMs(t *testing.T) {
	r, err := confimport.Parse(inlineClientFixture)
	require.NoError(t, err)
	require.Equal(t, "client", r.Role)
	require.True(t, r.HasInline())
	require.NotEmpty(t, r.Warnings)
	require.Empty(t, r.PKICaPath)
	require.NotContains(t, r.ExtraDirectives, "BEGIN CERTIFICATE")
	require.Contains(t, r.ExtraDirectives, "explicit-exit-notify")

	dir := t.TempDir()
	err = r.Materialize(confimport.MaterializeOptions{DestDir: dir})
	require.NoError(t, err)
	require.False(t, r.HasInline())
	// Inline warnings cleared after successful materialize.
	for _, w := range r.Warnings {
		require.NotContains(t, w, "inline <")
	}
	require.FileExists(t, r.PKICaPath)
	require.FileExists(t, r.PKICertPath)
	require.FileExists(t, r.PKIKeyPath)
	require.FileExists(t, r.TLSCryptPath)
	require.Equal(t, filepath.Join(dir, "ca.crt"), r.PKICaPath)
	require.Equal(t, filepath.Join(dir, "client.crt"), r.PKICertPath)
	require.Equal(t, filepath.Join(dir, "client.key"), r.PKIKeyPath)
	require.Equal(t, filepath.Join(dir, "tls-crypt.key"), r.TLSCryptPath)

	caPEM, err := os.ReadFile(r.PKICaPath)
	require.NoError(t, err)
	require.Contains(t, string(caPEM), "CA_BODY")

	// ToCreateRequest carries absolute paths and disables auto-issue.
	req := r.ToCreateRequest()
	require.Equal(t, r.PKICaPath, req.PKICaPath)
	require.Equal(t, r.PKICertPath, req.PKICertPath)
	require.Equal(t, r.PKIKeyPath, req.PKIKeyPath)
	require.Equal(t, r.TLSCryptPath, req.PKITLSCryptPath)
	require.NotNil(t, req.IssueServerCert)
	require.False(t, *req.IssueServerCert)
}

func TestMaterializeServerRoleNames(t *testing.T) {
	content := `
port 1194
proto udp
dev tun
server 10.8.0.0 255.255.255.0
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
`
	r, err := confimport.Parse(content)
	require.NoError(t, err)
	require.Equal(t, "server", r.Role)
	dir := t.TempDir()
	require.NoError(t, r.Materialize(confimport.MaterializeOptions{DestDir: dir}))
	require.Equal(t, filepath.Join(dir, "server.crt"), r.PKICertPath)
	require.Equal(t, filepath.Join(dir, "server.key"), r.PKIKeyPath)
}

func TestMaterializeNoopEmpty(t *testing.T) {
	r, err := confimport.Parse(clientFixture)
	require.NoError(t, err)
	require.False(t, r.HasInline())
	require.NoError(t, r.Materialize(confimport.MaterializeOptions{DestDir: t.TempDir()}))
}

func TestMaterializeRequiresDest(t *testing.T) {
	r, err := confimport.Parse(inlineClientFixture)
	require.NoError(t, err)
	err = r.Materialize(confimport.MaterializeOptions{})
	require.Error(t, err)
}

func TestNetworkMaskToCIDR(t *testing.T) {
	cidr, err := confimport.NetworkMaskToCIDR("10.8.0.0", "255.255.255.0")
	require.NoError(t, err)
	require.Equal(t, "10.8.0.0/24", cidr)

	cidr, err = confimport.NetworkMaskToCIDR("192.168.1.0", "255.255.255.0")
	require.NoError(t, err)
	require.Equal(t, "192.168.1.0/24", cidr)

	cidr, err = confimport.NetworkMaskToCIDR("10.0.0.0", "255.255.0.0")
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", cidr)
}

func TestToCreateRequest(t *testing.T) {
	r, err := confimport.Parse(serverFixture)
	require.NoError(t, err)
	req := r.ToCreateRequest()
	require.Equal(t, "server", req.Role)
	require.Equal(t, "10.8.0.0/24", req.ServerNetwork)
	require.Equal(t, 1194, req.Port)
	require.True(t, req.RedirectGateway)
	require.Equal(t, []string{"1.1.1.1", "8.8.8.8"}, req.PushDNS)
	require.Equal(t, []string{"10.0.0.0/16"}, req.PushRoutes)
	require.NotNil(t, req.IssueServerCert)
	require.False(t, *req.IssueServerCert)
	require.Contains(t, req.ExtraDirectives, "max-clients 100")
	require.Contains(t, req.ExtraDirectives, "tls-version-min 1.2")
	require.Contains(t, req.ExtraDirectives, "tun-mtu 1400")
	require.Contains(t, req.ExtraDirectives, "server-ipv6")
	require.Len(t, req.Plugins, 1)
}

func TestParseDevDevice(t *testing.T) {
	r, err := confimport.Parse("server 10.8.0.0 255.255.255.0\ndev tun0\nproto udp\nport 1194\n")
	require.NoError(t, err)
	require.Equal(t, "tun", r.DevType)
	require.Equal(t, "tun0", r.Device)
}

func TestParseEmptyFails(t *testing.T) {
	_, err := confimport.Parse("# just comments\n")
	require.Error(t, err)
}
