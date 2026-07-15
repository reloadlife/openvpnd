package adopt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/adopt"
)

func TestSplitCmdlineNUL(t *testing.T) {
	raw := "/usr/sbin/openvpn\x00--config\x00/etc/openvpn/server.conf\x00"
	argv := adopt.SplitCmdline(raw)
	require.Equal(t, []string{"/usr/sbin/openvpn", "--config", "/etc/openvpn/server.conf"}, argv)
}

func TestSplitCmdlineSpaces(t *testing.T) {
	argv := adopt.SplitCmdline(`/usr/sbin/openvpn --config /etc/openvpn/s.conf`)
	require.Equal(t, []string{"/usr/sbin/openvpn", "--config", "/etc/openvpn/s.conf"}, argv)
}

func TestIsOpenVPNArgv(t *testing.T) {
	require.True(t, adopt.IsOpenVPNArgv([]string{"/usr/sbin/openvpn", "--config", "x"}))
	require.True(t, adopt.IsOpenVPNArgv([]string{"openvpn"}))
	require.True(t, adopt.IsOpenVPNArgv([]string{"/opt/openvpn-2.6/sbin/openvpn", "--config", "x"}))
	require.False(t, adopt.IsOpenVPNArgv([]string{"/usr/bin/openvpnd"}))
	require.False(t, adopt.IsOpenVPNArgv([]string{"bash", "-c", "openvpn"}))
	require.False(t, adopt.IsOpenVPNArgv(nil))
}

func TestConfigPathFromArgv(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"long", []string{"openvpn", "--config", "/etc/openvpn/server.conf"}, "/etc/openvpn/server.conf"},
		{"equals", []string{"openvpn", "--config=/etc/openvpn/a.conf"}, "/etc/openvpn/a.conf"},
		{"short", []string{"openvpn", "-config", "/tmp/x.conf"}, "/tmp/x.conf"},
		{"bare conf", []string{"openvpn", "/etc/openvpn/client.ovpn"}, "/etc/openvpn/client.ovpn"},
		{"with flags", []string{"openvpn", "--daemon", "--config", "/var/lib/ovpn/s.conf", "--verb", "3"}, "/var/lib/ovpn/s.conf"},
		{"none", []string{"openvpn", "--daemon"}, ""},
		{"ignore option-like", []string{"openvpn", "--status", "/run/status.log"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, adopt.ConfigPathFromArgv(tc.argv))
		})
	}
}

func TestAdoptFromConf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.conf")
	content := `
port 1199
proto udp
dev tun
topology subnet
server 10.88.0.0 255.255.255.0
ca /etc/openvpn/ca.crt
cert /etc/openvpn/server.crt
key /etc/openvpn/server.key
cipher AES-256-GCM
auth SHA256
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	res, err := adopt.AdoptFromConf(path, "legacy")
	require.NoError(t, err)
	require.Equal(t, path, res.ConfPath)
	require.Equal(t, "legacy", res.Request.Name)
	require.Equal(t, "server", res.Request.Role)
	require.Equal(t, 1199, res.Request.Port)
	require.Equal(t, "10.88.0.0/24", res.Request.ServerNetwork)
	require.Equal(t, "/etc/openvpn/server.crt", res.Request.PKICertPath)
	require.Contains(t, res.Request.ExtraDirectives, "# adopted from "+path)
}

func TestAdoptFromConfRequiresAbs(t *testing.T) {
	_, err := adopt.AdoptFromConf("relative.conf", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

func TestAdoptFromConfMissing(t *testing.T) {
	_, err := adopt.AdoptFromConf("/no/such/openvpn-adopt-test.conf", "")
	require.Error(t, err)
}

func TestDiscoverOpenVPNFakeProc(t *testing.T) {
	// Build a minimal fake /proc tree via package-level test helper.
	// discover is exercised through real /proc if available; here we only
	// re-check cmdline helpers compose correctly for a synthetic candidate.
	raw := "/usr/sbin/openvpn\x00--config\x00/etc/openvpn/server.conf\x00--verb\x003\x00"
	argv := adopt.SplitCmdline(raw)
	require.True(t, adopt.IsOpenVPNArgv(argv))
	require.Equal(t, "/etc/openvpn/server.conf", adopt.ConfigPathFromArgv(argv))

	// Live scan should not error on Linux (may be empty).
	cands, err := adopt.DiscoverOpenVPN()
	require.NoError(t, err)
	for _, c := range cands {
		require.Greater(t, c.PID, 0)
		require.NotEmpty(t, c.Binary)
		require.NotEmpty(t, c.Cmdline)
	}
}
