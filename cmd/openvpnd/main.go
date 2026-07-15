package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/daemon"
	"github.com/reloadlife/openvpnd/internal/update"
	"github.com/reloadlife/openvpnd/internal/version"
)

func main() {
	root := &cobra.Command{
		Use:   "openvpnd",
		Short: "OpenVPN management daemon",
	}
	root.AddCommand(versionCmd(), runCmd(), updateCmd())
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
