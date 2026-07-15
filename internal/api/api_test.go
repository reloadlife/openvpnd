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
		Name:            "ovpn0",
		Role:            "server",
		BinaryName:      "v26",
		Port:            1194,
		ServerNetwork:   "10.8.0.0/24",
		Topology:        "subnet",
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

	// audit middleware: successful mutation should leave an api event
	ev, err := store.ListEvents(ctx, 50)
	require.NoError(t, err)
	var foundAPI bool
	for _, e := range ev {
		if e.Kind == "api" {
			foundAPI = true
			require.Contains(t, e.Message, "POST")
			require.NotContains(t, e.Meta, "test-token")
			require.NotContains(t, e.Meta, "Authorization")
			break
		}
	}
	require.True(t, foundAPI, "expected audit api event after mutations")
}

func TestReadyzChecks(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	dir := t.TempDir()
	// Fake openvpn binary path that exists
	bin := filepath.Join(dir, "openvpn")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755))
	confDir := filepath.Join(dir, "conf")
	pkiDir := filepath.Join(dir, "pki")

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "test-token"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.Binaries = map[string]string{"default": bin}
	cfg.OpenVPN.ConfDir = confDir
	cfg.OpenVPN.PKIDir = pkiDir
	cfg.OpenVPN.UseMockBackend = true
	cfg.OpenVPN.BandwidthEnforcement = "off"
	cfg.Listen.HTTP = "127.0.0.1:51980"
	cfg.Production = false

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	srv := api.NewServer(store, backend, cache, nil, cfg, slog.Default())
	router := srv.Router()

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var ready api.ReadyzResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &ready))
	require.Equal(t, "ok", ready.Status)
	require.Equal(t, "ok", ready.Checks["db"])
	require.Equal(t, "ok", ready.Checks["default_binary"])
	require.Equal(t, "ok", ready.Checks["conf_dir"])
	require.Equal(t, "ok", ready.Checks["pki_dir"])
	require.Equal(t, "mock", ready.Checks["backend"])

	// Missing binary → degraded, still 200
	cfg.OpenVPN.Binaries["default"] = filepath.Join(dir, "no-such-binary")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &ready))
	require.Equal(t, "degraded", ready.Status)
	require.Equal(t, "missing", ready.Checks["default_binary"])
	require.Equal(t, "ok", ready.Checks["db"])

	// system/info
	req := httptest.NewRequest(http.MethodGet, "/v1/system/info", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var info pkgapi.SystemInfo
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &info))
	require.Equal(t, "mock", info.Backend)
	require.Equal(t, "off", info.BandwidthMode)
	require.False(t, info.Production)
	require.Equal(t, "127.0.0.1:51980", info.ListenHTTP)
	require.NotNil(t, info.InstancesTotal)
}

func TestReadyzDBFail(t *testing.T) {
	// Closed store → ping fails → status fail / 503
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Close())

	dir := t.TempDir()
	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "t"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.Binaries = map[string]string{"default": "/usr/sbin/openvpn"}
	cfg.OpenVPN.ConfDir = dir
	cfg.OpenVPN.PKIDir = filepath.Join(dir, "pki")
	cfg.OpenVPN.UseMockBackend = true

	srv := api.NewServer(store, ovpnbackend.NewMock(), stats.NewCache(), nil, cfg, slog.Default())
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	var ready api.ReadyzResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &ready))
	require.Equal(t, "fail", ready.Status)
	require.Equal(t, "fail", ready.Checks["db"])
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
		PublicEndpoint:  "vpn.example.com:1194",
		PKICaPath:       ca,
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

