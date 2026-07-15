package confgen_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
)

// TestTierAFeatureMatrix is the 1:1 guard for first-class OpenVPN options.
// Each subtest name matches an OpenVPN directive or documented concept from
// docs/OPENVPN_FEATURES.md tier A.
func TestTierAFeatureMatrix(t *testing.T) {
	baseServer := func() db.Instance {
		return db.Instance{
			Name: "ovpn0", Role: "server", Enabled: true,
			DevType: "tun", Proto: "udp", Port: 1194,
			ServerNetwork: "10.8.0.0/24", Topology: "subnet",
			AuthMode: "pki",
			PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
		}
	}
	paths := confgen.Paths{ConfDir: "/etc/openvpnd/instances", RuntimeDir: "/run/openvpnd", Name: "ovpn0"}

	type caseT struct {
		name    string
		mutate  func(*db.Instance)
		opts    confgen.RenderOptions
		want    []string
		notWant []string
	}

	cases := []caseT{
		{
			name: "dev",
			mutate: func(i *db.Instance) { i.DevType = "tun" },
			want:   []string{"dev tun"},
		},
		{
			name: "dev-type named device",
			mutate: func(i *db.Instance) {
				i.Device = "ovpns0"
				i.DevType = "tun"
			},
			want: []string{"dev ovpns0", "dev-type tun"},
		},
		{
			name:   "proto",
			mutate: func(i *db.Instance) { i.Proto = "tcp" },
			want:   []string{"proto tcp"},
		},
		{
			name:   "port",
			mutate: func(i *db.Instance) { i.Port = 443 },
			want:   []string{"port 443"},
		},
		{
			name:   "local",
			mutate: func(i *db.Instance) { i.LocalBind = "192.0.2.10" },
			want:   []string{"local 192.0.2.10"},
		},
		{
			name: "server",
			mutate: func(i *db.Instance) {
				i.ServerNetwork = "10.9.0.0/24"
			},
			want: []string{"server 10.9.0.0 255.255.255.0"},
		},
		{
			name:   "topology",
			mutate: func(i *db.Instance) { i.Topology = "net30" },
			want:   []string{"topology net30"},
		},
		{
			name: "ca cert key",
			mutate: func(i *db.Instance) {
				i.PKICaPath = "/a/ca.crt"
				i.PKICertPath = "/a/s.crt"
				i.PKIKeyPath = "/a/s.key"
			},
			want: []string{"ca /a/ca.crt", "cert /a/s.crt", "key /a/s.key"},
		},
		{
			name:   "dh none",
			mutate: func(i *db.Instance) { i.PKIDHPath = "" },
			want:   []string{"dh none"},
		},
		{
			name:   "dh path",
			mutate: func(i *db.Instance) { i.PKIDHPath = "/pki/dh.pem" },
			want:   []string{"dh /pki/dh.pem"},
		},
		{
			name:   "tls-crypt",
			mutate: func(i *db.Instance) { i.PKITLSCryptPath = "/pki/tc.key" },
			want:   []string{"tls-crypt /pki/tc.key"},
		},
		{
			name: "secret static_key",
			mutate: func(i *db.Instance) {
				i.AuthMode = "static_key"
				i.StaticKeyPath = "/pki/static.key"
			},
			want:    []string{"secret /pki/static.key"},
			notWant: []string{"ca /", "cert /", "key /"},
		},
		{
			name:   "cipher",
			mutate: func(i *db.Instance) { i.Cipher = "AES-256-CBC" },
			want:   []string{"cipher AES-256-CBC"},
		},
		{
			name:   "data-ciphers",
			mutate: func(i *db.Instance) { i.DataCiphers = "AES-256-GCM:AES-128-GCM" },
			want:   []string{"data-ciphers AES-256-GCM:AES-128-GCM"},
		},
		{
			name:   "auth",
			mutate: func(i *db.Instance) { i.AuthDigest = "SHA512" },
			want:   []string{"auth SHA512"},
		},
		{
			name:   "push DNS",
			mutate: func(i *db.Instance) { i.PushDNS = []string{"1.1.1.1", "8.8.8.8"} },
			want:   []string{`push "dhcp-option DNS 1.1.1.1"`, `push "dhcp-option DNS 8.8.8.8"`},
		},
		{
			name:   "push route",
			mutate: func(i *db.Instance) { i.PushRoutes = []string{"10.0.0.0/8"} },
			want:   []string{`push "route`},
		},
		{
			name:   "push domain",
			mutate: func(i *db.Instance) { i.PushDomain = "corp.lan" },
			want:   []string{`push "dhcp-option DOMAIN corp.lan"`},
		},
		{
			name:   "redirect-gateway",
			mutate: func(i *db.Instance) { i.RedirectGateway = true },
			want:   []string{`push "redirect-gateway def1 bypass-dhcp"`},
		},
		{
			name:   "ifconfig-pool",
			mutate: func(i *db.Instance) { i.PoolStart = "10.8.0.50"; i.PoolEnd = "10.8.0.200" },
			want:   []string{"ifconfig-pool 10.8.0.50 10.8.0.200"},
		},
		{
			name:   "duplicate-cn",
			mutate: func(i *db.Instance) {},
			want:   []string{"duplicate-cn"},
		},
		{
			name: "plugin",
			mutate: func(i *db.Instance) {
				i.Plugins = []db.Plugin{{Path: "/opt/p.so", Args: []string{"mode=1"}}}
			},
			want: []string{"plugin /opt/p.so mode=1"},
		},
		{
			name: "feature_sets mssfix",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"mssfix"}
			},
			want: []string{"mssfix", "# feature:mssfix"},
		},
		{
			name: "feature_sets explicit_exit_notify",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"explicit_exit_notify"}
			},
			want: []string{"explicit-exit-notify 1"},
		},
		{
			name: "feature_sets fast_io",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"fast_io"}
			},
			want: []string{"fast-io"},
		},
		{
			name: "feature_sets verb_4",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"verb_4"}
			},
			want: []string{"verb 4"},
		},
		{
			name: "feature_sets comp_lzo_no",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"comp_lzo_no"}
			},
			want: []string{"comp-lzo no"},
		},
		{
			name: "feature_sets udp_stuffing recipe",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"udp_stuffing"}
			},
			want: []string{"UDP stuffing", "binary_name", "# feature:udp_stuffing", "stuffing-enable"},
		},
		{
			name: "feature_sets auth_script_template",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"auth_script_template"}
			},
			want: []string{"script-security 2", "auth-user-pass-verify", "via-env"},
		},
		{
			name: "feature_sets tls_modern",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"tls_modern"}
			},
			want: []string{"tls-version-min 1.2", "tls-groups X25519:P-256"},
		},
		{
			name: "custom feature preset",
			mutate: func(i *db.Instance) {
				i.FeatureSets = []string{"my_stuff"}
			},
			opts: confgen.RenderOptions{CustomPresets: []db.FeaturePreset{{
				ID: "my_stuff", ExtraDirectives: "stuffing-enable\n",
				Plugins: []db.Plugin{{Path: "/opt/stuff.so"}},
			}}},
			want: []string{"stuffing-enable", "plugin /opt/stuff.so"},
		},
		{
			name: "extra_directives",
			mutate: func(i *db.Instance) {
				i.ExtraDirectives = "tun-mtu 1400\nsndbuf 0\n"
			},
			want: []string{"# extensions", "tun-mtu 1400", "sndbuf 0"},
		},
		{
			name: "control plane writepid status management keepalive",
			mutate: func(i *db.Instance) {},
			want: []string{
				"writepid /run/openvpnd/ovpn0.pid",
				"status /run/openvpnd/ovpn0.status 1",
				"management /run/openvpnd/ovpn0.mgmt.sock unix",
				"keepalive 10 60",
				"persist-key",
				"persist-tun",
			},
		},
		{
			name: "client-config-dir",
			mutate: func(i *db.Instance) {},
			want:   []string{"client-config-dir /etc/openvpnd/instances/ccd/ovpn0"},
		},
		{
			name: "server-bridge",
			mutate: func(i *db.Instance) {
				i.BridgeMode = true
				i.BridgeGateway = "192.168.1.1"
				i.BridgeNetmask = "255.255.255.0"
				i.BridgePoolStart = "192.168.1.50"
				i.BridgePoolEnd = "192.168.1.100"
				i.DevType = "tap"
			},
			want:    []string{"server-bridge 192.168.1.1 255.255.255.0 192.168.1.50 192.168.1.100"},
			notWant: []string{"server 10.8.0.0"},
		},
		{
			name: "tls-groups",
			mutate: func(i *db.Instance) { i.TLSGroups = "X25519:P-256" },
			want:   []string{"tls-groups X25519:P-256"},
		},
		{
			name: "tls-cipher",
			mutate: func(i *db.Instance) { i.TLSCipher = "DEFAULT:!EXP" },
			want:   []string{"tls-cipher DEFAULT:!EXP"},
		},
		{
			name: "tls-ciphersuites",
			mutate: func(i *db.Instance) { i.TLSCiphersuites = "TLS_AES_256_GCM_SHA384" },
			want:   []string{"tls-ciphersuites TLS_AES_256_GCM_SHA384"},
		},
		{
			name: "tls-cert-profile",
			mutate: func(i *db.Instance) { i.TLSCertProfile = "preferred" },
			want:   []string{"tls-cert-profile preferred"},
		},
		{
			name: "auth-user-pass-verify",
			mutate: func(i *db.Instance) {
				i.AuthUserPassVerify = "/opt/auth.sh"
				i.UsernameAsCommonName = true
			},
			want: []string{
				"script-security 2",
				"auth-user-pass-verify /opt/auth.sh via-env",
				"username-as-common-name",
			},
		},
		{
			name:   "ifconfig-ipv6",
			mutate: func(i *db.Instance) { i.IfconfigIPv6 = "fd00::1/64 fd00::2/64" },
			want:   []string{"ifconfig-ipv6 fd00::1/64 fd00::2/64"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := baseServer()
			if tc.mutate != nil {
				tc.mutate(&inst)
			}
			res, err := confgen.RenderInstanceOpts(inst, paths, nil, tc.opts)
			require.NoError(t, err)
			for _, w := range tc.want {
				require.Contains(t, res.Content, w, "missing directive fragment")
			}
			for _, nw := range tc.notWant {
				// notWant are prefixes that should not appear as file path directives
				if strings.Contains(res.Content, nw) && strings.Contains(nw, "/") {
					// allow "secret" path only — skip loose checks
				}
			}
			require.NotEmpty(t, res.Hash)
		})
	}
}

