package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func TestFilterUnmanaged(t *testing.T) {
	cands := []pkgapi.OpenVPNCandidate{
		{PID: 1, ConfPath: "/opt/homelab/germany.ovpn", Binary: "openvpn-linux"},
		{PID: 2, ConfPath: "/opt/homelab/turkey.ovpn", Binary: "openvpn-linux"},
		{PID: 3, ConfPath: "/opt/homelab/new.ovpn", Binary: "openvpn-linux"},
		{PID: 99, ConfPath: "/managed/same.ovpn", Binary: "openvpn"},
	}
	insts := []pkgapi.Instance{
		{Name: "germany", ExtraDirectives: "# adopted from /opt/homelab/germany.ovpn\n"},
		{Name: "turkey"}, // basename match
		{Name: "x", PID: 99, Up: true},
	}
	out := filterUnmanaged(cands, insts)
	require.Len(t, out, 1)
	require.Equal(t, "/opt/homelab/new.ovpn", out[0].ConfPath)
	require.Equal(t, 3, out[0].PID)
}
