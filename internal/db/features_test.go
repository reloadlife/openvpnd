package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
)

func TestFeaturePresetsCRUD(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	p, err := store.UpsertFeaturePreset(ctx, db.FeaturePreset{
		ID: "my_stuff", Description: "stuffing",
		ExtraDirectives: "stuffing-enable\n",
		Plugins:         []db.Plugin{{Path: "/opt/s.so", Args: []string{"a=1"}}},
		EnvVars:         []db.EnvVar{{Name: "X", Value: "1"}},
		Notes:           "lab",
	})
	require.NoError(t, err)
	require.Equal(t, "my_stuff", p.ID)
	require.Len(t, p.Plugins, 1)
	require.Equal(t, "/opt/s.so", p.Plugins[0].Path)

	got, err := store.GetFeaturePreset(ctx, "my_stuff")
	require.NoError(t, err)
	require.Equal(t, "stuffing-enable\n", got.ExtraDirectives)

	list, err := store.ListFeaturePresets(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, store.DeleteFeaturePreset(ctx, "my_stuff"))
	_, err = store.GetFeaturePreset(ctx, "my_stuff")
	require.Error(t, err)
}

func TestInstanceExtensionsRoundTrip(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{"default": "/usr/sbin/openvpn"}))

	inst, err := store.CreateInstance(ctx, db.Instance{
		Name: "ovpn0", Role: "server", Enabled: true, BinaryName: "default",
		Port: 1194, ServerNetwork: "10.8.0.0/24", Topology: "subnet",
		Plugins:     []db.Plugin{{Path: "/opt/p.so", Args: []string{"z"}}},
		EnvVars:     []db.EnvVar{{Name: "FOO", Value: "bar"}},
		FeatureSets: []string{"mssfix", "udp_stuffing"},
		ExtraDirectives: "tun-mtu 1400\n",
	})
	require.NoError(t, err)
	require.Len(t, inst.Plugins, 1)
	require.Equal(t, []string{"mssfix", "udp_stuffing"}, inst.FeatureSets)

	got, err := store.GetInstance(ctx, "ovpn0")
	require.NoError(t, err)
	require.Equal(t, "bar", got.EnvVars[0].Value)
	require.Contains(t, got.ExtraDirectives, "tun-mtu")
}