func TestImportInstanceInlinePEMs(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
	}))

	pkiDir := t.TempDir()
	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "test-token"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = filepath.Join(t.TempDir(), "conf")
	cfg.OpenVPN.RuntimeDir = filepath.Join(t.TempDir(), "run")
	cfg.OpenVPN.PKIDir = pkiDir

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

	inlineClient := `
client
dev tun
proto udp
remote vpn.example.com 1194
cipher AES-256-GCM
auth SHA256
<ca>
-----BEGIN CERTIFICATE-----
CA_INLINE
-----END CERTIFICATE-----
</ca>
<cert>
-----BEGIN CERTIFICATE-----
CERT_INLINE
-----END CERTIFICATE-----
</cert>
<key>
-----BEGIN PRIVATE KEY-----
KEY_INLINE
-----END PRIVATE KEY-----
</key>
<tls-crypt>
-----BEGIN OpenVPN Static key V1-----
TC_INLINE
-----END OpenVPN Static key V1-----
</tls-crypt>
explicit-exit-notify 1
`

	// preview keeps inline warnings and does not write files
	noCreate := false
	previewBody, _ := json.Marshal(pkgapi.ImportInstanceRequest{
		Content: inlineClient,
		Create:  &noCreate,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/instances/import", bytes.NewReader(previewBody))
	auth(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var preview pkgapi.ImportInstanceResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &preview))
	require.False(t, preview.Created)
	require.Equal(t, "client", preview.Parsed.Role)
	require.Empty(t, preview.Parsed.PKICaPath)
	require.NotEmpty(t, preview.Warnings)
	require.Contains(t, preview.Warnings[0], "inline <")

	// create materializes under pki_dir/imported/<name>
	doCreate := true
	createBody, _ := json.Marshal(pkgapi.ImportInstanceRequest{
		Name:       "inline-client",
		Content:    inlineClient,
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
	require.Equal(t, "inline-client", created.Instance.Name)
	require.Equal(t, "client", created.Instance.Role)

	wantCA := filepath.Join(pkiDir, "imported", "inline-client", "ca.crt")
	wantCert := filepath.Join(pkiDir, "imported", "inline-client", "client.crt")
	wantKey := filepath.Join(pkiDir, "imported", "inline-client", "client.key")
	wantTC := filepath.Join(pkiDir, "imported", "inline-client", "tls-crypt.key")
	require.Equal(t, wantCA, created.Instance.PKICaPath)
	require.Equal(t, wantCert, created.Instance.PKICertPath)
	require.Equal(t, wantKey, created.Instance.PKIKeyPath)
	require.Equal(t, wantTC, created.Instance.PKITLSCryptPath)
	require.FileExists(t, wantCA)
	require.FileExists(t, wantCert)
	require.FileExists(t, wantKey)
	require.FileExists(t, wantTC)
	// Inline PEM warnings cleared after materialize.
	for _, w := range created.Warnings {
		require.NotContains(t, w, "inline <")
	}
	caPEM, err := os.ReadFile(wantCA)
	require.NoError(t, err)
	require.Contains(t, string(caPEM), "CA_INLINE")
}

func TestAdoptAndDiscover(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
	}))

	dir := t.TempDir()
	confPath := filepath.Join(dir, "legacy-server.conf")
	conf := `
port 1201
proto udp
dev tun
topology subnet
server 10.77.0.0 255.255.255.0
ca /etc/openvpn/ca.crt
cert /etc/openvpn/server.crt
key /etc/openvpn/server.key
cipher AES-256-GCM
auth SHA256
push "dhcp-option DNS 9.9.9.9"
`
	require.NoError(t, os.WriteFile(confPath, []byte(conf), 0o600))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "test-token"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = filepath.Join(dir, "conf")
	cfg.OpenVPN.RuntimeDir = filepath.Join(dir, "run")
	// Exercise live take-over path: dead PID soft-fails; create still succeeds.
	cfg.OpenVPN.AdoptTakeoverEnabled = true

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

	// discover — always succeeds (list may be empty)
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/discover", nil)
	auth(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var cands []pkgapi.OpenVPNCandidate
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cands))
	// shape only; host may have no openvpn
	require.NotNil(t, cands)

	// adopt missing path
	badBody, _ := json.Marshal(pkgapi.AdoptInstanceRequest{ConfPath: filepath.Join(dir, "nope.conf")})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/adopt", bytes.NewReader(badBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())

	// adopt relative path rejected
	relBody, _ := json.Marshal(pkgapi.AdoptInstanceRequest{ConfPath: "relative.conf"})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/adopt", bytes.NewReader(relBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

	// adopt temp conf with take_over (dead PID soft-fails; create still succeeds)
	enabled := true
	adoptBody, _ := json.Marshal(pkgapi.AdoptInstanceRequest{
		ConfPath:   confPath,
		Name:       "legacy",
		Enabled:    &enabled,
		BinaryName: "default",
		TakeOver:   true,
		PID:        99999,
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/adopt", bytes.NewReader(adoptBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var adopted pkgapi.AdoptInstanceResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &adopted))
	require.NotNil(t, adopted.Instance)
	require.Equal(t, "legacy", adopted.Instance.Name)
	require.Equal(t, "server", adopted.Instance.Role)
	require.Equal(t, 1201, adopted.Instance.Port)
	require.Equal(t, "10.77.0.0/24", adopted.Instance.ServerNetwork)
	require.Equal(t, []string{"9.9.9.9"}, adopted.Instance.PushDNS)
	require.Equal(t, confPath, adopted.ConfPath)
	require.Equal(t, 99999, adopted.PID)
	require.NotEmpty(t, adopted.Notes)
	joined := strings.Join(adopted.Notes, " ")
	require.True(t, strings.Contains(joined, "take_over=true"), "notes=%v", adopted.Notes)
	require.True(t, strings.Contains(joined, "99999") || strings.Contains(joined, "inspect"), "notes=%v", adopted.Notes)
	require.True(t, adopted.Instance.Enabled, "take_over should enable the instance")
	require.Equal(t, "/etc/openvpn/server.crt", adopted.Instance.PKICertPath)

	// route static path must not be treated as instance name
	list, err := store.ListInstances(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "legacy", list[0].Name)
}

func TestInstanceMgmtAndStatus(t *testing.T) {
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
	cfg.OpenVPN.ConfDir = "/tmp/openvpnd-mgmt-test/conf"
	cfg.OpenVPN.RuntimeDir = "/tmp/openvpnd-mgmt-test/run"

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

	noIssue := false
	body, _ := json.Marshal(pkgapi.InstanceCreateRequest{
		Name: "ovpn0", Role: "server", ServerNetwork: "10.8.0.0/24",
		Port: 1194, IssueServerCert: &noIssue, Enabled: boolPtr(true),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/instances", bytes.NewReader(body))
	auth(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	// ensure backend has live instance (create + reconcile should start enabled instance)
	// inject connected clients for status
	backend.SetClients("ovpn0", []ovpnbackend.LiveClient{{
		CommonName: "alice", RealAddress: "1.2.3.4:5678", VirtualAddress: "10.8.0.2",
		RxBytes: 1000, TxBytes: 2000,
	}})

	// GET status — structured live status
	req = httptest.NewRequest(http.MethodGet, "/v1/instances/ovpn0/status", nil)
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var st pkgapi.InstanceStatus
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &st))
	require.Equal(t, "ovpn0", st.Name)
	require.True(t, st.Up)
	require.Equal(t, 1, st.ConnectedClients)
	require.Len(t, st.Clients, 1)
	require.Equal(t, "alice", st.Clients[0].CommonName)
	require.Equal(t, int64(1000), st.Clients[0].RxBytes)
	require.Equal(t, int64(2000), st.TxBytes)

	// POST mgmt status
	mgmtBody, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: "status", Args: []string{"2"}})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(mgmtBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var mgmtResp pkgapi.MgmtCommandResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &mgmtResp))
	require.Contains(t, mgmtResp.Output, "CLIENT_LIST")
	require.Contains(t, mgmtResp.Output, "alice")

	// kill client
	killBody, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: "kill", Args: []string{"alice"}})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(killBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &mgmtResp))
	require.Contains(t, mgmtResp.Output, "SUCCESS")

	// signal soft restart
	sigBody, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: "signal", Args: []string{"SIGUSR1"}})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(sigBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// version / pid / hold / state
	for _, cmd := range []string{"version", "pid", "hold", "state"} {
		b, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: cmd})
		req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(b))
		auth(req)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, "cmd=%s body=%s", cmd, rr.Body.String())
	}

	// reject unknown command
	badBody, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: "exit"})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(badBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "not allowed")

	// kill without args
	noArg, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: "kill"})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(noArg))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

	// bad signal
	badSig, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: "signal", Args: []string{"SIGKILL"}})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(badSig))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

	// missing instance
	req = httptest.NewRequest(http.MethodGet, "/v1/instances/nope/status", nil)
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)

	// down instance → mgmt conflict
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/down", nil)
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	stillBody, _ := json.Marshal(pkgapi.MgmtCommandRequest{Command: "pid"})
	req = httptest.NewRequest(http.MethodPost, "/v1/instances/ovpn0/mgmt", bytes.NewReader(stillBody))
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())

	// status when down still returns 200 with error field
	req = httptest.NewRequest(http.MethodGet, "/v1/instances/ovpn0/status", nil)
	auth(req)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &st))
	require.False(t, st.Up)
	require.NotEmpty(t, st.Error)
}

