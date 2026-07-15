// Package backup creates and restores openvpnd state archives (tar.gz).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reloadlife/openvpnd/internal/db"
)

// FormatVersion is the MANIFEST.json schema version.
const FormatVersion = 1

// BackupOpts controls archive creation.
type BackupOpts struct {
	// Out is the destination .tar.gz path (required).
	Out string
	// DBPath is the state SQLite path (required for a useful backup).
	DBPath string
	// TimeseriesPath is the timeseries SQLite path. Empty uses db.DefaultTimeseriesPath.
	// Skipped when the path is memory-backed or the file does not exist.
	TimeseriesPath string
	// PKIDir is openvpn.pki_dir.
	PKIDir string
	// ConfDir is openvpn.conf_dir (generated confs).
	ConfDir string
	// ConfigPath, when set and present on disk, is included under config/.
	ConfigPath string
	// Version is the openvpnd version string written into MANIFEST.json.
	Version string
	// Host overrides os.Hostname (tests).
	Host string
	// Now overrides time.Now (tests).
	Now time.Time
}

// RestoreOpts controls archive extraction.
type RestoreOpts struct {
	// In is the source .tar.gz path (required).
	In string
	// Destinations (current host layout). Empty fields fall back to MANIFEST paths.
	DBPath         string
	TimeseriesPath string
	PKIDir         string
	ConfDir        string
	ConfigPath     string
	// Force overwrites non-empty destinations.
	Force bool
}

// Manifest describes an archive.
type Manifest struct {
	FormatVersion  int           `json:"format_version"`
	Version        string        `json:"version"`
	Host           string        `json:"host"`
	Timestamp      string        `json:"timestamp"`
	Paths          ManifestPaths `json:"paths"`
	HasStateDB     bool          `json:"has_state_db"`
	HasTimeseries  bool          `json:"has_timeseries"`
	HasPKI         bool          `json:"has_pki"`
	HasConf        bool          `json:"has_conf"`
	HasConfig      bool          `json:"has_config"`
	ConfigBasename string        `json:"config_basename,omitempty"`
}

// ManifestPaths records the source paths at backup time.
type ManifestPaths struct {
	DBPath         string `json:"db_path"`
	TimeseriesPath string `json:"timeseries_path,omitempty"`
	PKIDir         string `json:"pki_dir"`
	ConfDir        string `json:"conf_dir"`
	ConfigPath     string `json:"config_path,omitempty"`
}

// Result is returned after a successful Backup.
type Result struct {
	Path     string   `json:"path"`
	Manifest Manifest `json:"manifest"`
	Bytes    int64    `json:"bytes"`
}

