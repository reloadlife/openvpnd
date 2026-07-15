package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func TestFormatSystemInfoLine(t *testing.T) {
	require.Equal(t, "system ok", formatSystemInfoLine(pkgapi.SystemInfo{}))

	total, up := 2, 1
	line := formatSystemInfoLine(pkgapi.SystemInfo{
		Version:        "0.2.0",
		Status:         "ok",
		Hostname:       "vpn1",
		Backend:        "host",
		Production:     true,
		BandwidthMode:  "tc",
		InstancesTotal: &total,
		InstancesUp:    &up,
		Ready:          pkgapi.SystemReady{DB: true, StateDB: true},
	})
	require.Contains(t, line, "v0.2.0")
	require.Contains(t, line, "ok")
	require.Contains(t, line, "vpn1")
	require.Contains(t, line, "host")
	require.Contains(t, line, "production")
	require.Contains(t, line, "bw=tc")
	require.Contains(t, line, "inst 1/2 up")

	line2 := formatSystemInfoLine(pkgapi.SystemInfo{
		Version: "1.0.0",
		Ready:   pkgapi.SystemReady{DB: true},
	})
	require.Equal(t, "v1.0.0 · ready", line2)
}
