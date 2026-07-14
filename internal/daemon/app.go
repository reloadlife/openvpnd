package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/reloadlife/openvpnd/internal/api"
	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/metrics"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/reconcile"
	"github.com/reloadlife/openvpnd/internal/snmp"
	"github.com/reloadlife/openvpnd/internal/stats"
)

// App is the openvpnd process.
type App struct {
	cfg *config.DaemonConfig
	log *slog.Logger
}

// New creates an App.
func New(cfg *config.DaemonConfig, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}
	return &App{cfg: cfg, log: log}
}

// Run starts the daemon until signal.
func (a *App) Run(ctx context.Context) error {
	store, err := db.OpenWithOptions(db.OpenOptions{
		Path:           a.cfg.DB.Path,
		TimeseriesPath: a.cfg.DB.TimeseriesPath,
	})
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = store.Close() }()
	a.log.Info("sqlite open",
		"state", a.cfg.DB.Path,
		"timeseries", store.TimeseriesPath(),
	)

	if err := store.EnsureBinaryDefaults(ctx, a.cfg.OpenVPN.Binaries); err != nil {
		return fmt.Errorf("seed binaries: %w", err)
	}

	var backend ovpnbackend.Backend
	if a.cfg.OpenVPN.UseMockBackend {
		a.log.Warn("using mock openvpn backend (explicit use_mock_backend)")
		backend = ovpnbackend.NewMock()
	} else {
		hb, err := ovpnbackend.NewHostBackend(ovpnbackend.HostOptions{
			ConfDir:    a.cfg.OpenVPN.ConfDir,
			RuntimeDir: a.cfg.OpenVPN.RuntimeDir,
			AllowHooks: a.cfg.OpenVPN.AllowHooks,
		})
		if err != nil {
			return fmt.Errorf("open openvpn backend (set openvpn.use_mock_backend: true for airgap/dev): %w", err)
		}
		backend = hb
	}
	defer func() { _ = backend.Close() }()

	for name, path := range a.cfg.OpenVPN.Binaries {
		if ver, err := backend.ProbeBinary(ctx, path); err == nil {
			_ = store.UpdateBinaryVersion(ctx, name, ver)
			a.log.Info("openvpn binary", "name", name, "path", path, "version", ver)
		} else {
			a.log.Warn("openvpn binary probe failed", "name", name, "path", path, "err", err)
		}
	}

	cache := stats.NewCache()
	collector := metrics.New(cache, nil)

	rec := reconcile.New(store, backend, cache, reconcile.Config{
		ConfDir:        a.cfg.OpenVPN.ConfDir,
		RuntimeDir:     a.cfg.OpenVPN.RuntimeDir,
		DefaultBinary:  a.cfg.OpenVPN.DefaultBinary,
		SampleInterval: a.cfg.SampleInterval(),
		AllowHooks:     a.cfg.OpenVPN.AllowHooks,
	}, a.log)
	rec.SetMetrics(collector)

	srvAPI := api.NewServer(store, backend, cache, rec, a.cfg, a.log)
	handler := srvAPI.Router()

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go rec.Loop(ctx, a.cfg.ReconcileInterval())

	var servers []*http.Server
	errCh := make(chan error, 4)

	if a.cfg.Listen.HTTP != "" {
		ln, err := net.Listen("tcp", a.cfg.Listen.HTTP)
		if err != nil {
			return fmt.Errorf("listen http: %w", err)
		}
		s := &http.Server{Handler: handler}
		servers = append(servers, s)
		a.log.Info("http api listening", "addr", a.cfg.Listen.HTTP)
		go func() { errCh <- s.Serve(ln) }()
	}

	if a.cfg.Listen.Unix != "" {
		if err := os.MkdirAll(filepath.Dir(a.cfg.Listen.Unix), 0o755); err != nil {
			return err
		}
		_ = os.Remove(a.cfg.Listen.Unix)
		ln, err := net.Listen("unix", a.cfg.Listen.Unix)
		if err != nil {
			return fmt.Errorf("listen unix: %w", err)
		}
		_ = os.Chmod(a.cfg.Listen.Unix, 0o660)
		s := &http.Server{Handler: handler}
		servers = append(servers, s)
		a.log.Info("unix api listening", "path", a.cfg.Listen.Unix)
		go func() { errCh <- s.Serve(ln) }()
	}

	if a.cfg.Listen.Metrics != "" && a.cfg.Listen.Metrics != a.cfg.Listen.HTTP {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		ln, err := net.Listen("tcp", a.cfg.Listen.Metrics)
		if err != nil {
			return fmt.Errorf("listen metrics: %w", err)
		}
		s := &http.Server{Handler: mux}
		servers = append(servers, s)
		a.log.Info("metrics listening", "addr", a.cfg.Listen.Metrics)
		go func() { errCh <- s.Serve(ln) }()
	}

	var snmpAgent *snmp.Agent
	if a.cfg.SNMP.Enabled {
		snmpAgent = snmp.NewAgent(a.cfg.SNMP.Listen, a.cfg.SNMP.Community, a.cfg.SNMP.EnterpriseOID, cache, a.log)
		if err := snmpAgent.Start(); err != nil {
			a.log.Error("snmp start failed", "err", err)
		} else {
			defer func() { _ = snmpAgent.Close() }()
		}
	}

	select {
	case <-ctx.Done():
		a.log.Info("shutting down")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}

	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	for _, s := range servers {
		_ = s.Shutdown(shutdownCtx)
	}
	return nil
}
