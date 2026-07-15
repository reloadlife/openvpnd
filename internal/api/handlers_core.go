package api

import (
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/version"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReadyzResponse is the enhanced readiness payload (also mirrors pkg/api.ReadyStatus).
type ReadyzResponse struct {
	Status string            `json:"status"` // ok | degraded | fail
	Checks map[string]string `json:"checks"`
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	status := "ok"

	// db
	if s.store == nil {
		checks["db"] = "fail"
		status = "fail"
	} else if err := s.store.Ping(r.Context()); err != nil {
		checks["db"] = "fail"
		status = "fail"
	} else {
		checks["db"] = "ok"
	}

	// default_binary path exists
	binPath := ""
	if s.cfg != nil {
		binPath = s.cfg.DefaultBinaryPath()
	}
	if binPath == "" {
		checks["default_binary"] = "missing"
		if status != "fail" {
			status = "degraded"
		}
	} else if st, err := os.Stat(binPath); err != nil || st.IsDir() {
		checks["default_binary"] = "missing"
		if status != "fail" {
			status = "degraded"
		}
	} else {
		checks["default_binary"] = "ok"
	}

	// conf_dir writable
	confDir := ""
	if s.cfg != nil {
		confDir = s.cfg.OpenVPN.ConfDir
	}
	checks["conf_dir"] = probeDirWritable(confDir)
	if checks["conf_dir"] == "ro" && status != "fail" {
		status = "degraded"
	}

	// pki_dir exists or creatable
	pkiDir := ""
	if s.cfg != nil {
		pkiDir = s.cfg.OpenVPN.PKIDir
	}
	checks["pki_dir"] = probeDirExistsOrCreatable(pkiDir)
	if checks["pki_dir"] == "missing" && status != "fail" {
		status = "degraded"
	}

	// backend kind (informational)
	checks["backend"] = s.backendKind()

	// bandwidth / tc readiness (when enforcement needs the host tool)
	bwMode := "off"
	if s.cfg != nil {
		bwMode = strings.ToLower(strings.TrimSpace(s.cfg.OpenVPN.BandwidthEnforcement))
	}
	checks["bandwidth_mode"] = bwMode
	switch bwMode {
	case "tc", "htb":
		if _, err := exec.LookPath("tc"); err != nil {
			checks["bandwidth_tc"] = "missing"
			if status != "fail" {
				status = "degraded"
			}
		} else {
			checks["bandwidth_tc"] = "ok"
		}
	case "shaper", "log", "off", "", "none":
		checks["bandwidth_tc"] = "n/a"
	default:
		checks["bandwidth_tc"] = "n/a"
	}

	// webhooks (informational)
	if s.cfg != nil && s.cfg.Webhooks.Enabled && strings.TrimSpace(s.cfg.Webhooks.URL) != "" {
		checks["webhooks"] = "enabled"
	} else {
		checks["webhooks"] = "disabled"
	}

	code := http.StatusOK
	if status == "fail" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, ReadyzResponse{Status: status, Checks: checks})
}

func (s *Server) backendKind() string {
	if s.cfg != nil && s.cfg.OpenVPN.UseMockBackend {
		return "mock"
	}
	if _, ok := s.backend.(*ovpnbackend.Mock); ok {
		return "mock"
	}
	return "host"
}

// probeDirWritable returns "ok" if dir is writable, else "ro".
func probeDirWritable(dir string) string {
	if dir == "" {
		return "ro"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "ro"
	}
	f, err := os.CreateTemp(dir, ".openvpnd-readyz-*")
	if err != nil {
		return "ro"
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return "ok"
}

// probeDirExistsOrCreatable returns "ok" if dir exists or can be created, else "missing".
func probeDirExistsOrCreatable(dir string) string {
	if dir == "" {
		return "missing"
	}
	if st, err := os.Stat(dir); err == nil {
		if st.IsDir() {
			return "ok"
		}
		return "missing"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "missing"
	}
	return "ok"
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pkgapi.VersionInfo{
		Version: version.Version,
		Commit:  version.Commit,
		Date:    version.Date,
	})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := pkgapi.SystemInfo{
		Version:       version.Version,
		Commit:        version.Commit,
		Date:          version.Date,
		Production:    s.cfg != nil && s.cfg.Production,
		BandwidthMode: "off",
		Backend:       s.backendKind(),
	}
	if s.cfg != nil {
		info.BandwidthMode = s.cfg.OpenVPN.BandwidthEnforcement
		if info.BandwidthMode == "" {
			info.BandwidthMode = "off"
		}
		info.ListenHTTP = s.cfg.Listen.HTTP
		info.ListenUnix = s.cfg.Listen.Unix
		info.ListenMetrics = s.cfg.Listen.Metrics
		info.ReadOnly = s.cfg.ReadOnly
		info.PKIDir = s.cfg.OpenVPN.PKIDir
		info.ConfDir = s.cfg.OpenVPN.ConfDir
		info.RuntimeDir = s.cfg.OpenVPN.RuntimeDir
		info.Persistence = s.cfg.OpenVPN.Persistence
		info.PublicBaseURL = s.cfg.PublicBase()
		info.DBPath = s.cfg.DB.Path
		info.TimeseriesPath = s.cfg.DB.TimeseriesPath
	}
	if s.store != nil {
		if info.TimeseriesPath == "" {
			info.TimeseriesPath = s.store.TimeseriesPath()
		}
		info.Ready.DB = s.store.Ping(r.Context()) == nil
	}
	if info.TimeseriesPath == "" && info.DBPath != "" {
		info.TimeseriesPath = db.DefaultTimeseriesPath(info.DBPath)
	}
	info.Ready.PKIDir = dirExists(info.PKIDir)
	info.Ready.ConfDir = dirExists(info.ConfDir)
	info.Ready.StateDB = fileExists(info.DBPath)
	if info.TimeseriesPath != "" && info.TimeseriesPath != ":memory:" {
		info.Ready.TimeseriesDB = fileExists(info.TimeseriesPath)
	}
	if info.Ready.DB {
		info.Status = "ok"
	} else {
		info.Status = "degraded"
	}
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	if s.cache != nil {
		_, _, _, _, up, total := s.cache.Snapshot()
		info.InstancesTotal = &total
		info.InstancesUp = &up
	} else if s.store != nil {
		if list, err := s.store.ListInstances(r.Context()); err == nil {
			n := len(list)
			info.InstancesTotal = &n
		}
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := pkgapi.DaemonConfig{
		HTTPListen:         s.cfg.Listen.HTTP,
		UnixListen:         s.cfg.Listen.Unix,
		MetricsListen:      s.cfg.Listen.Metrics,
		SNMPEnabled:        s.cfg.SNMP.Enabled,
		SNMPListen:         s.cfg.SNMP.Listen,
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
		Production:         s.cfg.Production,
		Role:               RoleFromContext(r.Context()),
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

// dirExists reports whether p is an existing directory.
func dirExists(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// fileExists reports whether p is an existing regular file.
func fileExists(p string) bool {
	if p == "" || p == ":memory:" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

