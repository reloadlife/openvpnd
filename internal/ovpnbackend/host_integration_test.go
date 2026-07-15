//go:build integration

package ovpnbackend_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
)

// TestHostBackendProbeAndEnsure exercises the real HostBackend against a local
// openvpn binary. Skips when openvpn is missing or TUN setup is not permitted.
func TestHostBackendProbeAndEnsure(t *testing.T) {
	bin := ovpnbackend.FindOpenVPN()
	if bin == "" {
		t.Skip("openvpn not in PATH or common install locations")
	}
	if !ovpnbackend.HasNetAdmin() {
		t.Skip("not root and CAP_NET_ADMIN not effective; cannot create TUN")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	root := t.TempDir()
	confDir := filepath.Join(root, "conf")
	runDir := filepath.Join(root, "run")

	backend, err := ovpnbackend.NewHostBackend(ovpnbackend.HostOptions{
		ConfDir:    confDir,
		RuntimeDir: runDir,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = backend.Close() })

	// ProbeBinary must return something that looks like OpenVPN.
	ver, err := backend.ProbeBinary(ctx, bin)
	require.NoError(t, err)
	require.NotEmpty(t, ver)
	require.Contains(t, ver, "OpenVPN")
	t.Logf("probed: %s", ver)

	// Static key for a minimal p2p instance (no PKI).
	secretPath := filepath.Join(runDir, "static.key")
	require.NoError(t, genStaticKey(ctx, bin, secretPath))

	name := "itest0"
	confPath := filepath.Join(confDir, name+".conf")
	pidPath := filepath.Join(runDir, name+".pid")
	mgmtPath := filepath.Join(runDir, name+".mgmt.sock")
	// High localhost-only port to reduce collisions with production VPNs.
	port := 25194
	conf := minimalIntegrationConf(secretPath, pidPath, mgmtPath, port)

	err = backend.EnsureInstance(ctx, ovpnbackend.DesiredInstance{
		Name:        name,
		Role:        "server",
		Enabled:     true,
		BinaryPath:  bin,
		ConfPath:    confPath,
		ConfContent: conf,
		ConfHash:    "integration-1",
		PIDPath:     pidPath,
		MgmtPath:    mgmtPath,
	})
	require.NoError(t, err)

	// Conf must have been written even if the process dies quickly.
	body, err := os.ReadFile(confPath)
	require.NoError(t, err)
	require.Contains(t, string(body), "dev tun")
	require.Contains(t, string(body), secretPath)

	// Give openvpn a moment to settle (HostBackend already sleeps ~200ms).
	deadline := time.Now().Add(3 * time.Second)
	var up bool
	var livePID int
	for time.Now().Before(deadline) {
		live, lerr := backend.ListLive(ctx)
		require.NoError(t, lerr)
		for _, li := range live {
			if li.Name == name && li.Up {
				up = true
				livePID = li.PID
				break
			}
		}
		if up {
			break
		}
		// Process may have exited; surface log for diagnosis.
		if logBody, rerr := os.ReadFile(filepath.Join(runDir, name+".log")); rerr == nil && len(logBody) > 0 {
			t.Logf("openvpn log so far:\n%s", string(logBody))
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !up {
		logBody, _ := os.ReadFile(filepath.Join(runDir, name+".log"))
		t.Fatalf("instance %s never reported Up; log:\n%s", name, string(logBody))
	}
	require.Greater(t, livePID, 0)

	// Optional management probe — soft-fail if socket not ready yet.
	mgmtCtx, mgmtCancel := context.WithTimeout(ctx, 2*time.Second)
	defer mgmtCancel()
	if mgmt, merr := backend.Management(mgmtCtx, name); merr == nil {
		st, serr := mgmt.Status(mgmtCtx)
		_ = mgmt.Close()
		if serr == nil {
			t.Logf("mgmt status: name=%s clients=%d", st.Name, len(st.Clients))
		} else {
			t.Logf("mgmt status: %v", serr)
		}
	} else {
		t.Logf("mgmt dial skipped: %v", merr)
	}

	require.NoError(t, backend.StopInstance(ctx, name))

	// After stop, process should not be listed as up.
	live, err := backend.ListLive(ctx)
	require.NoError(t, err)
	for _, li := range live {
		if li.Name == name {
			require.False(t, li.Up, "instance still up after stop")
		}
	}
}

func genStaticKey(ctx context.Context, openvpnBin, secretPath string) error {
	// OpenVPN 2.5+: openvpn --genkey secret <file>
	// Older: openvpn --genkey --secret <file>
	cmd := exec.CommandContext(ctx, openvpnBin, "--genkey", "secret", secretPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		cmd2 := exec.CommandContext(ctx, openvpnBin, "--genkey", "--secret", secretPath)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("genkey: %w (%s); fallback: %v (%s)", err, out, err2, out2)
		}
	}
	return nil
}

func minimalIntegrationConf(secretPath, pidPath, mgmtPath string, port int) string {
	// Static-key p2p is deprecated but still works on 2.6 for short lab runs.
	// OpenSSL 3 / OpenVPN 2.6 reject the legacy BF-CBC default — set AES-256-CBC.
	return fmt.Sprintf(`dev tun
proto udp
local 127.0.0.1
port %d
ifconfig 10.255.254.1 10.255.254.2
secret %s
cipher AES-256-CBC
auth SHA256
persist-tun
persist-key
ping 10
ping-restart 60
verb 3
writepid %s
management %s unix
`, port, secretPath, pidPath, mgmtPath)
}
