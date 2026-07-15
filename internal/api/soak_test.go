//go:build soak

package api_test

// Stability soak: mock backend, many instances, up/down + list + reconcile loops.
// Not part of default `go test ./...` / `make test`.
//
//	make test-soak
//	go test -tags=soak -count=1 ./internal/api/ -run TestSoakStability -timeout 120s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

const (
	soakInstanceCount = 8
	soakIterations    = 50
)

type soakHarness struct {
	t       *testing.T
	store   *db.Store
	backend *ovpnbackend.Mock
	router  http.Handler
	token   string
}

func newSoakHarness(t *testing.T) *soakHarness {
	t.Helper()
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	dir := t.TempDir()
	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
	}))

	cfg := &config.DaemonConfig{}
	cfg.Auth.Token = "soak-token"
	cfg.OpenVPN.DefaultBinary = "default"
	cfg.OpenVPN.ConfDir = filepath.Join(dir, "conf")
	cfg.OpenVPN.RuntimeDir = filepath.Join(dir, "run")
	cfg.OpenVPN.PKIDir = filepath.Join(dir, "pki")
	cfg.OpenVPN.Persistence = "hybrid"
	cfg.OpenVPN.UseMockBackend = true
	cfg.OpenVPN.BandwidthEnforcement = "off"
	cfg.OpenVPN.Binaries = map[string]string{"default": "/usr/sbin/openvpn"}
	cfg.ProfileLinks.DefaultTTL = "1h"
	cfg.ProfileLinks.DefaultMaxUses = 1

	backend := ovpnbackend.NewMock()
	cache := stats.NewCache()
	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir:              cfg.OpenVPN.ConfDir,
		RuntimeDir:           cfg.OpenVPN.RuntimeDir,
		DefaultBinary:        "default",
		BandwidthEnforcement: "off",
	}, slog.Default())
	srv := api.NewServer(store, backend, cache, rec, cfg, slog.Default())

	return &soakHarness{
		t: t, store: store, backend: backend, router: srv.Router(), token: "soak-token",
	}
}

func (h *soakHarness) do(method, path string, body any) *httptest.ResponseRecorder {
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

// TestSoakStability creates N mock instances and repeatedly flips up/down,
// lists instances, and forces reconcile for soakIterations cycles.
func TestSoakStability(t *testing.T) {
	h := newSoakHarness(t)
	noIssue := false
	names := make([]string, 0, soakInstanceCount)

	for i := 0; i < soakInstanceCount; i++ {
		name := fmt.Sprintf("soak%d", i)
		// Distinct /24 per instance to avoid pool collisions in validation.
		net := fmt.Sprintf("10.%d.0.0/24", 80+i)
		rr := h.do(http.MethodPost, "/v1/instances", pkgapi.InstanceCreateRequest{
			Name:            name,
			Role:            "server",
			BinaryName:      "default",
			Port:            1194 + i,
			ServerNetwork:   net,
			Topology:        "subnet",
			IssueServerCert: &noIssue,
			Enabled:         boolPtr(false),
		})
		require.Equal(t, http.StatusCreated, rr.Code, "create %s: %s", name, rr.Body.String())
		names = append(names, name)
	}

	// Initial list
	rr := h.do(http.MethodGet, "/v1/instances", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var listed []pkgapi.Instance
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &listed))
	require.GreaterOrEqual(t, len(listed), soakInstanceCount)

	for iter := 0; iter < soakIterations; iter++ {
		// Flip every instance up then down (or reverse on odd iterations).
		for _, name := range names {
			if iter%2 == 0 {
				rr = h.do(http.MethodPost, "/v1/instances/"+name+"/up", nil)
				require.Equal(t, http.StatusOK, rr.Code, "iter=%d up %s: %s", iter, name, rr.Body.String())
			} else {
				rr = h.do(http.MethodPost, "/v1/instances/"+name+"/down", nil)
				require.Equal(t, http.StatusOK, rr.Code, "iter=%d down %s: %s", iter, name, rr.Body.String())
			}
		}

		// List must stay stable
		rr = h.do(http.MethodGet, "/v1/instances", nil)
		require.Equal(t, http.StatusOK, rr.Code, "iter=%d list: %s", iter, rr.Body.String())
		listed = nil
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &listed))
		require.Equal(t, soakInstanceCount, len(listed), "iter=%d instance count drift", iter)

		// Explicit reconcile
		rr = h.do(http.MethodPost, "/v1/reconcile", nil)
		require.Equal(t, http.StatusOK, rr.Code, "iter=%d reconcile: %s", iter, rr.Body.String())

		// Health stays green
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		hr := httptest.NewRecorder()
		h.router.ServeHTTP(hr, req)
		require.Equal(t, http.StatusOK, hr.Code, "iter=%d healthz", iter)
	}

	// Final down for all (leave clean)
	for _, name := range names {
		rr = h.do(http.MethodPost, "/v1/instances/"+name+"/down", nil)
		require.Equal(t, http.StatusOK, rr.Code, "final down %s: %s", name, rr.Body.String())
	}

	// One more reconcile + list
	rr = h.do(http.MethodPost, "/v1/reconcile", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = h.do(http.MethodGet, "/v1/instances", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}
