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

	// create server instance
	body := pkgapi.InstanceCreateRequest{
		Name:          "ovpn0",
		Role:          "server",
		BinaryName:    "v26",
		Port:          1194,
		ServerNetwork: "10.8.0.0/24",
		Topology:      "subnet",
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
	ib, _ := json.Marshal(pkgapi.InstanceCreateRequest{
		Name: "ovpn0", Role: "server", ServerNetwork: "10.8.0.0/24",
		PublicEndpoint: "vpn.example.com:1194",
		PKICaPath:      ca,
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