func boolPtr(v bool) *bool { return &v }

func TestMultiTokenRBAC(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
	}))

	dir := t.TempDir()
	cfg := &config.DaemonConfig{}
	// Multi-token mode: only tokens list is accepted (legacy auth.token ignored).
	cfg.Auth.Token = "legacy-ignored"
	cfg.Auth.Tokens = []config.AuthToken{
		{Name: "admin", Token: "admin-tok", Role: "admin"},
		{Name: "ops", Token: "ops-tok", Role: "operator"},
		{Name: "ro", Token: "ro-tok", Role: "readonly"},
	}
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = filepath.Join(dir, "conf")
	cfg.OpenVPN.RuntimeDir = filepath.Join(dir, "run")
	cfg.OpenVPN.PKIDir = filepath.Join(dir, "pki")
	cfg.OpenVPN.Persistence = "hybrid"

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir: cfg.OpenVPN.ConfDir, RuntimeDir: cfg.OpenVPN.RuntimeDir, DefaultBinary: "default",
	}, slog.Default())
	srv := api.NewServer(store, backend, cache, rec, cfg, slog.Default())
	router := srv.Router()

	withTok := func(req *http.Request, tok string) *http.Request {
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	do := func(method, path, tok string, body []byte) *httptest.ResponseRecorder {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, withTok(r, tok))
		return rr
	}
	errCode := func(rr *httptest.ResponseRecorder) string {
		var env map[string]map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &env)
		if e, ok := env["error"]; ok {
			if c, ok := e["code"].(string); ok {
				return c
			}
		}
		return ""
	}

	// Legacy single token no longer works when tokens list is set.
	rr := do(http.MethodGet, "/v1/instances", "legacy-ignored", nil)
	require.Equal(t, http.StatusUnauthorized, rr.Code)

	// --- readonly: GET ok, POST forbidden ---
	rr = do(http.MethodGet, "/v1/instances", "ro-tok", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	rr = do(http.MethodGet, "/v1/config", "ro-tok", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var cfgResp pkgapi.DaemonConfig
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cfgResp))
	require.Equal(t, "readonly", cfgResp.Role)

	noIssue := false
	instBody, _ := json.Marshal(pkgapi.InstanceCreateRequest{
		Name: "ovpn0", Role: "server", ServerNetwork: "10.8.0.0/24",
		Port: 1194, IssueServerCert: &noIssue,
	})
	rr = do(http.MethodPost, "/v1/instances", "ro-tok", instBody)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Equal(t, "forbidden", errCode(rr))

	// --- operator: can create instance + reconcile; cannot delete CA / system backup ---
	rr = do(http.MethodPost, "/v1/instances", "ops-tok", instBody)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	rr = do(http.MethodPost, "/v1/reconcile", "ops-tok", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	rr = do(http.MethodGet, "/v1/config", "ops-tok", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cfgResp))
	require.Equal(t, "operator", cfgResp.Role)

	// operator can create CA but not delete it
	caBody, _ := json.Marshal(pkgapi.CreateCARequest{Name: "main", CommonName: "Test CA"})
	rr = do(http.MethodPost, "/v1/pki/cas", "ops-tok", caBody)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	rr = do(http.MethodDelete, "/v1/pki/cas/main", "ops-tok", nil)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Equal(t, "forbidden", errCode(rr))

	// operator blocked from system backup
	backupBody, _ := json.Marshal(map[string]string{"path": filepath.Join(dir, "b.tar.gz")})
	rr = do(http.MethodPost, "/v1/system/backup", "ops-tok", backupBody)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Equal(t, "forbidden", errCode(rr))

	// operator may still GET system info
	rr = do(http.MethodGet, "/v1/system/info", "ops-tok", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// --- admin: full access including CA delete ---
	rr = do(http.MethodGet, "/v1/config", "admin-tok", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cfgResp))
	require.Equal(t, "admin", cfgResp.Role)

	rr = do(http.MethodDelete, "/v1/pki/cas/main", "admin-tok", nil)
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())

	// admin can create another CA (filesystem still holds "main"; use a new name)
	caBody2, _ := json.Marshal(pkgapi.CreateCARequest{Name: "admin-ca", CommonName: "Admin CA"})
	rr = do(http.MethodPost, "/v1/pki/cas", "admin-tok", caBody2)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
}

