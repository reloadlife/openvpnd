package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
)

// TestRoadmapFieldsRoundTrip verifies migration 00005/00006 columns persist.
func TestRoadmapFieldsRoundTrip(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{"default": "/usr/sbin/openvpn"}))

	_, err = store.CreateInstance(ctx, db.Instance{
		Name: "full", Role: "server", Enabled: true, BinaryName: "default",
		Port: 1194, ServerNetwork: "10.1.0.0/24", Topology: "subnet", DevType: "tap",
		PKICRLPath: "/pki/ca.crl", MaxClients: 50, TLSVersionMin: "1.2",
		TunMTU: 1400, Sndbuf: 1000, Rcvbuf: 2000, ServerIPv6: "fd00::/64",
		AuthUserPass: false,
		BridgeMode: true, BridgeGateway: "10.1.0.1", BridgeNetmask: "255.255.255.0",
		BridgePoolStart: "10.1.0.10", BridgePoolEnd: "10.1.0.50",
		TLSCipher: "TLS-ECDHE-RSA-WITH-AES-128-GCM-SHA256", TLSCiphersuites: "TLS_AES_256_GCM_SHA384",
		TLSGroups: "X25519", TLSCertProfile: "preferred",
		AuthUserPassVerify: "/bin/auth", ScriptSecurity: 2, UsernameAsCommonName: true,
		IfconfigIPv6: "fd00::1/64 fd00::2/64",
		FeatureSets:  []string{"mssfix"},
		Plugins:      []db.Plugin{{Path: "/p.so", Args: []string{"x"}}},
		EnvVars:      []db.EnvVar{{Name: "E", Value: "1"}},
	})
	require.NoError(t, err)

	got, err := store.GetInstance(ctx, "full")
	require.NoError(t, err)
	require.True(t, got.BridgeMode)
	require.Equal(t, "10.1.0.1", got.BridgeGateway)
	require.Equal(t, "X25519", got.TLSGroups)
	require.Equal(t, 50, got.MaxClients)
	require.Equal(t, "/pki/ca.crl", got.PKICRLPath)
	require.True(t, got.UsernameAsCommonName)
	require.Equal(t, 2, got.ScriptSecurity)
	require.Equal(t, "fd00::1/64 fd00::2/64", got.IfconfigIPv6)
	require.Len(t, got.Plugins, 1)

	// update patch
	got.TLSCipher = "CHANGED"
	got.BridgeMode = false
	updated, err := store.UpdateInstance(ctx, *got)
	require.NoError(t, err)
	require.Equal(t, "CHANGED", updated.TLSCipher)
	require.False(t, updated.BridgeMode)

	exp := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	cli, err := store.CreateClient(ctx, "full", db.Client{
		CommonName: "u1", Name: "User1", StaticIP: "10.1.0.10",
		IRoutes: []string{"10.20.0.0/16"}, PushDNS: []string{"8.8.8.8"},
		PushDomain: "lab", RedirectGateway: true, DisablePush: []string{"route"},
		BandwidthRxBps: 1000, BandwidthTxBps: 500, BandwidthTotalBps: 8_000_000,
		TrafficLimitBytes: 9999, ExpiresAt: exp,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"8.8.8.8"}, cli.PushDNS)
	require.True(t, cli.RedirectGateway)
	require.Equal(t, int64(9999), cli.TrafficLimitBytes)
	require.Equal(t, int64(8_000_000), cli.BandwidthTotalBps)
	require.True(t, cli.ExpiresAt.Equal(exp))

	again, err := store.GetClient(ctx, "full", "u1")
	require.NoError(t, err)
	require.Equal(t, []string{"10.20.0.0/16"}, again.IRoutes)
	require.Equal(t, []string{"route"}, again.DisablePush)
	require.Equal(t, "lab", again.PushDomain)
	require.Equal(t, int64(8_000_000), again.BandwidthTotalBps)
	require.True(t, again.ExpiresAt.Equal(exp))

	// Clear expiry via update (zero time → empty DB column).
	again.ExpiresAt = time.Time{}
	again.BandwidthTotalBps = 0
	updatedCli, err := store.UpdateClient(ctx, "full", "u1", *again)
	require.NoError(t, err)
	require.True(t, updatedCli.ExpiresAt.IsZero())
	require.Equal(t, int64(0), updatedCli.BandwidthTotalBps)
}

func TestCAWithCRLPathRoundTrip(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	ca, err := store.UpsertCA(ctx, db.CA{
		Name: "c", CommonName: "CN", CertPath: "/ca.crt", KeyPath: "/ca.key",
		CRLPath: "/ca.crl", CRLNumber: 3, SerialNext: 5,
	})
	require.NoError(t, err)
	require.Equal(t, "/ca.crl", ca.CRLPath)

	got, err := store.GetCA(ctx, "c")
	require.NoError(t, err)
	require.Equal(t, "/ca.crl", got.CRLPath)
	require.Equal(t, int64(3), got.CRLNumber)
}
