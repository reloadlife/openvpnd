package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reloadlife/openvpnd/internal/backup"
	"github.com/reloadlife/openvpnd/internal/version"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

// handleSystemBackup writes a backup archive to a host path (preferred) or streams it.
//
// Body (preferred):
//
//	{ "path": "/var/backups/openvpnd-….tar.gz" }
//
// Empty body or missing path streams application/gzip to the client.
// Auth: bearer + admin role (operators blocked by roleGuard for /v1/system/* mutations).
func (s *Server) handleSystemBackup(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.SystemBackupRequest
	// Empty body is allowed (stream mode). Non-empty JSON must decode.
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			// ContentLength -1 (unknown) with empty/EOF body → stream mode.
			if r.ContentLength > 0 || !strings.Contains(err.Error(), "EOF") {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
		}
	}

	dbPath := s.cfg.DB.Path
	tsPath := s.cfg.DB.TimeseriesPath
	if s.store != nil {
		// Best-effort WAL checkpoint so the on-disk state file is consistent.
		_ = s.store.CheckpointWAL()
		if tsPath == "" {
			tsPath = s.store.TimeseriesPath()
		}
	}

	opts := backup.BackupOpts{
		DBPath:         dbPath,
		TimeseriesPath: tsPath,
		PKIDir:         s.cfg.OpenVPN.PKIDir,
		ConfDir:        s.cfg.OpenVPN.ConfDir,
		Version:        version.Version,
	}

	// Prefer host path write.
	out := strings.TrimSpace(req.Path)
	if out != "" {
		if !filepath.IsAbs(out) {
			writeError(w, http.StatusBadRequest, "invalid_path", "path must be absolute")
			return
		}
		low := strings.ToLower(out)
		if !strings.HasSuffix(low, ".tar.gz") && !strings.HasSuffix(low, ".tgz") {
			writeError(w, http.StatusBadRequest, "invalid_path", "path must end with .tar.gz or .tgz")
			return
		}
		opts.Out = out
		res, err := backup.Backup(opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
			return
		}
		if s.store != nil {
			meta, _ := json.Marshal(map[string]any{"path": res.Path, "bytes": res.Bytes})
			_ = s.store.AddEvent(r.Context(), "info", "system.backup", "", "", "backup written", string(meta))
		}
		writeJSON(w, http.StatusOK, pkgapi.SystemBackupResponse{
			Path:    res.Path,
			Bytes:   res.Bytes,
			Version: res.Manifest.Version,
			Host:    res.Manifest.Host,
			TS:      res.Manifest.Timestamp,
			Status:  "ok",
		})
		return
	}

	// Stream mode: write to a temp file then stream.
	tmpDir, err := os.MkdirTemp("", "openvpnd-backup-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}
	defer os.RemoveAll(tmpDir)
	tmp := filepath.Join(tmpDir, "backup.tar.gz")
	opts.Out = tmp
	res, err := backup.Backup(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}

	f, err := os.Open(res.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}
	defer f.Close()

	mod := time.Now()
	if t, err := time.Parse(time.RFC3339, res.Manifest.Timestamp); err == nil {
		mod = t
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="openvpnd-backup.tar.gz"`)
	w.Header().Set("X-Openvpnd-Backup-Version", res.Manifest.Version)
	w.Header().Set("X-Openvpnd-Backup-Host", res.Manifest.Host)
	http.ServeContent(w, r, "openvpnd-backup.tar.gz", mod, f)
	if s.store != nil {
		_ = s.store.AddEvent(r.Context(), "info", "system.backup", "", "", "backup streamed", `{}`)
	}
}
