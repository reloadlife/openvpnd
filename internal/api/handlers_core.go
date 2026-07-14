package api

import (
	"net/http"

	"github.com/reloadlife/openvpnd/internal/version"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pkgapi.VersionInfo{
		Version: version.Version,
		Commit:  version.Commit,
		Date:    version.Date,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := pkgapi.DaemonConfig{
		HTTPListen:         s.cfg.Listen.HTTP,
		UnixListen:         s.cfg.Listen.Unix,
		MetricsListen:      s.cfg.Listen.Metrics,
		Persistence:        s.cfg.OpenVPN.Persistence,
		ConfDir:            s.cfg.OpenVPN.ConfDir,
		RuntimeDir:         s.cfg.OpenVPN.RuntimeDir,
		PKIDir:             s.cfg.OpenVPN.PKIDir,
		DefaultBinary:      s.cfg.OpenVPN.DefaultBinary,
		SampleInterval:     s.cfg.OpenVPN.SampleInterval,
		ReconcileInterval:  s.cfg.OpenVPN.ReconcileInterval,
		AllowHooks:         s.cfg.OpenVPN.AllowHooks,
		DBPath:             s.cfg.DB.Path,
		TimeseriesPath:     s.cfg.DB.TimeseriesPath,
		PublicBaseURL:      s.cfg.PublicBase(),
		ProfileLinkTTL:     s.cfg.ProfileLinks.DefaultTTL,
		ProfileLinkMaxUses: s.cfg.ProfileLinks.DefaultMaxUses,
		ReadOnly:           s.cfg.ReadOnly,
	}
	if s.store != nil && cfg.TimeseriesPath == "" {
		cfg.TimeseriesPath = s.store.TimeseriesPath()
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	if err := s.ForceReconcile(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "reconcile_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ev, err := s.store.ListEvents(r.Context(), 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.Event, 0, len(ev))
	for _, e := range ev {
		out = append(out, pkgapi.Event{
			ID: e.ID, TS: e.TS, Level: e.Level, Kind: e.Kind,
			Instance: e.Instance, ClientCN: e.ClientCN,
			Message: e.Message, Meta: e.Meta,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	rx, tx, rxBps, txBps, up, total := s.cache.Snapshot()
	writeJSON(w, http.StatusOK, pkgapi.Stats{
		InstancesTotal: total,
		InstancesUp:    up,
		RxBytes:        rx,
		TxBytes:        tx,
		RxBps:          rxBps,
		TxBps:          txBps,
	})
}
