package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstanceCreateFieldsRoleFilter(t *testing.T) {
	defs := instanceCreateFields([]string{"default"})
	f := newForm("t", defs, map[string]string{"role": "server"})
	vals := f.Values()
	require.Equal(t, "server", vals["role"])
	// server-only
	require.Contains(t, fieldKeys(f), "network")
	require.Contains(t, fieldKeys(f), "public_endpoint")
	require.Contains(t, fieldKeys(f), "max_clients")
	require.Contains(t, fieldKeys(f), "tls_version_min")
	require.Contains(t, fieldKeys(f), "tls_groups")
	require.Contains(t, fieldKeys(f), "tls_cipher")
	require.Contains(t, fieldKeys(f), "bridge_mode")
	require.Contains(t, fieldKeys(f), "bridge_gateway")
	require.Contains(t, fieldKeys(f), "auth_user_pass_verify")
	require.Contains(t, fieldKeys(f), "script_security")
	require.Contains(t, fieldKeys(f), "server_ipv6")
	require.Contains(t, fieldKeys(f), "ifconfig_ipv6")
	require.Contains(t, fieldKeys(f), "tun_mtu")
	require.NotContains(t, fieldKeys(f), "remote")
	require.NotContains(t, fieldKeys(f), "profile")
	require.NotContains(t, fieldKeys(f), "auth_user_pass")
	require.NotContains(t, fieldKeys(f), "auth_user_pass_file")

	// switch role via rebuild
	f.rebuild("client", f.Values())
	keys := fieldKeys(f)
	require.Contains(t, keys, "remote")
	require.Contains(t, keys, "profile")
	require.Contains(t, keys, "auth_user_pass")
	require.Contains(t, keys, "auth_user_pass_file")
	require.Contains(t, keys, "ifconfig_ipv6")
	require.NotContains(t, keys, "network")
	require.NotContains(t, keys, "issue_cert")
	require.NotContains(t, keys, "max_clients")
	require.NotContains(t, keys, "bridge_mode")
}

func TestClientCreateDefaultsFields(t *testing.T) {
	defs := clientCreateFields([]string{"ovpn0"})
	f := newForm("c", defs, map[string]string{
		"instance": "ovpn0", "issue_cert": "y", "mint_link": "y",
	})
	v := f.Values()
	require.Equal(t, "ovpn0", v["instance"])
	require.Equal(t, "y", v["issue_cert"])
	require.Equal(t, "y", v["mint_link"])
	// tips present on CN
	var cn fieldDef
	for _, d := range defs {
		if d.Key == "cn" {
			cn = d
			break
		}
	}
	require.NotEmpty(t, cn.Tip)
	require.Contains(t, fieldKeys(f), "iroutes")
	require.Contains(t, fieldKeys(f), "push_dns")
	require.Contains(t, fieldKeys(f), "push_domain")
	require.Contains(t, fieldKeys(f), "redirect_gw")
	require.Contains(t, fieldKeys(f), "disable_push")
	require.Contains(t, fieldKeys(f), "bandwidth_rx")
	require.Contains(t, fieldKeys(f), "bandwidth_tx")
	require.Contains(t, fieldKeys(f), "traffic_limit")
}

func TestAdoptInstanceFields(t *testing.T) {
	f := newForm("adopt", adoptInstanceFields(), map[string]string{
		"conf_path": "/etc/openvpn/server.conf", "take_over": "y", "pid": "1234",
	})
	keys := fieldKeys(f)
	require.Contains(t, keys, "conf_path")
	require.Contains(t, keys, "name")
	require.Contains(t, keys, "public_endpoint")
	require.Contains(t, keys, "take_over")
	require.Contains(t, keys, "pid")
	v := f.Values()
	require.Equal(t, "/etc/openvpn/server.conf", v["conf_path"])
	require.Equal(t, "y", v["take_over"])
	require.Equal(t, "1234", v["pid"])
}

func TestCAAndIssueCertFields(t *testing.T) {
	f := newForm("ca", caCreateFields(), map[string]string{"name": "default", "common_name": "CA"})
	require.Contains(t, fieldKeys(f), "common_name")
	require.Contains(t, fieldKeys(f), "org")

	iss := newForm("iss", issueCertFields([]string{"default"}), map[string]string{
		"ca_name": "default", "kind": "client", "common_name": "alice",
	})
	v := iss.Values()
	require.Equal(t, "default", v["ca_name"])
	require.Equal(t, "client", v["kind"])
	require.Equal(t, "alice", v["common_name"])
}

func fieldKeys(f formModel) []string {
	var out []string
	for _, field := range f.fields {
		out = append(out, field.Key)
	}
	return out
}

func TestFieldVisible(t *testing.T) {
	require.True(t, fieldVisible(fieldDef{Key: "x"}, "server"))
	require.True(t, fieldVisible(fieldDef{Key: "x", Roles: []string{"server"}}, "server"))
	require.False(t, fieldVisible(fieldDef{Key: "x", Roles: []string{"server"}}, "client"))
}
