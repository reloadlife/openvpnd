package confgen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/features"
)

// TestRoadmapDirectiveMatrix is 1:1 emission for v0.2 manageability fields.
func TestRoadmapDirectiveMatrix(t *testing.T) {
	paths := confgen.Paths{ConfDir: "/e", RuntimeDir: "/r", Name: "x"}

	cases := []struct {
		name string
		inst db.Instance
		want []string
	}{
		{
			name: "tls control channel",
			inst: db.Instance{
				Name: "x", Role: "server", Port: 1194, ServerNetwork: "10.0.0.0/24",
				AuthMode: "pki", PKICaPath: "/c", PKICertPath: "/s", PKIKeyPath: "/k",
				TLSCipher: "TLS-ECDHE-ECDSA-WITH-AES-256-GCM-SHA384",
				TLSCiphersuites: "TLS_AES_256_GCM_SHA384",
				TLSGroups: "X25519:P-256", TLSCertProfile: "preferred",
			},
			want: []string{
				"tls-cipher TLS-ECDHE-ECDSA-WITH-AES-256-GCM-SHA384",
				"tls-ciphersuites TLS_AES_256_GCM_SHA384",
				"tls-groups X25519:P-256",
				"tls-cert-profile preferred",
			},
		},
		{
			name: "ifconfig-ipv6",
			inst: db.Instance{
				Name: "x", Role: "server", Port: 1194, ServerNetwork: "10.0.0.0/24",
				AuthMode: "pki", PKICaPath: "/c", PKICertPath: "/s", PKIKeyPath: "/k",
				IfconfigIPv6: "fd00::1/64 fd00::2/64",
			},
			want: []string{"ifconfig-ipv6 fd00::1/64 fd00::2/64"},
		},
		{
			name: "crl-verify",
			inst: db.Instance{
				Name: "x", Role: "server", Port: 1194, ServerNetwork: "10.0.0.0/24",
				AuthMode: "pki", PKICaPath: "/c", PKICertPath: "/s", PKIKeyPath: "/k",
				PKICRLPath: "/c.crl",
			},
			want: []string{"crl-verify /c.crl"},
		},
		{
			name: "auth verify via-env",
			inst: db.Instance{
				Name: "x", Role: "server", Port: 1194, ServerNetwork: "10.0.0.0/24",
				AuthMode: "pki", PKICaPath: "/c", PKICertPath: "/s", PKIKeyPath: "/k",
				AuthUserPassVerify: "/auth.sh", ScriptSecurity: 2, UsernameAsCommonName: true,
			},
			want: []string{"script-security 2", "auth-user-pass-verify /auth.sh via-env", "username-as-common-name"},
		},
		{
			name: "bridge",
			inst: db.Instance{
				Name: "x", Role: "server", Port: 1194, DevType: "tap",
				AuthMode: "pki", PKICaPath: "/c", PKICertPath: "/s", PKIKeyPath: "/k",
				BridgeMode: true, BridgeGateway: "192.168.1.1", BridgeNetmask: "255.255.255.0",
				BridgePoolStart: "192.168.1.100", BridgePoolEnd: "192.168.1.200",
			},
			want: []string{"server-bridge 192.168.1.1 255.255.255.0 192.168.1.100 192.168.1.200", "dev tap"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := confgen.RenderInstance(tc.inst, paths, nil)
			require.NoError(t, err)
			for _, w := range tc.want {
				require.Contains(t, res.Content, w)
			}
		})
	}
}

func TestBuiltinPresetsEmitInConf(t *testing.T) {
	paths := confgen.Paths{ConfDir: "/e", RuntimeDir: "/r", Name: "x"}
	base := db.Instance{
		Name: "x", Role: "server", Port: 1194, ServerNetwork: "10.0.0.0/24",
		AuthMode: "pki", PKICaPath: "/c", PKICertPath: "/s", PKIKeyPath: "/k",
	}
	for _, p := range features.Builtin {
		t.Run(p.ID, func(t *testing.T) {
			inst := base
			inst.FeatureSets = []string{p.ID}
			res, err := confgen.RenderInstance(inst, paths, nil)
			require.NoError(t, err)
			if p.ExtraDirectives != "" {
				require.Contains(t, res.Content, "# feature:"+p.ID)
			}
		})
	}
}
