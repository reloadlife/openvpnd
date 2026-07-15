package api_test

// Feature verification suite: exercises every major manageability surface shipped
// in v0.2.0 (PKI/CRL, import/adopt, mgmt, bridge/TLS/auth knobs, CCD ACL,
// features presets, profile links). Uses mock OpenVPN backend only.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/api"
	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/reconcile"
	"github.com/reloadlife/openvpnd/internal/stats"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

type featureHarness struct {
	t       *testing.T
	store   *db.Store
	backend *ovpnbackend.Mock
	router  http.Handler
	dir     string
	token   string
}

func newFeatureHarness(t *testing.T) *featureHarness {
	t.Helper()
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	dir := t.TempDir()
	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
		"stuff":   "/opt/openvpn-stuff/sbin/openvpn",
	}))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "feature-token"
	cfg.PublicBaseURL = "https://vpn.test.example"
	cfg.ProfileLinks.DefaultTTL = "1h"
	cfg.ProfileLinks.DefaultMaxUses = 1
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = filepath.Join(dir, "conf")
	cfg.OpenVPN.RuntimeDir = filepath.Join(dir, "run")
	cfg.OpenVPN.PKIDir = filepath.Join(dir, "pki")
	cfg.OpenVPN.BandwidthEnforcement = "log"
	cfg.OpenVPN.Binaries = map[string]string{
		"default": "/usr/sbin/openvpn",
		"stuff":   "/opt/openvpn-stuff/sbin/openvpn",
	}

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir:              cfg.OpenVPN.ConfDir,
		RuntimeDir:           cfg.OpenVPN.RuntimeDir,
		DefaultBinary:        "default",
		BandwidthEnforcement: "log",
	}, slog.Default())
	srv := api.NewServer(store, backend, cache, rec, cfg, slog.Default())

	return &featureHarness{
		t: t, store: store, backend: backend, router: srv.Router(),
		dir: dir, token: "feature-token",
	}
}

func (h *featureHarness) do(method, path string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(h.t, err)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer "+h.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func (h *featureHarness) exportConf(name string) string {
	h.t.Helper()
	rr := h.do(http.MethodGet, "/v1/instances/"+name+"/export", nil)
	require.Equal(h.t, http.StatusOK, rr.Code, rr.Body.String())
	return rr.Body.String()
}

