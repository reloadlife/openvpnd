package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/api"
	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/reconcile"
	"github.com/reloadlife/openvpnd/internal/stats"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func TestAPIFlow(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
		"v26":     "/opt/openvpn26/sbin/openvpn",
	}))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "test-token"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = "/tmp/openvpnd-test/conf"
	cfg.OpenVPN.RuntimeDir = "/tmp/openvpnd-test/run"
	cfg.OpenVPN.Persistence = "hybrid"

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir:       cfg.OpenVPN.ConfDir,
		RuntimeDir:    cfg.OpenVPN.RuntimeDir,
		DefaultBinary: cfg.OpenVPN.DefaultBinary,
	}, slog.Default())

	srv := api.NewServer(store, backend, cache, rec, cfg, slog.Default())
	router := srv.Router()

	// unauthorized
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/instances", nil))
	require.Equal(t, http.StatusUnauthorized, rr.Code)

	// create server instance (disable auto PKI — no CA in this test)
	noIssue := false
	body := pkgapi.InstanceCreateRequest{
		Name:          "ovpn0",
		Role:          "server",
		BinaryName:    "v26",
		Port:          1194,
		ServerNetwork: "10.8.0.0/24",
		Topology:      "subnet",
		IssueServerCert: &noIssue,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/instances", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	// create client with auto IP
	cbody := pkgapi.ClientCreateRequest{CommonName: "alice", Name: "Alice"}
	b, _ = json.Marshal(cbody)
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/clients", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var cli pkgapi.ServerClient
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cli))
	require.Equal(t, "10.8.0.2", cli.StaticIP)

	// list binaries
	req = httptest.NewRequest(http.MethodGet, "/v1/binaries", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var bins []pkgapi.Binary
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &bins))
	require.GreaterOrEqual(t, len(bins), 2)

	// health
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestProfileLink(t *testing.T) {
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.crt")
	cert := filepath.Join(dir, "alice.crt")
	key := filepath.Join(dir, "alice.key")
	require.NoError(t, os.WriteFile(ca, []byte("-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----\n"), 0o600))
	require.NoError(t, os.WriteFile(cert, []byte("-----BEGIN CERTIFICATE-----\nCERT\n-----END CERTIFICATE-----\n"), 0o600))
	require.NoError(t, os.WriteFile(key, []byte("-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----\n"), 0o600))

	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{"default": "/usr/sbin/openvpn"}))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "test-token"
	cfg.Listen.HTTP = "127.0.0.1:51980"
	cfg.PublicBaseURL = "https://vpn.example.com"
	cfg.ProfileLinks.DefaultTTL = "1h"
	cfg.ProfileLinks.DefaultMaxUses = 1
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = dir
	cfg.OpenVPN.RuntimeDir = dir

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir: dir, RuntimeDir: dir, DefaultBinary: "default",
	}, slog.Default())
	srv := api.NewServer(store, backend, cache, rec, cfg, slog.Default())
	router := srv.Router()

	// create server + client with PKI paths
	noIssue := false
	ib, _ := json.Marshal(pkgapi.InstanceCreateRequest{
		Name: "ovpn0", Role: "server", ServerNetwork: "10.8.0.0/24",
		PublicEndpoint: "vpn.example.com:1194",
		PKICaPath:      ca,
		IssueServerCert: &noIssue,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/instances", bytes.NewReader(ib))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	cb, _ := json.Marshal(pkgapi.ClientCreateRequest{
		CommonName: "alice", ClientCertPath: cert, ClientKeyPath: key,
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/clients", bytes.NewReader(cb))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	// mint link
	lb, _ := json.Marshal(pkgapi.ProfileLinkRequest{TTL: "30m"})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/clients/alice/profile-link", bytes.NewReader(lb))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var link pkgapi.ProfileLink
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &link))
	require.Contains(t, link.DownloadURL, "https://vpn.example.com/p/")
	require.Contains(t, link.ImportURL, "openvpn://import-profile/https://vpn.example.com/p/")
	require.Equal(t, 1, link.MaxUses)

	// public download
	req = httptest.NewRequest(http.MethodGet, "/p/"+link.Token, nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "client")
	require.Contains(t, rr.Body.String(), "remote vpn.example.com 1194")
	require.Contains(t, rr.Body.String(), "<ca>")

	// single-use exhausted
	req = httptest.NewRequest(http.MethodGet, "/p/"+link.Token, nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestPKICreateAndIssue(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{"default": "/usr/sbin/openvpn"}))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "test-token"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.PKIDir = filepath.Join(dir, "pki")
	cfg.OpenVPN.ConfDir = filepath.Join(dir, "conf")
	cfg.OpenVPN.RuntimeDir = filepath.Join(dir, "run")
	cfg.Listen.HTTP = "127.0.0.1:51980"
	cfg.PublicBaseURL = "https://vpn.example.com"

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir: cfg.OpenVPN.ConfDir, RuntimeDir: cfg.OpenVPN.RuntimeDir, DefaultBinary: "default",
	}, slog.Default())
	srv := api.NewServer(store, backend, cache, rec, cfg, slog.Default())
	router := srv.Router()
	auth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
	}

	// create CA
	body, _ := json.Marshal(pkgapi.CreateCARequest{Name: "main", CommonName: "Test CA", Org: "test"})
	req := httptest.NewRequest(http.MethodPost, "/v1/pki/cas", bytes.NewReader(body))
	auth(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	// instance (issue via separate call below)
	noIssue := false
	ib, _ := json.Marshal(pkgapi.InstanceCreateRequest{
		Name: "ovpn0", Role: "server", ServerNetwork: "10.8.0.0/24", PublicEndpoint: "vpn.example.com:1194",
		IssueServerCert: &noIssue,
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances", bytes.NewReader(ib))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	// issue server cert
	sb, _ := json.Marshal(pkgapi.IssueServerCertRequest{CAName: "main", GenerateTLSCrypt: true})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/issue-server-cert", bytes.NewReader(sb))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	inst, err := store.GetInstance(ctx, "ovpn0")
	require.NoError(t, err)
	require.NotEmpty(t, inst.PKICaPath)
	require.NotEmpty(t, inst.PKICertPath)
	require.NotEmpty(t, inst.PKIKeyPath)
	require.NotEmpty(t, inst.PKITLSCryptPath)

	// one-shot client: auto IP + auto issue cert + profile link
	uses := 1
	cb, _ := json.Marshal(pkgapi.ClientCreateRequest{
		CommonName: "bob", Name: "Bob",
		MintProfileLink: true, ProfileLinkTTL: "1h", ProfileLinkMaxUses: &uses,
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/clients", bytes.NewReader(cb))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var created pkgapi.ClientCreateResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))
	require.NotEmpty(t, created.ClientCertPath)
	require.NotEmpty(t, created.ClientKeyPath)
	require.NotEmpty(t, created.StaticIP)
	require.NotNil(t, created.ProfileLink)
	require.Contains(t, created.ProfileLink.ImportURL, "openvpn://import-profile/")
	require.Contains(t, strings.Join(created.AutoFilled, ","), "issue_cert")

	cli, err := store.GetClient(ctx, "ovpn0", "bob")
	require.NoError(t, err)
	require.NotEmpty(t, cli.ClientCertPath)
	require.NotEmpty(t, cli.ClientKeyPath)

	// profile works
	req = httptest.NewRequest(http.MethodGet, "/v1/instances/ovpn0/clients/bob/client-config", nil)
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "client")
	require.Contains(t, rr.Body.String(), "<ca>")
	require.Contains(t, rr.Body.String(), "<cert>")
	require.Contains(t, rr.Body.String(), "explicit-exit-notify")

	// list certs → revoke bob → CRL on CA + crl-verify in export
	req = httptest.NewRequest(http.MethodGet, "/v1/pki/certs?ca=main", nil)
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var certs []pkgapi.Certificate
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &certs))
	var bobID int64
	for _, c := range certs {
		if c.CommonName == "bob" {
			bobID = c.ID
			break
		}
	}
	require.NotZero(t, bobID)

	rb, _ := json.Marshal(pkgapi.RevokeCertRequest{Reason: "keyCompromise"})
	req = httptest.NewRequest(http.MethodPost, "/v1/pki/certs/"+strconv.FormatInt(bobID, 10)+"/revoke", bytes.NewReader(rb))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var revoked pkgapi.Certificate
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &revoked))
	require.True(t, revoked.Revoked)
	require.NotEmpty(t, revoked.RevokedAt)

	ca, err := store.GetCA(ctx, "main")
	require.NoError(t, err)
	require.NotEmpty(t, ca.CRLPath)
	require.FileExists(t, ca.CRLPath)

	inst, err = store.GetInstance(ctx, "ovpn0")
	require.NoError(t, err)
	require.Equal(t, ca.CRLPath, inst.PKICRLPath)

	req = httptest.NewRequest(http.MethodGet, "/v1/instances/ovpn0/export", nil)
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "crl-verify "+ca.CRLPath)

	// renew clears revoked
	req = httptest.NewRequest(http.MethodPost, "/v1/pki/certs/"+strconv.FormatInt(bobID, 10)+"/renew", bytes.NewReader([]byte("{}")))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var renewed pkgapi.Certificate
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &renewed))
	require.False(t, renewed.Revoked)
	require.NotEqual(t, revoked.Serial, renewed.Serial)
}

