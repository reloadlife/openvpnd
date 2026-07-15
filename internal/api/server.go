package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/reconcile"
	"github.com/reloadlife/openvpnd/internal/stats"
)

// Server is the REST API.
type Server struct {
	store      *db.Store
	backend    ovpnbackend.Backend
	cache      *stats.Cache
	reconciler *reconcile.Reconciler
	cfg        *config.DaemonConfig
	log        *slog.Logger
}

// NewServer constructs the API server.
func NewServer(
	store *db.Store,
	backend ovpnbackend.Backend,
	cache *stats.Cache,
	reconciler *reconcile.Reconciler,
	cfg *config.DaemonConfig,
	log *slog.Logger,
) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		store:      store,
		backend:    backend,
		cache:      cache,
		reconciler: reconciler,
		cfg:        cfg,
		log:        log,
	}
}

// Router returns the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second))
	r.Use(requestID)

	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Handle("/metrics", promhttp.Handler())

	// Presigned profile download — no bearer auth (token is the credential).
	r.Get("/p/{token}", s.handlePublicProfile)

	r.Route("/v1", func(r chi.Router) {
		r.Use(bearerAuth(s.cfg.Auth.Token))
		r.Use(readOnlyGuard(s.cfg.ReadOnly))

		r.Get("/version", s.handleVersion)
		r.Get("/config", s.handleConfig)
		r.Post("/reconcile", s.handleReconcile)
		r.Get("/events", s.handleEvents)
		r.Get("/stats", s.handleStats)

		r.Get("/binaries", s.handleListBinaries)
		r.Post("/binaries", s.handleCreateBinary)
		r.Get("/binaries/{name}", s.handleGetBinary)
		r.Delete("/binaries/{name}", s.handleDeleteBinary)

		r.Get("/instances", s.handleListInstances)
		r.Post("/instances", s.handleCreateInstance)
		// Static paths before /instances/{name} so they are not captured as a name.
		r.Post("/instances/import", s.handleImportInstance)
		r.Post("/instances/adopt", s.handleAdoptInstance)
		r.Get("/instances/discover", s.handleDiscoverOpenVPN)
		r.Get("/instances/{name}", s.handleGetInstance)
		r.Patch("/instances/{name}", s.handleUpdateInstance)
		r.Delete("/instances/{name}", s.handleDeleteInstance)
		r.Post("/instances/{name}/up", s.handleInstanceUp)
		r.Post("/instances/{name}/down", s.handleInstanceDown)
		r.Post("/instances/{name}/restart", s.handleInstanceRestart)
		r.Get("/instances/{name}/export", s.handleInstanceExport)
		r.Get("/instances/{name}/status", s.handleInstanceStatus)
		r.Post("/instances/{name}/mgmt", s.handleInstanceMgmt)

		r.Get("/instances/{name}/clients", s.handleListClients)
		r.Post("/instances/{name}/clients", s.handleCreateClient)
		r.Get("/instances/{name}/clients/{cn}", s.handleGetClient)
		r.Patch("/instances/{name}/clients/{cn}", s.handleUpdateClient)
		r.Delete("/instances/{name}/clients/{cn}", s.handleDeleteClient)
		r.Post("/instances/{name}/clients/{cn}/suspend", s.handleClientSuspend)
		r.Post("/instances/{name}/clients/{cn}/resume", s.handleClientResume)
		r.Post("/instances/{name}/clients/{cn}/reset-traffic", s.handleClientResetTraffic)
		r.Get("/instances/{name}/clients/{cn}/client-config", s.handleClientConfig)
		r.Post("/instances/{name}/clients/{cn}/profile-link", s.handleCreateProfileLink)
		r.Get("/instances/{name}/clients/{cn}/profile-links", s.handleListProfileLinks)
		r.Post("/instances/{name}/clients/{cn}/issue-cert", s.handleIssueClientCert)
		r.Post("/instances/{name}/issue-server-cert", s.handleIssueServerCert)

		// PKI / mTLS
		r.Get("/pki/cas", s.handleListCAs)
		r.Post("/pki/cas", s.handleCreateCA)
		r.Get("/pki/cas/{name}", s.handleGetCA)
		r.Delete("/pki/cas/{name}", s.handleDeleteCA)
		r.Post("/pki/cas/{name}/rebuild-crl", s.handleRebuildCRL)
		r.Get("/pki/certs", s.handleListCerts)
		r.Post("/pki/certs", s.handleIssueCert)
		r.Get("/pki/certs/{id}", s.handleGetCert)
		r.Post("/pki/certs/{id}/revoke", s.handleRevokeCert)
		r.Post("/pki/certs/{id}/renew", s.handleRenewCert)
		r.Get("/pki/tls-crypt", s.handleListTLSCrypt)
		r.Post("/pki/tls-crypt", s.handleGenerateTLSCrypt)

		// Extensions / custom OpenVPN features
		r.Get("/features", s.handleListFeaturePresets)
		r.Post("/features", s.handleUpsertFeaturePreset)
		r.Delete("/features/{id}", s.handleDeleteFeaturePreset)

		r.Delete("/profile-tokens/{token}", s.handleRevokeProfileLink)
	})
	return r
}

// ForceReconcile runs one reconcile cycle exclusively.
func (s *Server) ForceReconcile(ctx context.Context) error {
	if s.reconciler == nil {
		return nil
	}
	return s.reconciler.RunOnce(ctx)
}
