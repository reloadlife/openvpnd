package adopt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDiscoverOpenVPNWithFakeProc builds a synthetic /proc tree.
func TestDiscoverOpenVPNWithFakeProc(t *testing.T) {
	root := t.TempDir()

	// openvpn with --config
	writeProc(t, root, "4242", "/usr/sbin/openvpn\x00--config\x00/etc/openvpn/server.conf\x00")
	// openvpn bare .conf
	writeProc(t, root, "4243", "/usr/local/sbin/openvpn\x00/etc/openvpn/client.ovpn\x00")
	// not openvpn
	writeProc(t, root, "100", "/usr/bin/bash\x00-c\x00sleep 1\x00")
	// non-numeric dir
	require.NoError(t, os.Mkdir(filepath.Join(root, "self"), 0o755))
	// openvpnd must be skipped
	writeProc(t, root, "7", "/usr/bin/openvpnd\x00run\x00")

	cands, err := discoverOpenVPN(root)
	require.NoError(t, err)
	require.Len(t, cands, 2)

	byPID := map[int]Candidate{}
	for _, c := range cands {
		byPID[c.PID] = c
	}
	require.Equal(t, "/etc/openvpn/server.conf", byPID[4242].ConfPath)
	require.Equal(t, "/usr/sbin/openvpn", byPID[4242].Binary)
	require.Contains(t, byPID[4242].Cmdline, "--config")
	require.Equal(t, "/etc/openvpn/client.ovpn", byPID[4243].ConfPath)
}

func writeProc(t *testing.T, root, pid, cmdline string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644))
}
