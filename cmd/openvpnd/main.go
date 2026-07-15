package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/reloadlife/openvpnd/internal/backup"
	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/daemon"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/update"
	"github.com/reloadlife/openvpnd/internal/version"
)

func main() {
	root := &cobra.Command{
		Use:   "openvpnd",
		Short: "OpenVPN management daemon",
	}
	root.AddCommand(versionCmd(), runCmd(), updateCmd(), backupCmd(), restoreCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
}

func runCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDaemon(configPath)
			if err != nil {
				return err
			}
			log := newLogger(cfg.Log.Level, cfg.Log.Format)
			app := daemon.New(cfg, log)
			return app.Run(context.Background())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")
	return cmd
}

func updateCmd() *cobra.Command {
	var check bool
	var ver string
	var repo string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update openvpnd from GitHub Releases",
		Long: `Download a release from GitHub, verify SHA256SUMS when present, and
atomically replace this executable (and openvpnctl in the same directory).

After updating a systemd-managed daemon:

  sudo systemctl restart openvpnd`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return update.Run(cmd.Context(), update.Options{
				Repo:           repo,
				Version:        ver,
				CurrentVersion: version.Version,
				BinaryName:     "openvpnd",
			}, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only check whether a newer version is available")
	cmd.Flags().StringVar(&ver, "version", "", "install specific tag (e.g. v0.2.0); default latest")
	cmd.Flags().StringVar(&repo, "repo", update.DefaultRepo, "GitHub repository owner/name")
	return cmd
}

func backupCmd() *cobra.Command {
	var configPath string
	var out string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a tar.gz backup of state DB, PKI, and confs",
		Long: `Archive production state for disaster recovery:

  - state SQLite DB (+ WAL siblings) and timeseries DB when present
  - openvpn.pki_dir
  - openvpn.conf_dir (generated confs)
  - optional copy of the daemon config file (--config)

Loads daemon config for paths (same as "openvpnd run").`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if out == "" {
				return fmt.Errorf("--out is required")
			}
			cfg, err := config.LoadDaemon(configPath)
			if err != nil {
				return err
			}
			ts := cfg.DB.TimeseriesPath
			if ts == "" {
				ts = db.DefaultTimeseriesPath(cfg.DB.Path)
			}
			res, err := backup.Backup(backup.BackupOpts{
				Out:            out,
				DBPath:         cfg.DB.Path,
				TimeseriesPath: ts,
				PKIDir:         cfg.OpenVPN.PKIDir,
				ConfDir:        cfg.OpenVPN.ConfDir,
				ConfigPath:     configPath,
				Version:        version.Version,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "backup written: %s (%d bytes)\n", res.Path, res.Bytes)
			fmt.Fprintf(cmd.OutOrStdout(), "  host=%s version=%s ts=%s\n",
				res.Manifest.Host, res.Manifest.Version, res.Manifest.Timestamp)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output tar.gz path (required)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to daemon config (for db/pki/conf paths; included in archive when set)")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func restoreCmd() *cobra.Command {
	var configPath string
	var in string
	var force bool
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore state DB, PKI, and confs from a backup archive",
		Long: `Extract a backup created by "openvpnd backup".

Destinations come from the daemon config (same paths as "openvpnd run").
Non-empty destinations require --force.

Stop the daemon before restore:

  systemctl stop openvpnd
  openvpnd restore --in /var/backups/openvpnd-….tar.gz --config /etc/openvpnd/config.yaml
  systemctl start openvpnd`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if in == "" {
				return fmt.Errorf("--in is required")
			}
			cfg, err := config.LoadDaemon(configPath)
			if err != nil {
				return err
			}
			ts := cfg.DB.TimeseriesPath
			if ts == "" {
				ts = db.DefaultTimeseriesPath(cfg.DB.Path)
			}
			man, err := backup.Restore(backup.RestoreOpts{
				In:             in,
				DBPath:         cfg.DB.Path,
				TimeseriesPath: ts,
				PKIDir:         cfg.OpenVPN.PKIDir,
				ConfDir:        cfg.OpenVPN.ConfDir,
				ConfigPath:     configPath,
				Force:          force,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "restore ok: host=%s version=%s ts=%s\n",
				man.Host, man.Version, man.Timestamp)
			fmt.Fprintf(cmd.OutOrStdout(), "  db=%s pki=%s conf=%s\n",
				cfg.DB.Path, cfg.OpenVPN.PKIDir, cfg.OpenVPN.ConfDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&in, "in", "", "input tar.gz path (required)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to daemon config (for restore destinations)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite non-empty destinations")
	_ = cmd.MarkFlagRequired("in")
	return cmd
}

func newLogger(level, format string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn", "warning":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lv}
	var h slog.Handler
	if strings.ToLower(format) == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