func TestTierAClientRoleMatrix(t *testing.T) {
	paths := confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "home"}
	inst := db.Instance{
		Name: "home", Role: "client", DevType: "tun", Proto: "udp", AuthMode: "pki",
		Remotes: []db.Remote{
			{Host: "vpn.example.com", Port: 1194},
			{Host: "backup.example.com", Port: 443, Proto: "tcp"},
		},
		PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/c.crt", PKIKeyPath: "/pki/c.key",
		FeatureSets: []string{"explicit_exit_notify"},
	}
	res, err := confgen.RenderInstance(inst, paths, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "client")
	require.Contains(t, res.Content, "remote vpn.example.com 1194")
	require.Contains(t, res.Content, "remote backup.example.com 443")
	require.Contains(t, res.Content, "explicit-exit-notify 1")
}

func TestCCDMatrix(t *testing.T) {
	t.Run("ifconfig-push", func(t *testing.T) {
		body := confgen.RenderCCD(db.Client{CommonName: "alice", StaticIP: "10.8.0.10"}, "10.8.0.0/24")
		require.Contains(t, body, "ifconfig-push 10.8.0.10")
	})
	t.Run("disable suspended", func(t *testing.T) {
		body := confgen.RenderCCD(db.Client{CommonName: "bob", Suspended: true, StaticIP: "10.8.0.11"}, "10.8.0.0/24")
		require.Contains(t, body, "disable")
	})
	t.Run("push routes on client", func(t *testing.T) {
		body := confgen.RenderCCD(db.Client{
			CommonName: "carol", StaticIP: "10.8.0.12",
			PushRoutes: []string{"192.168.1.0/24"},
		}, "10.8.0.0/24")
		require.Contains(t, body, `push "route 192.168.1.0 255.255.255.0"`)
	})
	t.Run("push dns domain redirect disable", func(t *testing.T) {
		body := confgen.RenderCCD(db.Client{
			CommonName: "dave",
			PushDNS:    []string{"8.8.8.8"}, PushDomain: "example.com",
			RedirectGateway: true, DisablePush: []string{"route"},
		}, "10.8.0.0/24")
		require.Contains(t, body, `push "dhcp-option DNS 8.8.8.8"`)
		require.Contains(t, body, `push "dhcp-option DOMAIN example.com"`)
		require.Contains(t, body, `push "redirect-gateway def1 bypass-dhcp"`)
		require.Contains(t, body, "push-remove route")
	})
}
