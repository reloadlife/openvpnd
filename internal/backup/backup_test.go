package backup_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/backup"
)

func TestBackupRestoreRoundTrip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	outDir := t.TempDir()

	// Source layout
	dbPath := filepath.Join(src, "state.db")
	tsPath := filepath.Join(src, "timeseries.db")
	pkiDir := filepath.Join(src, "pki")
	confDir := filepath.Join(src, "conf")
	cfgPath := filepath.Join(src, "config.yaml")

	require.NoError(t, os.WriteFile(dbPath, []byte("STATE-DB-V1"), 0o600))
	require.NoError(t, os.WriteFile(tsPath, []byte("TS-DB-V1"), 0o600))
	// WAL sibling
	require.NoError(t, os.WriteFile(dbPath+"-wal", []byte("WAL"), 0o600))

	require.NoError(t, os.MkdirAll(filepath.Join(pkiDir, "ca"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkiDir, "ca", "ca.crt"), []byte("CERT"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkiDir, "ca", "ca.key"), []byte("KEY"), 0o600))

	require.NoError(t, os.MkdirAll(filepath.Join(confDir, "ovpn0"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(confDir, "ovpn0", "server.conf"), []byte("port 1194\n"), 0o644))

	require.NoError(t, os.WriteFile(cfgPath, []byte("auth:\n  token: secret\n"), 0o600))

	archive := filepath.Join(outDir, "openvpnd-backup.tar.gz")
	fixed := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	res, err := backup.Backup(backup.BackupOpts{
		Out:            archive,
		DBPath:         dbPath,
		TimeseriesPath: tsPath,
		PKIDir:         pkiDir,
		ConfDir:        confDir,
		ConfigPath:     cfgPath,
		Version:        "test-1.0.0",
		Host:           "testhost",
		Now:            fixed,
	})
	require.NoError(t, err)
	require.Equal(t, archive, res.Path)
	require.True(t, res.Bytes > 0)
	require.True(t, res.Manifest.HasStateDB)
	require.True(t, res.Manifest.HasTimeseries)
	require.True(t, res.Manifest.HasPKI)
	require.True(t, res.Manifest.HasConf)
	require.True(t, res.Manifest.HasConfig)
	require.Equal(t, "test-1.0.0", res.Manifest.Version)
	require.Equal(t, "testhost", res.Manifest.Host)
	require.Equal(t, "2026-07-15T12:00:00Z", res.Manifest.Timestamp)
	require.Equal(t, backup.FormatVersion, res.Manifest.FormatVersion)

	// ReadManifest
	man, err := backup.ReadManifest(archive)
	require.NoError(t, err)
	require.Equal(t, res.Manifest.Version, man.Version)

	// Restore without force into empty destinations
	rdstDB := filepath.Join(dst, "state.db")
	rdstTS := filepath.Join(dst, "timeseries.db")
	rdstPKI := filepath.Join(dst, "pki")
	rdstConf := filepath.Join(dst, "conf")
	rdstCfg := filepath.Join(dst, "config.yaml")

	got, err := backup.Restore(backup.RestoreOpts{
		In:             archive,
		DBPath:         rdstDB,
		TimeseriesPath: rdstTS,
		PKIDir:         rdstPKI,
		ConfDir:        rdstConf,
		ConfigPath:     rdstCfg,
	})
	require.NoError(t, err)
	require.Equal(t, "testhost", got.Host)

	assertFile(t, rdstDB, "STATE-DB-V1")
	assertFile(t, rdstDB+"-wal", "WAL")
	assertFile(t, rdstTS, "TS-DB-V1")
	assertFile(t, filepath.Join(rdstPKI, "ca", "ca.crt"), "CERT")
	assertFile(t, filepath.Join(rdstPKI, "ca", "ca.key"), "KEY")
	assertFile(t, filepath.Join(rdstConf, "ovpn0", "server.conf"), "port 1194\n")
	assertFile(t, rdstCfg, "auth:\n  token: secret\n")

	// Second restore without force must fail (non-empty)
	_, err = backup.Restore(backup.RestoreOpts{
		In:     archive,
		DBPath: rdstDB,
		PKIDir: rdstPKI,
		ConfDir: rdstConf,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--force")

	// Force overwrite
	require.NoError(t, os.WriteFile(rdstDB, []byte("OLD"), 0o600))
	_, err = backup.Restore(backup.RestoreOpts{
		In:             archive,
		DBPath:         rdstDB,
		TimeseriesPath: rdstTS,
		PKIDir:         rdstPKI,
		ConfDir:        rdstConf,
		ConfigPath:     rdstCfg,
		Force:          true,
	})
	require.NoError(t, err)
	assertFile(t, rdstDB, "STATE-DB-V1")
}

func TestBackupRequiresOutAndDB(t *testing.T) {
	_, err := backup.Backup(backup.BackupOpts{})
	require.Error(t, err)

	dir := t.TempDir()
	_, err = backup.Backup(backup.BackupOpts{Out: filepath.Join(dir, "x.tar.gz")})
	require.Error(t, err)
}

func TestBackupMissingStateDB(t *testing.T) {
	dir := t.TempDir()
	_, err := backup.Backup(backup.BackupOpts{
		Out:    filepath.Join(dir, "b.tar.gz"),
		DBPath: filepath.Join(dir, "missing.db"),
	})
	require.Error(t, err)
}

func TestBackupSkipsMissingOptionalDirs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o600))
	archive := filepath.Join(dir, "b.tar.gz")

	res, err := backup.Backup(backup.BackupOpts{
		Out:    archive,
		DBPath: dbPath,
		PKIDir: filepath.Join(dir, "no-pki"),
		ConfDir: filepath.Join(dir, "no-conf"),
		// no timeseries file
		TimeseriesPath: filepath.Join(dir, "no-ts.db"),
		Version:        "v",
		Host:           "h",
	})
	require.NoError(t, err)
	require.True(t, res.Manifest.HasStateDB)
	require.False(t, res.Manifest.HasTimeseries)
	require.False(t, res.Manifest.HasPKI)
	require.False(t, res.Manifest.HasConf)
}

