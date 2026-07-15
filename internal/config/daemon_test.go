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
	require.True(t, cfg.OpenVPN.AdoptTakeoverEnabled, "adopt_takeover_enabled defaults true")
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

func TestValidateRejectsWeakToken(t *testing.T) {
	t.Setenv("OPENVPND_ALLOW_INSECURE", "")

	for _, tok := range []string{"", "change-me", "  change-me  "} {
		// trim only applies via WeakAuthToken on empty/change-me; spaces around change-me still weak after TrimSpace
		cfg := &config.DaemonConfig{}
		cfg.Auth.Token = tok
		if tok == "  change-me  " {
			// WeakAuthToken trims, so this is still weak
		}
		err := cfg.Validate()
		require.Error(t, err, "token %q", tok)
		require.Contains(t, err.Error(), "change-me")
	}

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "strong-secret"
	require.NoError(t, cfg.Validate())
}

func TestValidateAllowInsecure(t *testing.T) {
	t.Setenv("OPENVPND_ALLOW_INSECURE", "1")
	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "change-me"
	require.NoError(t, cfg.Validate())
}

func TestValidateProductionRejectsWeakTokenEvenWithAllowInsecure(t *testing.T) {
	t.Setenv("OPENVPND_ALLOW_INSECURE", "1")
	cfg := &config.DaemonConfig{}
	cfg.Production = true
	cfg.Auth.Token = "change-me"
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "production")

	cfg.Auth.Token = "prod-secret"
	require.NoError(t, cfg.Validate())
}

func TestValidateEmptyToken(t *testing.T) {
	t.Setenv("OPENVPND_ALLOW_INSECURE", "")
	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = ""
	err := cfg.Validate()
	require.Error(t, err)
}

func TestLoadDaemonAuthTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.yaml")
	content := `
auth:
  token: "legacy"
  tokens:
    - name: admin
      token: "a-secret"
      role: admin
    - name: ops
      token: "o-secret"
      role: operator
    - name: ro
      token: "r-secret"
      role: readonly
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	cfg, err := config.LoadDaemon(path)
	require.NoError(t, err)
	require.Len(t, cfg.Auth.Tokens, 3)
	require.Equal(t, "admin", cfg.Auth.Tokens[0].Role)
	require.Equal(t, "operator", cfg.Auth.Tokens[1].Role)
	require.Equal(t, "readonly", cfg.Auth.Tokens[2].Role)

	principals := cfg.AuthPrincipals()
	require.Len(t, principals, 3)
	// multi-token mode: legacy token not accepted
	for _, p := range principals {
		require.NotEqual(t, "legacy", p.Token)
	}
}

func TestLoadDaemonAuthTokensInvalidRole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-role.yaml")
	content := "auth:\n  tokens:\n    - name: x\n      token: t\n      role: superuser\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	_, err := config.LoadDaemon(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "role")
}

func TestAuthPrincipalsLegacy(t *testing.T) {
	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "only-token"
	ps := cfg.AuthPrincipals()
	require.Len(t, ps, 1)
	require.Equal(t, "admin", ps[0].Role)
	require.Equal(t, "only-token", ps[0].Token)
}

func TestWeakAuthTokenMulti(t *testing.T) {
	cfg := &config.DaemonConfig{}
	cfg.Auth.Tokens = []config.AuthToken{{Name: "a", Token: "change-me", Role: "admin"}}
	require.True(t, cfg.WeakAuthToken())

	cfg.Auth.Tokens = []config.AuthToken{{Name: "a", Token: "strong", Role: "admin"}}
	require.False(t, cfg.WeakAuthToken())
}

func TestNormalizeRole(t *testing.T) {
	require.Equal(t, config.RoleAdmin, config.NormalizeRole("ADMIN"))
	require.Equal(t, config.RoleOperator, config.NormalizeRole("ops"))
	require.Equal(t, config.RoleReadonly, config.NormalizeRole("read_only"))
	require.Equal(t, "", config.NormalizeRole("nope"))
}
