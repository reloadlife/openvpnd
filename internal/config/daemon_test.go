package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/config"
)

func TestLoadDaemonDefaults(t *testing.T) {
	cfg, err := config.LoadDaemon("")
	require.NoError(t, err)
	require.Equal(t, "off", cfg.OpenVPN.BandwidthEnforcement)
	require.Equal(t, "hybrid", cfg.OpenVPN.Persistence)
	require.NotEmpty(t, cfg.Listen.HTTP)
	require.Equal(t, 24*time.Hour, cfg.ProfileLinkTTL())
	require.Contains(t, cfg.PublicBase(), "http://")
}

func TestLoadDaemonBandwidthModes(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []string{"off", "tc", "shaper", "log", "htb", "none"} {
		path := filepath.Join(dir, mode+".yaml")
		content := "auth:\n  token: t\nopenvpn:\n  bandwidth_enforcement: " + mode + "\n"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		cfg, err := config.LoadDaemon(path)
		require.NoError(t, err, mode)
		switch mode {
		case "htb":
			require.Equal(t, "tc", cfg.OpenVPN.BandwidthEnforcement)
		case "none", "off":
			require.Equal(t, "off", cfg.OpenVPN.BandwidthEnforcement)
		default:
			require.Equal(t, mode, cfg.OpenVPN.BandwidthEnforcement)
		}
	}

	bad := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("auth:\n  token: t\nopenvpn:\n  bandwidth_enforcement: banana\n"), 0o600))
	_, err := config.LoadDaemon(bad)
	require.Error(t, err)
}

func TestProfileLinkTTLFallback(t *testing.T) {
	cfg := &config.DaemonConfig{}
	cfg.ProfileLinks.DefaultTTL = "not-a-duration"
	require.Equal(t, 24*time.Hour, cfg.ProfileLinkTTL())

	cfg.ProfileLinks.DefaultTTL = "15m"
	require.Equal(t, 15*time.Minute, cfg.ProfileLinkTTL())
}