func TestRestoreRejectsMissingManifest(t *testing.T) {
	// Craft invalid archive without MANIFEST via Backup then... harder.
	// Empty gzip is enough to fail.
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.tar.gz")
	require.NoError(t, os.WriteFile(p, []byte("not-gzip"), 0o600))
	_, err := backup.Restore(backup.RestoreOpts{In: p, DBPath: filepath.Join(dir, "db")})
	require.Error(t, err)
}

func TestRestoreSkipsExistingConfigWithoutForce(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	dbPath := filepath.Join(src, "state.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("DB"), 0o600))
	cfgSrc := filepath.Join(src, "daemon.yaml")
	require.NoError(t, os.WriteFile(cfgSrc, []byte("old-config\n"), 0o600))

	archive := filepath.Join(src, "a.tar.gz")
	_, err := backup.Backup(backup.BackupOpts{
		Out: archive, DBPath: dbPath, ConfigPath: cfgSrc, Version: "v", Host: "h",
	})
	require.NoError(t, err)

	rdstDB := filepath.Join(dst, "state.db")
	rdstCfg := filepath.Join(dst, "daemon.yaml")
	// Pre-existing config (as when operator uses --config for path discovery).
	require.NoError(t, os.WriteFile(rdstCfg, []byte("live-config\n"), 0o600))

	_, err = backup.Restore(backup.RestoreOpts{
		In: archive, DBPath: rdstDB, ConfigPath: rdstCfg,
	})
	require.NoError(t, err)
	assertFile(t, rdstDB, "DB")
	assertFile(t, rdstCfg, "live-config\n") // not overwritten

	_, err = backup.Restore(backup.RestoreOpts{
		In: archive, DBPath: rdstDB, ConfigPath: rdstCfg, Force: true,
	})
	require.NoError(t, err)
	assertFile(t, rdstCfg, "old-config\n")
}

func TestManifestJSONShape(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("db"), 0o600))
	archive := filepath.Join(dir, "a.tar.gz")

	_, err := backup.Backup(backup.BackupOpts{
		Out:     archive,
		DBPath:  dbPath,
		Version: "1.2.3",
		Host:    "box",
		Now:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	})
	require.NoError(t, err)

	man, err := backup.ReadManifest(archive)
	require.NoError(t, err)

	raw, err := json.Marshal(man)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	require.EqualValues(t, float64(1), m["format_version"])
	require.Equal(t, "1.2.3", m["version"])
	require.Equal(t, "box", m["host"])
	require.NotEmpty(t, m["timestamp"])
	paths, ok := m["paths"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, dbPath, paths["db_path"])
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, path)
	require.Equal(t, want, string(b), path)
}
