package bandwidth_test

// device-plan tests live next to peer Plan tests below.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/bandwidth"
)

func TestNormalizeMode(t *testing.T) {
	require.Equal(t, bandwidth.ModeOff, bandwidth.NormalizeMode(""))
	require.Equal(t, bandwidth.ModeOff, bandwidth.NormalizeMode("off"))
	require.Equal(t, bandwidth.ModeTC, bandwidth.NormalizeMode("tc"))
	require.Equal(t, bandwidth.ModeTC, bandwidth.NormalizeMode("HTB"))
	require.Equal(t, bandwidth.ModeShaper, bandwidth.NormalizeMode("shaper"))
	require.Equal(t, bandwidth.ModeLog, bandwidth.NormalizeMode("log"))
	require.Equal(t, bandwidth.ModeOff, bandwidth.NormalizeMode("nft")) // unsupported → off
}

func TestPlanEgressAndIngress(t *testing.T) {
	rules := bandwidth.Plan(bandwidth.PlanInput{
		Device:   "tun0",
		StaticIP: "10.8.0.5",
		RxBps:    1_000_000, // 1 Mbit download
		TxBps:    500_000,   // 0.5 Mbit upload
		ClassID:  12,
	})
	require.NotEmpty(t, rules)

	joined := ""
	for _, r := range rules {
		joined += r.String() + "\n"
	}

	require.Contains(t, joined, "qdisc replace dev tun0 root handle 1: htb")
	require.Contains(t, joined, "class replace dev tun0 parent 1:1 classid 1:12 htb rate 1000000bit")
	require.Contains(t, joined, "match ip dst 10.8.0.5/32")
	require.Contains(t, joined, "qdisc replace dev tun0 handle ffff: ingress")
	require.Contains(t, joined, "match ip src 10.8.0.5/32")
	require.Contains(t, joined, "police rate 500000bit")

	apply := bandwidth.ApplyRules(rules)
	remove := bandwidth.RemoveRules(rules)
	require.NotEmpty(t, apply)
	require.NotEmpty(t, remove)
	for _, r := range apply {
		require.False(t, r.Undo)
	}
	for _, r := range remove {
		require.True(t, r.Undo)
	}
	// undo filters before classes for egress
	require.Contains(t, remove[0].String(), "filter del")
}

func TestPlanSkipsInvalid(t *testing.T) {
	require.Nil(t, bandwidth.Plan(bandwidth.PlanInput{Device: "", StaticIP: "10.8.0.2", RxBps: 100}))
	require.Nil(t, bandwidth.Plan(bandwidth.PlanInput{Device: "tun0", StaticIP: "not-an-ip", RxBps: 100}))
	require.Nil(t, bandwidth.Plan(bandwidth.PlanInput{Device: "tun0", StaticIP: "10.8.0.2", RxBps: 0, TxBps: 0}))
}

func TestPlanRxOnly(t *testing.T) {
	rules := bandwidth.Plan(bandwidth.PlanInput{
		Device: "ovpns0", StaticIP: "10.9.0.2", RxBps: 8000, ClassID: 10,
	})
	joined := ""
	for _, r := range bandwidth.ApplyRules(rules) {
		joined += r.String() + "\n"
	}
	require.Contains(t, joined, "dst 10.9.0.2/32")
	require.NotContains(t, joined, "ingress")
}

func TestMaxShaperBytesPerSec(t *testing.T) {
	require.Equal(t, int64(0), bandwidth.MaxShaperBytesPerSec(nil))
	n := bandwidth.MaxShaperBytesPerSec([]bandwidth.ClientLimit{
		{RxBps: 8000, TxBps: 16000}, // max 16000 bit/s → 2000 B/s
		{RxBps: 800, TxBps: 0},
	})
	require.Equal(t, int64(2000), n)
	// tiny bitrate still ≥ 1 byte/s
	require.Equal(t, int64(1), bandwidth.MaxShaperBytesPerSec([]bandwidth.ClientLimit{{RxBps: 4}}))
}

func TestPlanDeviceWholeTunnel(t *testing.T) {
	// Client-role: shape entire TUN without per-IP filters.
	rules := bandwidth.PlanDevice(bandwidth.DevicePlanInput{
		Device: "zur0",
		RxBps:  10_000_000,
		TxBps:  5_000_000,
	})
	require.NotEmpty(t, rules)
	joined := ""
	for _, r := range bandwidth.ApplyRules(rules) {
		joined += r.String() + "\n"
	}
	require.Contains(t, joined, "qdisc replace dev zur0 root handle 1: htb")
	require.Contains(t, joined, "classid 1:10 htb rate 5000000bit")
	require.Contains(t, joined, "police rate 10000000bit")
	require.NotContains(t, joined, "match ip dst")
	require.NotContains(t, joined, "match ip src")
	require.Nil(t, bandwidth.PlanDevice(bandwidth.DevicePlanInput{Device: "zur0"}))
	require.Equal(t, int64(1250000), bandwidth.ShaperBytesPerSec(10_000_000, 5_000_000))
}

