package confgen_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
)

func TestRenderServer(t *testing.T) {
	inst := db.Instance{
		Name:          "ovpn0",
		Role:          "server",
		DevType:       "tun",
		Proto:         "udp",
		Port:          1194,
		ServerNetwork: "10.8.0.0/24",
		Topology:      "subnet",
		AuthMode:      "pki",
		PKICaPath:     "/pki/ca.crt",
		PKICertPath:   "/pki/server.crt",
		PKIKeyPath:    "/pki/server.key",
		PKIDHPath:     "/pki/dh.pem",
		PushDNS:       []string{"1.1.1.1"},
	}
	paths := confgen.Paths{ConfDir: "/etc/openvpnd/instances", RuntimeDir: "/run/openvpnd", Name: "ovpn0"}
	res, err := confgen.RenderInstance(inst, paths, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "server 10.8.0.0 255.255.255.0")
	require.Contains(t, res.Content, "management ")
	require.Contains(t, res.Content, "client-config-dir")
	require.Contains(t, res.Content, `push "dhcp-option DNS 1.1.1.1"`)
	require.Contains(t, res.Content, "dh ") // path or "none"
	require.NotEmpty(t, res.Hash)
}

func TestRenderServerDHNone(t *testing.T) {
	inst := db.Instance{
		Name: "ovpn0", Role: "server", Port: 1194, ServerNetwork: "10.8.0.0/24",
		AuthMode: "pki", PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
	}
	res, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "ovpn0"}, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "dh none")
}

func TestRenderServerCRLAndKnobs(t *testing.T) {
	inst := db.Instance{
		Name: "ovpn0", Role: "server", Port: 1194, ServerNetwork: "10.8.0.0/24",
		AuthMode: "pki", PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
		PKICRLPath: "/pki/ca.crl",
		MaxClients: 64, TLSVersionMin: "1.2", TunMTU: 1400, Sndbuf: 393216, Rcvbuf: 393216,
		ServerIPv6: "fd00:1194:0:0::/64",
	}
	res, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "ovpn0"}, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "crl-verify /pki/ca.crl")
	require.Contains(t, res.Content, "max-clients 64")
	require.Contains(t, res.Content, "tls-version-min 1.2")
	require.Contains(t, res.Content, "tun-mtu 1400")
	require.Contains(t, res.Content, "sndbuf 393216")
	require.Contains(t, res.Content, "rcvbuf 393216")
	require.Contains(t, res.Content, "server-ipv6 fd00:1194:0:0::/64")
}

func TestRenderClientAuthUserPass(t *testing.T) {
	inst := db.Instance{
		Name: "home", Role: "client", AuthMode: "pki",
		Remotes:   []db.Remote{{Host: "vpn.example.com", Port: 1194}},
		PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/c.crt", PKIKeyPath: "/pki/c.key",
		AuthUserPass: true,
	}
	res, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "home"}, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "auth-user-pass")
	require.NotContains(t, res.Content, "auth-user-pass /")
}

func TestRenderClientAuthUserPassFile(t *testing.T) {
	inst := db.Instance{
		Name: "home", Role: "client", AuthMode: "pki",
		Remotes:          []db.Remote{{Host: "vpn.example.com", Port: 1194}},
		PKICaPath:        "/pki/ca.crt", PKICertPath: "/pki/c.crt", PKIKeyPath: "/pki/c.key",
		AuthUserPass:     true,
		AuthUserPassFile: "/etc/openvpn/pass.txt",
	}
	res, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "home"}, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "auth-user-pass /etc/openvpn/pass.txt")
}

func TestRenderServerBridge(t *testing.T) {
	inst := db.Instance{
		Name: "br0", Role: "server", Port: 1194, DevType: "tap",
		AuthMode: "pki", PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
		BridgeMode: true, BridgeGateway: "192.168.1.1", BridgeNetmask: "255.255.255.0",
		BridgePoolStart: "192.168.1.100", BridgePoolEnd: "192.168.1.200",
	}
	res, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "br0"}, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "server-bridge 192.168.1.1 255.255.255.0 192.168.1.100 192.168.1.200")
	require.NotContains(t, res.Content, "\nserver ")
}

func TestRenderServerBridgeMissingFields(t *testing.T) {
	inst := db.Instance{
		Name: "br0", Role: "server", Port: 1194, DevType: "tap",
		AuthMode: "pki", BridgeMode: true, BridgeGateway: "192.168.1.1",
	}
	_, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "br0"}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bridge_mode requires")
}

func TestRenderTLSAndAuthVerify(t *testing.T) {
	inst := db.Instance{
		Name: "ovpn0", Role: "server", Port: 1194, ServerNetwork: "10.8.0.0/24",
		AuthMode: "pki", PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
		TLSGroups: "X25519:P-256", TLSCipher: "TLS-ECDHE-ECDSA-WITH-AES-256-GCM-SHA384",
		TLSCiphersuites: "TLS_AES_256_GCM_SHA384", TLSCertProfile: "preferred",
		AuthUserPassVerify: "/usr/local/bin/auth.sh", UsernameAsCommonName: true,
		IfconfigIPv6: "fd00::1/64 fd00::2/64",
	}
	res, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "ovpn0"}, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "tls-groups X25519:P-256")
	require.Contains(t, res.Content, "tls-cipher TLS-ECDHE-ECDSA-WITH-AES-256-GCM-SHA384")
	require.Contains(t, res.Content, "tls-ciphersuites TLS_AES_256_GCM_SHA384")
	require.Contains(t, res.Content, "tls-cert-profile preferred")
	require.Contains(t, res.Content, "script-security 2")
	require.Contains(t, res.Content, "auth-user-pass-verify /usr/local/bin/auth.sh via-env")
	require.Contains(t, res.Content, "username-as-common-name")
	require.Contains(t, res.Content, "ifconfig-ipv6 fd00::1/64 fd00::2/64")
}