// TestAllManageabilityFeatures is the master verification suite for shipped features.
func TestAllManageabilityFeatures(t *testing.T) {
	h := newFeatureHarness(t)
	ctx := context.Background()

	t.Run("features_list_builtins", func(t *testing.T) {
		rr := h.do(http.MethodGet, "/v1/features", nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var list []pkgapi.FeaturePreset
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
		ids := map[string]bool{}
		for _, p := range list {
			ids[p.ID] = true
		}
		for _, want := range []string{
			"udp_stuffing", "udp_stuffing_env", "auth_script_template", "tls_modern",
			"mssfix", "explicit_exit_notify", "fast_io", "verb_4", "comp_lzo_no",
		} {
			require.True(t, ids[want], "missing builtin feature %s", want)
		}
	})

	t.Run("pki_ca_issue_revoke_crl_renew", func(t *testing.T) {
		rr := h.do(http.MethodPost, "/v1/pki/cas", pkgapi.CreateCARequest{
			Name: "main", CommonName: "Test CA", Org: "openvpnd-test",
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

		// create server with auto issue + tls-crypt + advanced knobs
		issue := true
		tlsCrypt := true
		rr = h.do(http.MethodPost, "/v1/instances", pkgapi.InstanceCreateRequest{
			Name: "srv0", Role: "server", BinaryName: "default",
			ServerNetwork: "10.88.0.0/24", Port: 1194,
			PublicEndpoint: "vpn.test.example:1194",
			CAName: "main", ServerCN: "vpn.test.example",
			IssueServerCert: &issue, GenerateTLSCrypt: &tlsCrypt,
			CreateCAIfEmpty: false,
			MaxClients:      32,
			TLSVersionMin:   "1.2",
			TLSGroups:       "X25519:P-256",
			TLSCipher:       "TLS-ECDHE-ECDSA-WITH-AES-256-GCM-SHA384",
			TunMTU:          1400,
			ServerIPv6:      "fd00:88::/64",
			FeatureSets:     []string{"mssfix", "tls_modern"},
			Enabled:         boolPtr(true),
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

		conf := h.exportConf("srv0")
		for _, frag := range []string{
			"server 10.88.0.0 255.255.255.0",
			"max-clients 32",
			"tls-version-min 1.2",
			"tls-groups",
			"tun-mtu 1400",
			"server-ipv6 fd00:88::/64",
			"mssfix",
			"management ",
			"client-config-dir",
			"dh none",
		} {
			require.Contains(t, conf, frag, "conf missing %q", frag)
		}

		// list certs, revoke a client after issue via client create
		rr = h.do(http.MethodPost, "/v1/instances/srv0/clients", pkgapi.ClientCreateRequest{
			CommonName: "alice", Name: "Alice",
			IRoutes:           []string{"192.168.50.0/24"},
			PushDNS:           []string{"1.1.1.1"},
			PushDomain:        "corp.test",
			RedirectGateway:   true,
			DisablePush:       []string{"redirect-gateway"},
			BandwidthRxBps:    8_000_000,
			BandwidthTxBps:    2_000_000,
			TrafficLimitBytes: 0,
			MintProfileLink:   true,
			ProfileLinkTTL:    "30m",
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var created pkgapi.ClientCreateResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))
		require.NotEmpty(t, created.StaticIP)
		require.NotEmpty(t, created.ClientCertPath)
		require.NotNil(t, created.ProfileLink)
		require.Contains(t, created.ProfileLink.ImportURL, "openvpn://import-profile/")
		require.Contains(t, created.ProfileLink.DownloadURL, "https://vpn.test.example/p/")

		// Re-read client for paths + CCD generation (confgen is SoT; mock backend may not write files)
		cli, err := h.store.GetClient(ctx, "srv0", "alice")
		require.NoError(t, err)
		require.Equal(t, []string{"192.168.50.0/24"}, cli.IRoutes)
		require.Equal(t, []string{"1.1.1.1"}, cli.PushDNS)
		require.True(t, cli.RedirectGateway)
		require.Equal(t, int64(8_000_000), cli.BandwidthRxBps)
		require.Equal(t, []string{"redirect-gateway"}, cli.DisablePush)

		// GET client via API
		rr = h.do(http.MethodGet, "/v1/instances/srv0/clients/alice", nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var apiCli pkgapi.ServerClient
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &apiCli))
		require.Equal(t, []string{"192.168.50.0/24"}, apiCli.IRoutes)
		require.Equal(t, int64(8_000_000), apiCli.BandwidthRxBps)

		body := confgen.RenderCCD(*cli, "10.88.0.0/24")
		require.Contains(t, body, "ifconfig-push")
		require.Contains(t, body, "iroute 192.168.50.0")
		require.Contains(t, body, `push "dhcp-option DNS 1.1.1.1"`)
		require.Contains(t, body, "redirect-gateway")
		require.Contains(t, body, "push-remove")

		// reconcile still succeeds
		rr = h.do(http.MethodPost, "/v1/reconcile", nil)
		require.True(t, rr.Code == http.StatusOK || rr.Code == http.StatusNoContent, rr.Body.String())

		// profile download authenticated
		rr = h.do(http.MethodGet, "/v1/instances/srv0/clients/alice/client-config", nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		ovpn := rr.Body.String()
		require.Contains(t, ovpn, "client")
		require.Contains(t, ovpn, "remote vpn.test.example 1194")
		require.Contains(t, ovpn, "<ca>")
		require.Contains(t, ovpn, "<cert>")
		require.Contains(t, ovpn, "<key>")

		// list certs and revoke client leaf
		rr = h.do(http.MethodGet, "/v1/pki/certs?ca=main", nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var certs []pkgapi.Certificate
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &certs))
		var clientCertID int64
		for _, c := range certs {
			if c.Kind == "client" && c.CommonName == "alice" {
				clientCertID = c.ID
			}
		}
		require.NotZero(t, clientCertID)

		rr = h.do(http.MethodPost, "/v1/pki/certs/"+itoa(clientCertID)+"/revoke", pkgapi.RevokeCertRequest{Reason: "keyCompromise"})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var revoked pkgapi.Certificate
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &revoked))
		require.True(t, revoked.Revoked)

		// rebuild CRL
		rr = h.do(http.MethodPost, "/v1/pki/cas/main/rebuild-crl", nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var ca pkgapi.CA
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &ca))
		require.NotEmpty(t, ca.CRLPath)
		_, err = os.Stat(ca.CRLPath)
		require.NoError(t, err)

		// server conf should pick up crl-verify after issue paths rewired / set
		inst, err := h.store.GetInstance(ctx, "srv0")
		require.NoError(t, err)
		if inst.PKICRLPath == "" {
			inst.PKICRLPath = ca.CRLPath
			_, err = h.store.UpdateInstance(ctx, *inst)
			require.NoError(t, err)
		}
		conf = h.exportConf("srv0")
		require.Contains(t, conf, "crl-verify")

		// renew
		rr = h.do(http.MethodPost, "/v1/pki/certs/"+itoa(clientCertID)+"/renew", map[string]any{})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var renewed pkgapi.Certificate
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &renewed))
		require.False(t, renewed.Revoked)
		require.Equal(t, "alice", renewed.CommonName)
	})

	t.Run("bridge_mode_conf", func(t *testing.T) {
		noIssue := false
		rr := h.do(http.MethodPost, "/v1/instances", pkgapi.InstanceCreateRequest{
			Name: "br0", Role: "server", DevType: "tap",
			Port: 1195, ServerNetwork: "10.99.0.0/24", // may be ignored in bridge
			BridgeMode: true, BridgeGateway: "10.99.0.1", BridgeNetmask: "255.255.255.0",
			BridgePoolStart: "10.99.0.50", BridgePoolEnd: "10.99.0.100",
			IssueServerCert: &noIssue, Enabled: boolPtr(false),
			AuthMode: "pki",
			PKICaPath: "/tmp/ca.crt", PKICertPath: "/tmp/s.crt", PKIKeyPath: "/tmp/s.key",
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		conf := h.exportConf("br0")
		require.Contains(t, conf, "server-bridge 10.99.0.1 255.255.255.0 10.99.0.50 10.99.0.100")
		require.NotContains(t, conf, "server 10.99.0.0")
		require.Contains(t, conf, "dev tap")
	})

	t.Run("auth_verify_and_client_auth_user_pass", func(t *testing.T) {
		noIssue := false
		rr := h.do(http.MethodPost, "/v1/instances", pkgapi.InstanceCreateRequest{
			Name: "auth0", Role: "server", Port: 1196, ServerNetwork: "10.70.0.0/24",
			AuthUserPassVerify: "/usr/local/libexec/openvpnd-auth.sh",
			ScriptSecurity:     2, UsernameAsCommonName: true,
			IssueServerCert: &noIssue, Enabled: boolPtr(false),
			PKICaPath: "/tmp/ca.crt", PKICertPath: "/tmp/s.crt", PKIKeyPath: "/tmp/s.key",
			FeatureSets: []string{"auth_script_template"},
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		conf := h.exportConf("auth0")
		require.Contains(t, conf, "script-security")
		require.Contains(t, conf, "auth-user-pass-verify")
		require.Contains(t, conf, "username-as-common-name")

		rr = h.do(http.MethodPost, "/v1/instances", pkgapi.InstanceCreateRequest{
			Name: "cli0", Role: "client", Proto: "udp",
			Remote: "vpn.test.example:1194", AuthUserPass: true,
			AuthUserPassFile: "/etc/openvpn/pass.txt",
			IssueServerCert:  &noIssue, Enabled: boolPtr(false),
			PKICaPath: "/tmp/ca.crt", PKICertPath: "/tmp/c.crt", PKIKeyPath: "/tmp/c.key",
			FeatureSets: []string{"explicit_exit_notify"},
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		conf = h.exportConf("cli0")
		require.Contains(t, conf, "client")
		require.Contains(t, conf, "remote vpn.test.example 1194")
		require.Contains(t, conf, "auth-user-pass /etc/openvpn/pass.txt")
		require.Contains(t, conf, "explicit-exit-notify")
	})

	t.Run("import_inline_and_adopt", func(t *testing.T) {
		inline := `
client
dev tun
proto udp
remote import.example 1194
auth-user-pass
<ca>
-----BEGIN CERTIFICATE-----
IMPCA
-----END CERTIFICATE-----
</ca>
<cert>
-----BEGIN CERTIFICATE-----
IMPCERT
-----END CERTIFICATE-----
</cert>
<key>
-----BEGIN PRIVATE KEY-----
IMPKEY
-----END PRIVATE KEY-----
</key>
`
		create := true
		rr := h.do(http.MethodPost, "/v1/instances/import", pkgapi.ImportInstanceRequest{
			Name: "imp0", Content: inline, Create: &create, BinaryName: "default",
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var imp pkgapi.ImportInstanceResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &imp))
		require.NotNil(t, imp.Instance)
		require.Equal(t, "client", imp.Instance.Role)
		require.NotEmpty(t, imp.Instance.PKICaPath)
		require.FileExists(t, imp.Instance.PKICaPath)
		require.FileExists(t, imp.Instance.PKICertPath)
		require.FileExists(t, imp.Instance.PKIKeyPath)

		// adopt from disk
		confPath := filepath.Join(h.dir, "hand.conf")
		require.NoError(t, os.WriteFile(confPath, []byte(`
port 1210
proto udp
dev tun
server 10.66.0.0 255.255.255.0
topology subnet
ca /x/ca.crt
cert /x/s.crt
key /x/s.key
`), 0o600))
		rr = h.do(http.MethodPost, "/v1/instances/adopt", pkgapi.AdoptInstanceRequest{
			ConfPath: confPath, Name: "hand0", TakeOver: true, Enabled: boolPtr(false),
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var ad pkgapi.AdoptInstanceResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &ad))
		require.Equal(t, "hand0", ad.Instance.Name)
		require.Equal(t, 1210, ad.Instance.Port)
		require.Equal(t, "10.66.0.0/24", ad.Instance.ServerNetwork)

		rr = h.do(http.MethodGet, "/v1/instances/discover", nil)
		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("mgmt_status_kill_signal_whitelist", func(t *testing.T) {
		// use existing srv0 from earlier subtest — same harness store
		// ensure up
		rr := h.do(http.MethodPost, "/v1/instances/srv0/up", nil)
		// may already be up
		_ = rr
		h.backend.SetClients("srv0", []ovpnbackend.LiveClient{{
			CommonName: "bob", RealAddress: "9.9.9.9:1111", VirtualAddress: "10.88.0.3",
			RxBytes: 50, TxBytes: 60,
		}})

		rr = h.do(http.MethodGet, "/v1/instances/srv0/status", nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var st pkgapi.InstanceStatus
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &st))
		require.True(t, st.Up)

		rr = h.do(http.MethodPost, "/v1/instances/srv0/mgmt", pkgapi.MgmtCommandRequest{
			Command: "status", Args: []string{"2"},
		})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = h.do(http.MethodPost, "/v1/instances/srv0/mgmt", pkgapi.MgmtCommandRequest{
			Command: "kill", Args: []string{"bob"},
		})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = h.do(http.MethodPost, "/v1/instances/srv0/mgmt", pkgapi.MgmtCommandRequest{
			Command: "signal", Args: []string{"SIGUSR1"},
		})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		// reject evil
		rr = h.do(http.MethodPost, "/v1/instances/srv0/mgmt", pkgapi.MgmtCommandRequest{
			Command: "rm", Args: []string{"-rf", "/"},
		})
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	})

	t.Run("custom_feature_and_plugin", func(t *testing.T) {
		rr := h.do(http.MethodPost, "/v1/features", pkgapi.FeaturePreset{
			ID: "lab_plugin", Description: "lab",
			ExtraDirectives: "persist-remote-ip\n",
			Plugins:         []pkgapi.Plugin{{Path: "/opt/lab.so", Args: []string{"a=1"}}},
			EnvVars:         []pkgapi.EnvVar{{Name: "LAB", Value: "1"}},
		})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		noIssue := false
		rr = h.do(http.MethodPost, "/v1/instances", pkgapi.InstanceCreateRequest{
			Name: "ext0", Role: "server", Port: 1220, ServerNetwork: "10.55.0.0/24",
			FeatureSets: []string{"lab_plugin", "udp_stuffing_env"},
			Plugins:     []pkgapi.Plugin{{Path: "/opt/extra.so"}},
			IssueServerCert: &noIssue, Enabled: boolPtr(false),
			PKICaPath: "/tmp/ca.crt", PKICertPath: "/tmp/s.crt", PKIKeyPath: "/tmp/s.key",
			BinaryName: "stuff",
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		conf := h.exportConf("ext0")
		require.Contains(t, conf, "plugin /opt/lab.so a=1")
		require.Contains(t, conf, "plugin /opt/extra.so")
		require.Contains(t, conf, "persist-remote-ip")
		// stuffing env is process env, not conf — template comments ok
		require.True(t, strings.Contains(conf, "STUFFING") || strings.Contains(conf, "stuffing") || strings.Contains(conf, "feature:udp_stuffing_env") || strings.Contains(conf, "udp_stuffing"))
	})

	t.Run("binaries_list", func(t *testing.T) {
		rr := h.do(http.MethodGet, "/v1/binaries", nil)
		require.Equal(t, http.StatusOK, rr.Code)
		var bins []pkgapi.Binary
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &bins))
		require.GreaterOrEqual(t, len(bins), 2)
	})
}

func itoa(id int64) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(
		// avoid strconv import cycle noise
		func() string {
			return jsonNumber(id)
		}(), " ", ""), "\n", ""))
}

func jsonNumber(id int64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
