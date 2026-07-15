package features_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/features"
)

func TestExpandUDPStuffing(t *testing.T) {
	extra, plugins, env := features.Expand(
		[]string{"udp_stuffing", "mssfix"},
		nil,
		"persist-remote-ip",
		[]db.Plugin{{Path: "/opt/p.so", Args: []string{"a=1"}}},
		[]db.EnvVar{{Name: "FOO", Value: "bar"}},
	)
	require.Contains(t, extra, "mssfix")
	require.Contains(t, extra, "stuffing")
	require.Contains(t, extra, "binary_name")
	require.Contains(t, extra, "persist-remote-ip")
	require.Len(t, plugins, 1)
	require.Equal(t, "/opt/p.so", plugins[0].Path)
	require.NotEmpty(t, env)
}

func TestExpandUDPStuffingEnv(t *testing.T) {
	extra, _, env := features.Expand(
		[]string{"udp_stuffing_env"},
		nil, "", nil, nil,
	)
	require.Contains(t, extra, "STUFFING_ENABLE")
	require.Contains(t, extra, "binary_name")
	names := map[string]string{}
	for _, e := range env {
		names[e.Name] = e.Value
	}
	require.Equal(t, "1", names["STUFFING_ENABLE"])
}

func TestExpandAuthScriptTemplate(t *testing.T) {
	extra, _, _ := features.Expand(
		[]string{"auth_script_template"},
		nil, "", nil, nil,
	)
	require.Contains(t, extra, "script-security 2")
	require.Contains(t, extra, "auth-user-pass-verify")
	require.Contains(t, extra, "/usr/local/libexec/openvpnd-auth.sh")
	require.Contains(t, extra, "via-env")
	require.Contains(t, extra, "EXAMPLE")
}

func TestExpandTLSModern(t *testing.T) {
	extra, _, _ := features.Expand(
		[]string{"tls_modern"},
		nil, "", nil, nil,
	)
	require.Contains(t, extra, "tls-version-min 1.2")
	require.Contains(t, extra, "tls-groups X25519:P-256")
}

func TestListMerged(t *testing.T) {
	list := features.ListMerged([]db.FeaturePreset{
		{ID: "custom_one", Description: "x"},
	})
	ids := map[string]bool{}
	for _, p := range list {
		ids[p.ID] = true
	}
	require.True(t, ids["udp_stuffing"])
	require.True(t, ids["udp_stuffing_env"])
	require.True(t, ids["auth_script_template"])
	require.True(t, ids["tls_modern"])
	require.True(t, ids["custom_one"])
}