// Backup creates a tar.gz of state DB, optional timeseries, PKI, confs, and config.
func Backup(opts BackupOpts) (*Result, error) {
	if strings.TrimSpace(opts.Out) == "" {
		return nil, fmt.Errorf("backup: --out is required")
	}
	if strings.TrimSpace(opts.DBPath) == "" {
		return nil, fmt.Errorf("backup: db path is required")
	}

	tsPath := opts.TimeseriesPath
	if tsPath == "" {
		tsPath = db.DefaultTimeseriesPath(opts.DBPath)
	}

	host := opts.Host
	if host == "" {
		h, err := os.Hostname()
		if err != nil {
			host = "unknown"
		} else {
			host = h
		}
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	man := Manifest{
		FormatVersion: FormatVersion,
		Version:       opts.Version,
		Host:          host,
		Timestamp:     now.Format(time.RFC3339),
		Paths: ManifestPaths{
			DBPath:         opts.DBPath,
			TimeseriesPath: tsPath,
			PKIDir:         opts.PKIDir,
			ConfDir:        opts.ConfDir,
			ConfigPath:     opts.ConfigPath,
		},
	}

	if err := os.MkdirAll(filepath.Dir(opts.Out), 0o755); err != nil {
		return nil, fmt.Errorf("backup: create out dir: %w", err)
	}

	// Write to a temp file then rename for atomicity.
	tmp := opts.Out + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("backup: create archive: %w", err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Track files we'll add after building the rest so MANIFEST is complete.
	addFile := func(name string, path string) error {
		return writeFileToTar(tw, name, path)
	}
	addTree := func(prefix, dir string) (bool, error) {
		return writeTreeToTar(tw, prefix, dir)
	}

	// State DB (+ WAL siblings)
	if isMemoryPath(opts.DBPath) {
		return nil, fmt.Errorf("backup: cannot backup in-memory state db %q", opts.DBPath)
	}
	if _, err := os.Stat(opts.DBPath); err != nil {
		return nil, fmt.Errorf("backup: state db %s: %w", opts.DBPath, err)
	}
	if err := addFile("db/state.db", opts.DBPath); err != nil {
		return nil, err
	}
	man.HasStateDB = true
	_ = addSQLiteSiblings(addFile, "db/state.db", opts.DBPath)

	// Timeseries (optional)
	if !isMemoryPath(tsPath) {
		if st, err := os.Stat(tsPath); err == nil && !st.IsDir() {
			if err := addFile("db/timeseries.db", tsPath); err != nil {
				return nil, err
			}
			man.HasTimeseries = true
			_ = addSQLiteSiblings(addFile, "db/timeseries.db", tsPath)
		}
	}

	// PKI
	if d := strings.TrimSpace(opts.PKIDir); d != "" {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			has, err := addTree("pki", d)
			if err != nil {
				return nil, err
			}
			man.HasPKI = has
		}
	}

	// Conf
	if d := strings.TrimSpace(opts.ConfDir); d != "" {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			has, err := addTree("conf", d)
			if err != nil {
				return nil, err
			}
			man.HasConf = has
		}
	}

	// Optional config file
	if p := strings.TrimSpace(opts.ConfigPath); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			base := filepath.Base(p)
			if err := addFile(filepath.Join("config", base), p); err != nil {
				return nil, err
			}
			man.HasConfig = true
			man.ConfigBasename = base
		}
	}

	// MANIFEST.json last so flags are complete (tar readers scan whole archive).
	manBytes, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("backup: marshal manifest: %w", err)
	}
	manBytes = append(manBytes, '\n')
	if err := writeBytesToTar(tw, "MANIFEST.json", manBytes, now); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("backup: close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("backup: close gzip: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("backup: close file: %w", err)
	}
	if err := os.Rename(tmp, opts.Out); err != nil {
		return nil, fmt.Errorf("backup: rename: %w", err)
	}
	ok = true

	info, err := os.Stat(opts.Out)
	var size int64
	if err == nil {
		size = info.Size()
	}
	return &Result{Path: opts.Out, Manifest: man, Bytes: size}, nil
}

