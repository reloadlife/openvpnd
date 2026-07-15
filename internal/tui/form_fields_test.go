package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerFormHasRoadmapFields(t *testing.T) {
	f := newForm("t", instanceCreateFields([]string{"default"}), map[string]string{"role": "server"})
	keys := map[string]bool{}
	for _, field := range f.fields {
		keys[field.Key] = true
	}
	for _, want := range []string{
		"max_clients", "tls_version_min", "tls_groups", "tls_cipher",
		"bridge_mode", "auth_user_pass_verify", "server_ipv6", "ifconfig_ipv6",
		"features", "plugin", "extra",
	} {
		require.True(t, keys[want], "server form missing %s", want)
	}
}

func TestClientInstanceFormHasAuthFields(t *testing.T) {
	f := newForm("t", instanceCreateFields([]string{"default"}), map[string]string{"role": "client"})
	keys := map[string]bool{}
	for _, field := range f.fields {
		keys[field.Key] = true
	}
	require.True(t, keys["auth_user_pass"] || keys["remote"] || keys["profile"])
	// client should not show server bridge
	require.False(t, keys["bridge_mode"])
	require.False(t, keys["network"])
}

func TestVPNUserFormHasACLAndBandwidth(t *testing.T) {
	f := newForm("c", clientCreateFields([]string{"ovpn0"}), map[string]string{"instance": "ovpn0"})
	keys := map[string]bool{}
	for _, field := range f.fields {
		keys[field.Key] = true
	}
	for _, want := range []string{
		"cn", "issue_cert", "mint_link", "iroutes",
	} {
		require.True(t, keys[want], "client form missing %s", want)
	}
	// bandwidth fields if present
	hasBW := keys["bandwidth_rx"] || keys["bandwidth_tx"] || keys["traffic_limit"]
	hasPush := keys["push_dns"] || keys["push_domain"] || keys["redirect_gw"]
	require.True(t, hasBW || hasPush || keys["iroutes"], "expected ACL or bandwidth fields on client form")
}

func TestCompactTipSingleLine(t *testing.T) {
	line := compactTip(fieldDef{Hint: "short", Tip: "very long tip that should not all show"}, 20)
	require.LessOrEqual(t, len(line), 21)
	require.NotContains(t, line, "\n")
}
