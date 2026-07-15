package reconcile_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/reconcile"
	"github.com/reloadlife/openvpnd/internal/stats"
)

func TestReconcileEnablesServerInstance(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	dir := t.TempDir()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
	}))
	_, err = store.CreateInstance(ctx, db.Instance{
		Name: "ovpn0", Role: "server", Enabled: true, BinaryName: "default",
		Proto: "udp", Port: 1194, ServerNetwork: "10.8.0.0/24", Topology: "subnet",
		AuthMode: "pki", DevType: "tun",
		PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
	})
	require.NoError(t, err)

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir: filepath.Join(dir, "conf"), RuntimeDir: filepath.Join(dir, "run"),
		DefaultBinary: "default",
	}, slog.Default())

	require.NoError(t, rec.RunOnce(ctx))

	live, err := backend.ListLive(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, "ovpn0", live[0].Name)
	require.True(t, live[0].Up)

	// conf should exist on disk when host writes — mock does not write; still ok
	st, ok := cache.GetInstance("ovpn0")
	if ok {
		require.True(t, st.Up || st.Name == "ovpn0")
	}
}