func TestLegacySingleTokenIsAdmin(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{"default": "/usr/sbin/openvpn"}))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "solo-admin"
	// Tokens empty → legacy token is admin
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = t.TempDir()
	cfg.OpenVPN.RuntimeDir = t.TempDir()

	srv := api.NewServer(store, ovpnbackend.NewMock(), stats.NewCache(), nil, cfg, slog.Default())
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	req.Header.Set("Authorization", "Bearer solo-admin")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var cfgResp pkgapi.DaemonConfig
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cfgResp))
	require.Equal(t, "admin", cfgResp.Role)
}

func TestSystemBackupAPI(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	store, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	pkiDir := filepath.Join(dir, "pki")
	confDir := filepath.Join(dir, "conf")
	require.NoError(t, os.MkdirAll(pkiDir, 0o755))
	require.NoError(t, os.MkdirAll(confDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkiDir, "marker"), []byte("pki"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(confDir, "x.conf"), []byte("port 1\n"), 0o644))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "admin-token"
	cfg.DB.Path = dbPath
	cfg.OpenVPN.PKIDir = pkiDir
	cfg.OpenVPN.ConfDir = confDir
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.Binaries = map[string]string{"default": "/usr/sbin/openvpn"}

	srv := api.NewServer(store, ovpnbackend.NewMock(), stats.NewCache(), nil, cfg, slog.Default())
	router := srv.Router()

	// path write
	out := filepath.Join(dir, "backup.tar.gz")
	body, _ := json.Marshal(map[string]string{"path": out})
	req := httptest.NewRequest(http.MethodPost, "/v1/system/backup", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var resp pkgapi.SystemBackupResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Equal(t, out, resp.Path)
	require.Greater(t, resp.Bytes, int64(0))
	require.Equal(t, "ok", resp.Status)
	st, err := os.Stat(out)
	require.NoError(t, err)
	require.Greater(t, st.Size(), int64(0))

	// relative path rejected
	body, _ = json.Marshal(map[string]string{"path": "relative.tar.gz"})
	req = httptest.NewRequest(http.MethodPost, "/v1/system/backup", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)

	// system info includes paths + ready
	req = httptest.NewRequest(http.MethodGet, "/v1/system/info", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var info pkgapi.SystemInfo
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &info))
	require.Equal(t, dbPath, info.DBPath)
	require.Equal(t, pkiDir, info.PKIDir)
	require.True(t, info.Ready.DB)
	require.True(t, info.Ready.PKIDir)
	require.True(t, info.Ready.ConfDir)
}