// Restore extracts an archive to the configured destinations.
// Destinations that already exist and are non-empty require Force.
func Restore(opts RestoreOpts) (*Manifest, error) {
	if strings.TrimSpace(opts.In) == "" {
		return nil, fmt.Errorf("restore: --in is required")
	}

	f, err := os.Open(opts.In)
	if err != nil {
		return nil, fmt.Errorf("restore: open archive: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("restore: gzip: %w", err)
	}
	defer gr.Close()

	// First pass: load entire archive into memory map (backups are modest).
	// Alternative would be extract-to-temp; memory map is simpler for unit tests.
	entries, man, err := readArchive(gr)
	if err != nil {
		return nil, err
	}

	dbPath := firstNonEmpty(opts.DBPath, man.Paths.DBPath)
	tsPath := firstNonEmpty(opts.TimeseriesPath, man.Paths.TimeseriesPath)
	if tsPath == "" && dbPath != "" {
		tsPath = db.DefaultTimeseriesPath(dbPath)
	}
	pkiDir := firstNonEmpty(opts.PKIDir, man.Paths.PKIDir)
	confDir := firstNonEmpty(opts.ConfDir, man.Paths.ConfDir)
	cfgPath := firstNonEmpty(opts.ConfigPath, man.Paths.ConfigPath)

	// Destination checks
	if man.HasStateDB {
		if err := checkFileDest(dbPath, opts.Force); err != nil {
			return nil, fmt.Errorf("restore: state db: %w", err)
		}
	}
	if man.HasTimeseries && tsPath != "" && !isMemoryPath(tsPath) {
		if err := checkFileDest(tsPath, opts.Force); err != nil {
			return nil, fmt.Errorf("restore: timeseries db: %w", err)
		}
	}
	if man.HasPKI && pkiDir != "" {
		if err := checkDirDest(pkiDir, opts.Force); err != nil {
			return nil, fmt.Errorf("restore: pki dir: %w", err)
		}
	}
	if man.HasConf && confDir != "" {
		if err := checkDirDest(confDir, opts.Force); err != nil {
			return nil, fmt.Errorf("restore: conf dir: %w", err)
		}
	}
	// Config is optional. When the operator passes --config to discover destinations,
	// that file already exists — do not block the whole restore; only overwrite with --force.
	restoreConfig := false
	if man.HasConfig && cfgPath != "" {
		if err := checkFileDest(cfgPath, opts.Force); err != nil {
			if opts.Force {
				return nil, fmt.Errorf("restore: config: %w", err)
			}
			// skip existing config without --force
			restoreConfig = false
		} else {
			restoreConfig = true
		}
	}

	// Write files
	if man.HasStateDB {
		if err := writeEntry(entries, "db/state.db", dbPath); err != nil {
			return nil, err
		}
		_ = writeOptionalSibling(entries, "db/state.db-wal", dbPath+"-wal")
		_ = writeOptionalSibling(entries, "db/state.db-shm", dbPath+"-shm")
	}
	if man.HasTimeseries && tsPath != "" && !isMemoryPath(tsPath) {
		if _, ok := entries["db/timeseries.db"]; ok {
			if err := writeEntry(entries, "db/timeseries.db", tsPath); err != nil {
				return nil, err
			}
			_ = writeOptionalSibling(entries, "db/timeseries.db-wal", tsPath+"-wal")
			_ = writeOptionalSibling(entries, "db/timeseries.db-shm", tsPath+"-shm")
		}
	}
	if man.HasPKI && pkiDir != "" {
		if err := writePrefix(entries, "pki/", pkiDir); err != nil {
			return nil, err
		}
	}
	if man.HasConf && confDir != "" {
		if err := writePrefix(entries, "conf/", confDir); err != nil {
			return nil, err
		}
	}
	if restoreConfig {
		base := man.ConfigBasename
		if base == "" {
			base = filepath.Base(cfgPath)
		}
		src := filepath.Join("config", base)
		// Fall back: any single file under config/
		if _, ok := entries[src]; !ok {
			for k := range entries {
				if strings.HasPrefix(k, "config/") && !strings.HasSuffix(k, "/") {
					src = k
					break
				}
			}
		}
		if err := writeEntry(entries, src, cfgPath); err != nil {
			return nil, err
		}
	}

	return &man, nil
}

// ReadManifest opens an archive and returns only MANIFEST.json.
func ReadManifest(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	_, man, err := readArchive(gr)
	if err != nil {
		return nil, err
	}
	return &man, nil
}

func readArchive(r io.Reader) (map[string][]byte, Manifest, error) {
	tr := tar.NewReader(r)
	entries := make(map[string][]byte)
	var man Manifest
	var foundMan bool

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Manifest{}, fmt.Errorf("restore: tar: %w", err)
		}
		name := filepath.ToSlash(hdr.Name)
		name = strings.TrimPrefix(name, "./")
		if name == "" || name == "." {
			continue
		}
		// Reject path traversal
		if strings.Contains(name, "..") {
			return nil, Manifest{}, fmt.Errorf("restore: unsafe path %q", name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			entries[strings.TrimSuffix(name, "/")+"/"] = nil
		case tar.TypeReg, tar.TypeRegA:
			if hdr.Size > 512<<20 {
				return nil, Manifest{}, fmt.Errorf("restore: entry %s too large", name)
			}
			var rd io.Reader = tr
			if hdr.Size > 0 {
				rd = io.LimitReader(tr, hdr.Size)
			}
			b, err := io.ReadAll(rd)
			if err != nil {
				return nil, Manifest{}, fmt.Errorf("restore: read %s: %w", name, err)
			}
			if name == "MANIFEST.json" {
				if err := json.Unmarshal(b, &man); err != nil {
					return nil, Manifest{}, fmt.Errorf("restore: parse MANIFEST.json: %w", err)
				}
				foundMan = true
			}
			entries[name] = b
		}
	}
	if !foundMan {
		return nil, Manifest{}, fmt.Errorf("restore: MANIFEST.json missing from archive")
	}
	return entries, man, nil
}

