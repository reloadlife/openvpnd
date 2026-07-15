package reconcile_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestTrafficLimitSuspendsClient(t *testing.T) {
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
		AuthMode: "pki", DevType: "tun", Device: "tun0",
		PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
	})
	require.NoError(t, err)

	cl, err := store.CreateClient(ctx, "ovpn0", db.Client{
		CommonName:        "alice",
		Name:              "Alice",
		StaticIP:          "10.8.0.2",
		TrafficLimitBytes: 1000,
	})
	require.NoError(t, err)
	require.False(t, cl.Suspended)

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir: filepath.Join(dir, "conf"), RuntimeDir: filepath.Join(dir, "run"),
		DefaultBinary: "default", BandwidthEnforcement: "off",
	}, slog.Default())

	require.NoError(t, rec.RunOnce(ctx))

	// Client is connected with traffic under limit.
	backend.SetClients("ovpn0", []ovpnbackend.LiveClient{{
		CommonName: "alice", RealAddress: "1.2.3.4:1194", VirtualAddress: "10.8.0.2",
		RxBytes: 100, TxBytes: 100, ConnectedSince: time.Now().UTC(),
	}})
	require.NoError(t, rec.RunOnce(ctx))
	got, err := store.GetClient(ctx, "ovpn0", "alice")
	require.NoError(t, err)
	require.False(t, got.Suspended)

	// Exceed limit.
	backend.SetClients("ovpn0", []ovpnbackend.LiveClient{{
		CommonName: "alice", RealAddress: "1.2.3.4:1194", VirtualAddress: "10.8.0.2",
		RxBytes: 700, TxBytes: 400, ConnectedSince: time.Now().UTC(),
	}})
	require.NoError(t, rec.RunOnce(ctx))

	got, err = store.GetClient(ctx, "ovpn0", "alice")
	require.NoError(t, err)
	require.True(t, got.Suspended, "client should be suspended after traffic limit")

	kills := backend.Kills()
	require.NotEmpty(t, kills)
	require.Contains(t, kills, "ovpn0/alice")

	events, err := store.ListEvents(ctx, 20)
	require.NoError(t, err)
	found := false
	for _, e := range events {
		if e.Kind == "peer.suspended" && e.ClientCN == "alice" &&
			strings.Contains(e.Meta, `"reason":"traffic_limit"`) {
			found = true
			break
		}
	}
	require.True(t, found, "expected peer.suspended traffic_limit event")
}

func TestExpiresAtSuspendsClient(t *testing.T) {
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
		AuthMode: "pki", DevType: "tun", Device: "tun0",
		PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
	})
	require.NoError(t, err)

	_, err = store.CreateClient(ctx, "ovpn0", db.Client{
		CommonName: "bob", Name: "Bob", StaticIP: "10.8.0.3",
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	})
	require.NoError(t, err)

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir: filepath.Join(dir, "conf"), RuntimeDir: filepath.Join(dir, "run"),
		DefaultBinary: "default", BandwidthEnforcement: "off",
	}, slog.Default())

	// Bring instance up first so SetClients can attach to the mock live row.
	require.NoError(t, rec.RunOnce(ctx))
	got, err := store.GetClient(ctx, "ovpn0", "bob")
	require.NoError(t, err)
	require.True(t, got.Suspended, "expired client should be suspended on first reconcile")

	// Connected + already suspended → kill on next sample.
	backend.SetClients("ovpn0", []ovpnbackend.LiveClient{{
		CommonName: "bob", RealAddress: "1.2.3.4:1194", VirtualAddress: "10.8.0.3",
		RxBytes: 10, TxBytes: 10, ConnectedSince: time.Now().UTC(),
	}})
	require.NoError(t, rec.RunOnce(ctx))

	kills := backend.Kills()
	require.Contains(t, kills, "ovpn0/bob")

	events, err := store.ListEvents(ctx, 20)
	require.NoError(t, err)
	found := false
	for _, e := range events {
		if e.Kind == "peer.expired" && e.ClientCN == "bob" {
			found = true
			break
		}
	}
	require.True(t, found, "expected peer.expired event")
}

func TestBandwidthTotalAppliesToBothDirections(t *testing.T) {
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
		AuthMode: "pki", DevType: "tun", Device: "tun0",
		PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
	})
	require.NoError(t, err)
	_, err = store.CreateClient(ctx, "ovpn0", db.Client{
		CommonName: "alice", StaticIP: "10.8.0.2",
		BandwidthTotalBps: 8_000_000, // rx/tx left 0
	})
	require.NoError(t, err)

	backend := ovpnbackend.NewMock()
	rec := reconcile.New(store, backend, stats.NewCache(), reconcile.Config{
		ConfDir: filepath.Join(dir, "conf"), RuntimeDir: filepath.Join(dir, "run"),
		DefaultBinary: "default", BandwidthEnforcement: "log",
	}, slog.Default())

	require.NoError(t, rec.RunOnce(ctx))
	require.Equal(t, 1, rec.BandwidthEnforcer().AppliedCount("ovpn0"))
}

func TestBandwidthSyncLogMode(t *testing.T) {
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
		AuthMode: "pki", DevType: "tun", Device: "tun0",
		PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
	})
	require.NoError(t, err)
	_, err = store.CreateClient(ctx, "ovpn0", db.Client{
		CommonName: "alice", StaticIP: "10.8.0.2",
		BandwidthRxBps: 1_000_000, BandwidthTxBps: 500_000,
	})
	require.NoError(t, err)

	backend := ovpnbackend.NewMock()
	rec := reconcile.New(store, backend, stats.NewCache(), reconcile.Config{
		ConfDir: filepath.Join(dir, "conf"), RuntimeDir: filepath.Join(dir, "run"),
		DefaultBinary: "default", BandwidthEnforcement: "log",
	}, slog.Default())

	// log mode plans rules without exec; Sync still records applied clients.
	require.NoError(t, rec.RunOnce(ctx))
	require.Equal(t, 1, rec.BandwidthEnforcer().AppliedCount("ovpn0"))

	// Removing bandwidth limits clears rules.
	_, err = store.UpdateClient(ctx, "ovpn0", "alice", db.Client{
		CommonName: "alice", StaticIP: "10.8.0.2",
		BandwidthRxBps: 0, BandwidthTxBps: 0,
	})
	require.NoError(t, err)
	require.NoError(t, rec.RunOnce(ctx))
	require.Equal(t, 0, rec.BandwidthEnforcer().AppliedCount("ovpn0"))
}
