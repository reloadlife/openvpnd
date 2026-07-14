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
	require.Contains(t, extra, "persist-remote-ip")
	require.Len(t, plugins, 1)
	require.Equal(t, "/opt/p.so", plugins[0].Path)
	require.NotEmpty(t, env)
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
	require.True(t, ids["custom_one"])
}