func writeFileToTar(tw *tar.Writer, name, path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("backup: stat %s: %w", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", path, err)
	}
	defer f.Close()

	hdr, err := tar.FileInfoHeader(st, "")
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(name)
	// Normalize mode for portability (keep owner r/w for secrets).
	if st.Mode().Perm()&0o077 == 0 {
		hdr.Mode = int64(st.Mode().Perm())
	} else {
		hdr.Mode = 0o600
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: header %s: %w", name, err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("backup: write %s: %w", name, err)
	}
	return nil
}

func writeBytesToTar(tw *tar.Writer, name string, b []byte, mod time.Time) error {
	hdr := &tar.Header{
		Name:    filepath.ToSlash(name),
		Mode:    0o644,
		Size:    int64(len(b)),
		ModTime: mod,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

func writeTreeToTar(tw *tar.Writer, prefix, root string) (bool, error) {
	root = filepath.Clean(root)
	var any bool
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			// Root dir itself — ensure prefix entry exists as dir if empty later.
			return nil
		}
		name := filepath.ToSlash(filepath.Join(prefix, rel))
		if info.IsDir() {
			hdr := &tar.Header{
				Name:     name + "/",
				Mode:     0o755,
				Typeflag: tar.TypeDir,
				ModTime:  info.ModTime(),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			any = true
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil // skip sockets/devices/symlinks for safety
		}
		if err := writeFileToTar(tw, name, path); err != nil {
			return err
		}
		any = true
		return nil
	})
	return any, err
}

func addSQLiteSiblings(add func(name, path string) error, archiveBase, dbPath string) error {
	for _, suf := range []string{"-wal", "-shm"} {
		p := dbPath + suf
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			if err := add(archiveBase+suf, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkFileDest(path string, force bool) error {
	if path == "" {
		return fmt.Errorf("destination path empty")
	}
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if !force {
		return fmt.Errorf("%s exists (use --force to overwrite)", path)
	}
	return nil
}

func checkDirDest(dir string, force bool) error {
	if dir == "" {
		return fmt.Errorf("destination dir empty")
	}
	st, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if force {
		return nil
	}
	// Empty dir is OK.
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(ents) > 0 {
		return fmt.Errorf("%s is not empty (use --force to overwrite)", dir)
	}
	return nil
}

func writeEntry(entries map[string][]byte, name, dest string) error {
	b, ok := entries[name]
	if !ok {
		return fmt.Errorf("restore: missing archive entry %s", name)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	// Prefer restrictive perms for DB/keys; config/conf may be wider later.
	mode := os.FileMode(0o600)
	return os.WriteFile(dest, b, mode)
}

func writeOptionalSibling(entries map[string][]byte, name, dest string) error {
	b, ok := entries[name]
	if !ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, b, 0o600)
}

func writePrefix(entries map[string][]byte, prefix, destRoot string) error {
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return err
	}
	for name, data := range entries {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "" {
			continue
		}
		// Directory marker
		if strings.HasSuffix(name, "/") || data == nil {
			dir := filepath.Join(destRoot, filepath.FromSlash(strings.TrimSuffix(rel, "/")))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			continue
		}
		target := filepath.Join(destRoot, filepath.FromSlash(rel))
		// Ensure we stay under destRoot
		clean := filepath.Clean(target)
		if !strings.HasPrefix(clean, filepath.Clean(destRoot)+string(os.PathSeparator)) && clean != filepath.Clean(destRoot) {
			return fmt.Errorf("restore: path escapes dest: %s", name)
		}
		if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		// conf files are often 644; pki keys stay 600
		if strings.HasPrefix(prefix, "conf/") || prefix == "conf/" {
			mode = 0o644
		}
		if err := os.WriteFile(clean, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func isMemoryPath(path string) bool {
	if path == "" || path == ":memory:" {
		return true
	}
	return strings.HasPrefix(path, "file:") && strings.Contains(path, "mode=memory")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
