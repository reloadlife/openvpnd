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

	// client + issue
	cb, _ := json.Marshal(pkgapi.ClientCreateRequest{CommonName: "bob", Name: "Bob"})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/clients", bytes.NewReader(cb))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/clients/bob/issue-cert", bytes.NewReader([]byte(`{"ca_name":"main"}`)))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

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
}