func TestSyncDeviceLogMode(t *testing.T) {
	e := bandwidth.NewEnforcer("log", slog.Default())
	require.NoError(t, e.SyncDevice(context.Background(), "zur0", "zur0", 1_000_000, 500_000))
	require.NoError(t, e.ClearDevice(context.Background(), "zur0"))
}

func TestExceedsTrafficLimit(t *testing.T) {
	require.False(t, bandwidth.ExceedsTrafficLimit(100, 100, 0))
	require.False(t, bandwidth.ExceedsTrafficLimit(50, 49, 100))
	require.True(t, bandwidth.ExceedsTrafficLimit(50, 50, 100))
	require.True(t, bandwidth.ExceedsTrafficLimit(200, 0, 100))
	require.True(t, bandwidth.ExceedsTrafficLimit(-1, 100, 100))
}

type fakeRunner struct {
	cmds    []string
	missing map[string]bool
	fail    map[string]error
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if f.missing != nil && f.missing[file] {
		return "", execErr("not found")
	}
	return "/sbin/" + file, nil
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	_ = ctx
	line := name + " " + strings.Join(args, " ")
	f.cmds = append(f.cmds, line)
	if f.fail != nil {
		for k, err := range f.fail {
			if strings.Contains(line, k) {
				return err
			}
		}
	}
	return nil
}

type execErr string

func (e execErr) Error() string { return string(e) }

func TestEnforcerSyncLogMode(t *testing.T) {
	e := bandwidth.NewEnforcer("log", slog.Default())
	fr := &fakeRunner{}
	e.SetRunner(fr)

	err := e.Sync(context.Background(), "ovpn0", "tun0", []bandwidth.ClientLimit{
		{CommonName: "alice", StaticIP: "10.8.0.2", RxBps: 1_000_000, TxBps: 0},
		{CommonName: "bob", StaticIP: "10.8.0.3", RxBps: 0, TxBps: 0}, // skip
		{CommonName: "carol", StaticIP: "", RxBps: 100},                // skip
	})
	require.NoError(t, err)
	require.Equal(t, 1, e.AppliedCount("ovpn0"))
	// log mode does not exec
	require.Empty(t, fr.cmds)

	// remove alice
	require.NoError(t, e.Sync(context.Background(), "ovpn0", "tun0", nil))
	require.Equal(t, 0, e.AppliedCount("ovpn0"))
}

func TestEnforcerSyncTCMode(t *testing.T) {
	e := bandwidth.NewEnforcer("tc", slog.Default())
	fr := &fakeRunner{}
	e.SetRunner(fr)

	require.NoError(t, e.Sync(context.Background(), "ovpn0", "tun0", []bandwidth.ClientLimit{
		{CommonName: "alice", StaticIP: "10.8.0.2", RxBps: 100000, TxBps: 50000},
	}))
	require.Equal(t, 1, e.AppliedCount("ovpn0"))
	require.NotEmpty(t, fr.cmds)
	joined := strings.Join(fr.cmds, "\n")
	require.Contains(t, joined, "dst 10.8.0.2/32")
	require.Contains(t, joined, "src 10.8.0.2/32")

	// re-sync same limits → no extra applies (idempotent)
	n := len(fr.cmds)
	require.NoError(t, e.Sync(context.Background(), "ovpn0", "tun0", []bandwidth.ClientLimit{
		{CommonName: "alice", StaticIP: "10.8.0.2", RxBps: 100000, TxBps: 50000},
	}))
	require.Equal(t, n, len(fr.cmds))

	// clear instance
	require.NoError(t, e.ClearInstance(context.Background(), "ovpn0"))
	require.Equal(t, 0, e.AppliedCount("ovpn0"))
}

func TestEnforcerMissingBinaryNoop(t *testing.T) {
	e := bandwidth.NewEnforcer("tc", slog.Default())
	fr := &fakeRunner{missing: map[string]bool{"tc": true}}
	e.SetRunner(fr)
	// Soft no-op — no hard error when tc absent
	require.NoError(t, e.Apply(context.Background(), "ovpn0", "tun0", bandwidth.ClientLimit{
		CommonName: "alice", StaticIP: "10.8.0.2", RxBps: 1000,
	}))
}

func TestEnforcerOffAndShaperNoop(t *testing.T) {
	for _, mode := range []string{"off", "shaper"} {
		e := bandwidth.NewEnforcer(mode, slog.Default())
		fr := &fakeRunner{}
		e.SetRunner(fr)
		require.NoError(t, e.Sync(context.Background(), "ovpn0", "tun0", []bandwidth.ClientLimit{
			{CommonName: "alice", StaticIP: "10.8.0.2", RxBps: 1000},
		}))
		require.Empty(t, fr.cmds)
		require.Equal(t, 0, e.AppliedCount("ovpn0"))
	}
}

func TestNeedsShaping(t *testing.T) {
	require.True(t, bandwidth.NeedsShaping("10.8.0.2", 1, 0))
	require.False(t, bandwidth.NeedsShaping("", 1, 1))
	require.False(t, bandwidth.NeedsShaping("10.8.0.2", 0, 0))
}
