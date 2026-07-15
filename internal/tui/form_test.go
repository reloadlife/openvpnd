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
	require.NotContains(t, fieldKeys(f), "remote")
	require.NotContains(t, fieldKeys(f), "profile")

	// switch role via rebuild
	f.rebuild("client", f.Values())
	keys := fieldKeys(f)
	require.Contains(t, keys, "remote")
	require.Contains(t, keys, "profile")
	require.NotContains(t, keys, "network")
	require.NotContains(t, keys, "issue_cert")
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