func TestRenderAuthVerifyViaFile(t *testing.T) {
	inst := db.Instance{
		Name: "ovpn0", Role: "server", Port: 1194, ServerNetwork: "10.8.0.0/24",
		AuthMode: "pki", PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
		AuthUserPassVerify: "/usr/local/bin/auth.sh", ScriptSecurity: 1,
	}
	res, err := confgen.RenderInstance(inst, confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "ovpn0"}, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "script-security 1")
	require.Contains(t, res.Content, "auth-user-pass-verify /usr/local/bin/auth.sh via-file")
}

func TestRenderClient(t *testing.T) {
	inst := db.Instance{
		Name:        "home",
		Role:        "client",
		DevType:     "tun",
		Proto:       "udp",
		AuthMode:    "pki",
		Remotes:     []db.Remote{{Host: "vpn.example.com", Port: 1194}},
		PKICaPath:   "/pki/ca.crt",
		PKICertPath: "/pki/client.crt",
		PKIKeyPath:  "/pki/client.key",
	}
	paths := confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "home"}
	res, err := confgen.RenderInstance(inst, paths, nil)
	require.NoError(t, err)
	require.Contains(t, res.Content, "client")
	require.Contains(t, res.Content, "remote vpn.example.com 1194")
	require.True(t, strings.HasPrefix(res.Content, "# Generated by openvpnd"))
}

func TestRenderCCD(t *testing.T) {
	body := confgen.RenderCCD(db.Client{CommonName: "alice", StaticIP: "10.8.0.5"}, "10.8.0.0/24")
	require.Contains(t, body, "ifconfig-push 10.8.0.5")
	disabled := confgen.RenderCCD(db.Client{CommonName: "bob", Suspended: true}, "10.8.0.0/24")
	require.Contains(t, disabled, "disable")
}

func TestRenderShaperMode(t *testing.T) {
	inst := db.Instance{
		Name: "ovpn0", Role: "server", Port: 1194, ServerNetwork: "10.8.0.0/24",
		AuthMode: "pki", PKICaPath: "/pki/ca.crt", PKICertPath: "/pki/s.crt", PKIKeyPath: "/pki/s.key",
	}
	paths := confgen.Paths{ConfDir: "/tmp", RuntimeDir: "/tmp", Name: "ovpn0"}
	clients := []db.Client{
		{CommonName: "alice", BandwidthRxBps: 8_000_000, BandwidthTxBps: 1_000_000}, // max 8 Mbit → 1_000_000 B/s
		{CommonName: "bob", BandwidthRxBps: 800_000},
	}
	res, err := confgen.RenderInstanceOpts(inst, paths, clients, confgen.RenderOptions{
		BandwidthEnforcement: "shaper",
	})
	require.NoError(t, err)
	require.Contains(t, res.Content, "bandwidth_enforcement=shaper")
	require.Contains(t, res.Content, "shaper 1000000")

	// off → no shaper directive
	resOff, err := confgen.RenderInstanceOpts(inst, paths, clients, confgen.RenderOptions{
		BandwidthEnforcement: "off",
	})
	require.NoError(t, err)
	require.NotContains(t, resOff.Content, "shaper ")
}

func TestRenderCCDIroutes(t *testing.T) {
	body := confgen.RenderCCD(db.Client{
		CommonName: "branch",
		StaticIP:   "10.8.0.10",
		PushRoutes: []string{"10.9.0.0/24"},
		IRoutes:    []string{"192.168.50.0/24", "10.20.0.0/16"},
	}, "10.8.0.0/24")
	require.Contains(t, body, "ifconfig-push 10.8.0.10")
	require.Contains(t, body, `push "route 10.9.0.0 255.255.255.0"`)
	require.Contains(t, body, "iroute 192.168.50.0 255.255.255.0")
	require.Contains(t, body, "iroute 10.20.0.0 255.255.0.0")
	// suspended still short-circuits before iroutes
	off := confgen.RenderCCD(db.Client{
		CommonName: "x", Suspended: true, IRoutes: []string{"10.0.0.0/8"},
	}, "10.8.0.0/24")
	require.Contains(t, off, "disable")
	require.NotContains(t, off, "iroute")
}

func TestRenderCCDPushOverrides(t *testing.T) {
	body := confgen.RenderCCD(db.Client{
		CommonName: "alice", StaticIP: "10.8.0.5",
		PushDNS: []string{"1.1.1.1"}, PushDomain: "corp.lan",
		RedirectGateway: true,
		DisablePush:     []string{"redirect-gateway", "dhcp-option DNS"},
	}, "10.8.0.0/24")
	require.Contains(t, body, `push "dhcp-option DNS 1.1.1.1"`)
	require.Contains(t, body, `push "dhcp-option DOMAIN corp.lan"`)
	require.Contains(t, body, `push "redirect-gateway def1 bypass-dhcp"`)
	require.Contains(t, body, "push-remove redirect-gateway")
	require.Contains(t, body, "push-remove dhcp-option DNS")
}
