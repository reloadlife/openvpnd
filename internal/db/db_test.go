package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
)

func TestStoreCRUD(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
		"v26":     "/opt/openvpn/sbin/openvpn",
	}))

	bins, err := store.ListBinaries(ctx)
	require.NoError(t, err)
	require.Len(t, bins, 2)

	inst, err := store.CreateInstance(ctx, db.Instance{
		Name:          "ovpn0",
		Role:          "server",
		Enabled:       true,
		BinaryName:    "v26",
		Port:          1194,
		ServerNetwork: "10.8.0.0/24",
		Topology:      "subnet",
	})
	require.NoError(t, err)
	require.Equal(t, "server", inst.Role)
	require.Equal(t, "v26", inst.BinaryName)

	cli, err := store.CreateClient(ctx, "ovpn0", db.Client{
		CommonName: "alice",
		Name:       "Alice",
		StaticIP:   "10.8.0.2",
	})
	require.NoError(t, err)
	require.Equal(t, "alice", cli.CommonName)
	require.Equal(t, "ovpn0", cli.InstanceName)

	clients, err := store.ListClientsByInstance(ctx, "ovpn0")
	require.NoError(t, err)
	require.Len(t, clients, 1)

	require.NoError(t, store.AddEvent(ctx, "info", "test", "ovpn0", "alice", "hello", "{}"))
	ev, err := store.ListEvents(ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, ev)
}