func TestImportInstance(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
	}))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "test-token"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = "/tmp/openvpnd-test/conf"
	cfg.OpenVPN.RuntimeDir = "/tmp/openvpnd-test/run"

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir: cfg.OpenVPN.ConfDir, RuntimeDir: cfg.OpenVPN.RuntimeDir, DefaultBinary: "default",
	}, slog.Default())
	srv := api.NewServer(store, backend, cache, rec, cfg, slog.Default())
	router := srv.Router()
	auth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
	}

	minimalServer := `
port 1195
proto udp
dev tun
topology subnet
server 10.9.0.0 255.255.255.0
cipher AES-256-GCM
auth SHA256
ca /etc/openvpn/ca.crt
cert /etc/openvpn/server.crt
key /etc/openvpn/server.key
tls-crypt /etc/openvpn/tc.key
push "dhcp-option DNS 1.1.1.1"
push "route 10.0.0.0 255.255.0.0"
management /var/run/openvpn.sock unix
verb 3
persist-key
persist-tun
writepid /var/run/openvpn.pid
status /var/run/status 1
keepalive 10 60
client-to-client
`

	// preview only (create=false)
	noCreate := false
	previewBody, _ := json.Marshal(pkgapi.ImportInstanceRequest{
		Content: minimalServer,
		Create:  &noCreate,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/instances/import", bytes.NewReader(previewBody))
	auth(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var preview pkgapi.ImportInstanceResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &preview))
	require.Nil(t, preview.Instance)
	require.False(t, preview.Created)
	require.Equal(t, "server", preview.Parsed.Role)
	require.Equal(t, 1195, preview.Parsed.Port)
	require.Equal(t, "10.9.0.0/24", preview.Parsed.ServerNetwork)
	require.Equal(t, []string{"1.1.1.1"}, preview.Parsed.PushDNS)
	require.Equal(t, []string{"10.0.0.0/16"}, preview.Parsed.PushRoutes)
	require.Equal(t, "/etc/openvpn/ca.crt", preview.Parsed.PKICaPath)
	require.Contains(t, preview.Parsed.ExtraDirectives, "client-to-client")
	require.NotContains(t, preview.Parsed.ExtraDirectives, "management")

	// create
	doCreate := true
	createBody, _ := json.Marshal(pkgapi.ImportInstanceRequest{
		Name:       "imported",
		Content:    minimalServer,
		Create:     &doCreate,
		BinaryName: "default",
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/import", bytes.NewReader(createBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var created pkgapi.ImportInstanceResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))
	require.True(t, created.Created)
	require.NotNil(t, created.Instance)
	require.Equal(t, "imported", created.Instance.Name)
	require.Equal(t, "server", created.Instance.Role)
	require.Equal(t, 1195, created.Instance.Port)
	require.Equal(t, "10.9.0.0/24", created.Instance.ServerNetwork)
	require.Equal(t, "/etc/openvpn/server.crt", created.Instance.PKICertPath)
	require.Equal(t, []string{"1.1.1.1"}, created.Instance.PushDNS)

	// route must not collide with /instances/{name}
	list, err := store.ListInstances(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
}
