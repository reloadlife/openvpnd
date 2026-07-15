package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/tui"
	"github.com/reloadlife/openvpnd/internal/update"
	"github.com/reloadlife/openvpnd/internal/version"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func main() {
	var configPath string
	root := &cobra.Command{
		Use:   "openvpnctl",
		Short: "OpenVPN control panel (TUI + CLI)",
		Long:  "Full-screen TUI by default. Subcommands for scripting.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(configPath)
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config file")
	root.AddCommand(
		versionCmd(),
		updateCmd(),
		tuiCmd(&configPath),
		instanceCmd(&configPath),
		clientCmd(&configPath),
		binaryCmd(&configPath),
		pkiCmd(&configPath),
		statsCmd(&configPath),
		reconcileCmd(&configPath),
		eventsCmd(&configPath),
		systemCmd(&configPath),
	)
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

func updateCmd() *cobra.Command {
	var check bool
	var ver string
	var repo string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update openvpnctl from GitHub Releases",
		Long: `Download a release from GitHub, verify SHA256SUMS when present, and
atomically replace this executable (and openvpnd in the same directory).

If openvpnd was updated under systemd:

  sudo systemctl restart openvpnd`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return update.Run(cmd.Context(), update.Options{
				Repo:           repo,
				Version:        ver,
				CurrentVersion: version.Version,
				BinaryName:     "openvpnctl",
			}, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only check whether a newer version is available")
	cmd.Flags().StringVar(&ver, "version", "", "install specific tag (e.g. v0.2.0); default latest")
	cmd.Flags().StringVar(&repo, "repo", update.DefaultRepo, "GitHub repository owner/name")
	return cmd
}

func tuiCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open full-screen TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(*configPath)
		},
	}
}

func runTUI(configPath string) error {
	cfg, client, err := loadClient(configPath)
	if err != nil {
		return err
	}
	return tui.Run(tui.Config{
		Client:          client,
		Endpoint:        cfg.Endpoint(),
		RefreshInterval: cfg.Refresh(),
	})
}

func loadClient(configPath string) (*config.CtlConfig, *pkgapi.Client, error) {
	cfg, err := config.LoadCtl(configPath)
	if err != nil {
		return nil, nil, err
	}
	client, err := pkgapi.NewClient(cfg.Endpoint(), pkgapi.WithToken(cfg.Server.Token))
	if err != nil {
		return nil, nil, err
	}
	return cfg, client, nil
}

func instanceCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "instance", Short: "Instance operations", Aliases: []string{"inst", "i"}}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			list, err := c.ListInstances(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tROLE\tENABLED\tUP\tBINARY\tPORT\tCLIENTS\tRX\tTX")
			for _, i := range list {
				fmt.Fprintf(w, "%s\t%s\t%v\t%v\t%s\t%d\t%d\t%d\t%d\n",
					i.Name, i.Role, i.Enabled, i.Up, i.BinaryName, i.Port, i.ConnectedClients, i.RxBytes, i.TxBytes)
			}
			return w.Flush()
		},
	})
	var role, binary, network, remote, publicEP, pushDNS, topology, proto, caName string
	var port int
	var noCert, noTLSCrypt, createCA bool
	create := &cobra.Command{
		Use:   "create [NAME]",
		Short: "Create an instance (auto name/network/port/PKI when omitted)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			issue := !noCert
			genTC := !noTLSCrypt
			req := pkgapi.InstanceCreateRequest{
				Name: name, Role: role, BinaryName: binary, Port: port,
				ServerNetwork: network, Topology: topology, Proto: proto,
				Remote: remote, PublicEndpoint: publicEP, PushDNS: splitCSVFlag(pushDNS),
				CAName: caName, CreateCAIfEmpty: createCA,
				IssueServerCert: &issue, GenerateTLSCrypt: &genTC,
			}
			out, err := c.CreateInstance(context.Background(), req)
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	create.Flags().StringVar(&role, "role", "server", "server|client")
	create.Flags().StringVar(&binary, "binary", "", "binary registry name (default: default)")
	create.Flags().IntVar(&port, "port", 0, "listen port (0=auto free from 1194)")
	create.Flags().StringVar(&proto, "proto", "", "udp|tcp|… (default udp)")
	create.Flags().StringVar(&network, "network", "", "server CIDR (empty=auto free 10.x.0.0/24)")
	create.Flags().StringVar(&topology, "topology", "", "subnet|net30|p2p (default subnet)")
	create.Flags().StringVar(&remote, "remote", "", "client remote host[:port[:proto]]")
	create.Flags().StringVar(&publicEP, "public-endpoint", "", "vpn.example.com:1194")
	create.Flags().StringVar(&pushDNS, "push-dns", "", "comma-separated DNS to push")
	create.Flags().StringVar(&caName, "ca", "", "CA for auto issue")
	create.Flags().BoolVar(&createCA, "create-ca", false, "create CA default if none")
	create.Flags().BoolVar(&noCert, "no-cert", false, "do not auto-issue server cert")
	create.Flags().BoolVar(&noTLSCrypt, "no-tls-crypt", false, "do not auto-generate tls-crypt")
	cmd.AddCommand(create)

	cmd.AddCommand(&cobra.Command{
		Use:   "get NAME",
		Short: "Get instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			out, err := c.GetInstance(context.Background(), args[0])
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "delete NAME",
		Short: "Delete instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.DeleteInstance(context.Background(), args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "up NAME",
		Short: "Enable and start instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.InstanceUp(context.Background(), args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "down NAME",
		Short: "Disable and stop instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.InstanceDown(context.Background(), args[0])
		},
	})
	var issueCAName, serverCN string
	var genTC bool
	issueSrv := &cobra.Command{
		Use:   "issue-cert NAME",
		Short: "Issue server cert from CA and wire instance PKI paths",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.IssueServerCert(context.Background(), args[0], pkgapi.IssueServerCertRequest{
				CAName: issueCAName, CommonName: serverCN, GenerateTLSCrypt: genTC,
			})
		},
	}
	issueSrv.Flags().StringVar(&issueCAName, "ca", "", "CA name (default first)")
	issueSrv.Flags().StringVar(&serverCN, "cn", "", "server CN (default public_endpoint host or name)")
	issueSrv.Flags().BoolVar(&genTC, "tls-crypt", true, "generate tls-crypt key for instance")
	cmd.AddCommand(issueSrv)
	return cmd
}

func clientCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "client", Short: "Server client operations", Aliases: []string{"c"}}
	cmd.AddCommand(&cobra.Command{
		Use:   "list INSTANCE",
		Short: "List clients on a server instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			list, err := c.ListClients(context.Background(), args[0])
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "CN\tNAME\tSTATIC_IP\tSUSPENDED\tCONNECTED\tRX\tTX")
			for _, cl := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%v\t%d\t%d\n",
					cl.CommonName, cl.Name, cl.StaticIP, cl.Suspended, cl.Connected, cl.RxBytes, cl.TxBytes)
			}
			return w.Flush()
		},
	})
	var displayName, staticIP, caName, linkTTL string
	var issueCert, mintLink bool
	var linkUses int
	create := &cobra.Command{
		Use:   "create INSTANCE CN",
		Short: "Create VPN user (auto IP + cert; optional install link)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			req := pkgapi.ClientCreateRequest{
				CommonName: args[1], Name: displayName, StaticIP: staticIP,
				CAName: caName, MintProfileLink: mintLink, ProfileLinkTTL: linkTTL,
			}
			if cmd.Flags().Changed("issue-cert") {
				req.IssueCert = &issueCert
			}
			if cmd.Flags().Changed("link-uses") {
				req.ProfileLinkMaxUses = &linkUses
			}
			out, err := c.CreateClient(context.Background(), args[0], req)
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	create.Flags().StringVar(&displayName, "name", "", "display name (default: CN)")
	create.Flags().StringVar(&staticIP, "ip", "", "static IP (empty=auto from pool)")
	create.Flags().StringVar(&caName, "ca", "", "CA name for issue-cert (default: instance CA)")
	create.Flags().BoolVar(&issueCert, "issue-cert", true, "mint client cert (default on when CA exists)")
	create.Flags().BoolVar(&mintLink, "link", false, "mint one-click profile download / import URL")
	create.Flags().StringVar(&linkTTL, "link-ttl", "24h", "profile link TTL when --link")
	create.Flags().IntVar(&linkUses, "link-uses", 1, "profile link max downloads when --link")
	cmd.AddCommand(create)
	cmd.AddCommand(&cobra.Command{
		Use:   "delete INSTANCE CN",
		Short: "Delete client",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.DeleteClient(context.Background(), args[0], args[1])
		},
	})
	var ttl string
	var maxUses int
	link := &cobra.Command{
		Use:   "link INSTANCE CN",
		Short: "Mint a one-click profile URL (download + openvpn://import-profile/)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			req := pkgapi.ProfileLinkRequest{TTL: ttl}
			if cmd.Flags().Changed("max-uses") {
				req.MaxUses = &maxUses
			}
			out, err := c.CreateProfileLink(context.Background(), args[0], args[1], req)
			if err != nil {
				return err
			}
			fmt.Println("download:", out.DownloadURL)
			fmt.Println("import:  ", out.ImportURL)
			fmt.Println("expires: ", out.ExpiresAt.Format(time.RFC3339))
			fmt.Println("max_uses:", out.MaxUses)
			return nil
		},
	}
	link.Flags().StringVar(&ttl, "ttl", "24h", "link lifetime")
	link.Flags().IntVar(&maxUses, "max-uses", 1, "max downloads (0=unlimited until expiry)")
	cmd.AddCommand(link)
	cmd.AddCommand(&cobra.Command{
		Use:   "config INSTANCE CN",
		Short: "Print client .ovpn (authenticated)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			body, err := c.ClientConfig(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Print(body)
			return nil
		},
	})
	var issueCA string
	issueCli := &cobra.Command{
		Use:   "issue-cert INSTANCE CN",
		Short: "Issue client cert from CA and wire paths",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.IssueClientCert(context.Background(), args[0], args[1], pkgapi.IssueClientCertRequest{CAName: issueCA})
		},
	}
	issueCli.Flags().StringVar(&issueCA, "ca", "", "CA name (default first / instance CA)")
	cmd.AddCommand(issueCli)
	return cmd
}

func pkiCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "pki", Short: "Certificate authority and mTLS material"}
	cmd.AddCommand(&cobra.Command{
		Use:   "ca-list",
		Short: "List CAs",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			list, err := c.ListCAs(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tCN\tNOT_AFTER\tCERT")
			for _, ca := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ca.Name, ca.CommonName, ca.NotAfter, ca.CertPath)
			}
			return w.Flush()
		},
	})
	var org string
	var days int
	caCreate := &cobra.Command{
		Use:   "ca-create [NAME]",
		Short: "Create a new CA (mTLS root)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			name := "default"
			if len(args) == 1 {
				name = args[0]
			}
			cn := "OpenVPNd CA " + name
			if flagCN, _ := cmd.Flags().GetString("cn"); flagCN != "" {
				cn = flagCN
			}
			out, err := c.CreateCA(context.Background(), pkgapi.CreateCARequest{
				Name: name, CommonName: cn, Org: org, ValidDays: days,
			})
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	caCreate.Flags().String("cn", "", "CA common name")
	caCreate.Flags().StringVar(&org, "org", "", "organization")
	caCreate.Flags().IntVar(&days, "days", 3650, "validity days")
	cmd.AddCommand(caCreate)

	var certCA string
	certList := &cobra.Command{
		Use:   "cert-list",
		Short: "List issued certificates",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			list, err := c.ListCertificates(context.Background(), certCA)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tCA\tKIND\tCN\tNOT_AFTER\tFINGERPRINT")
			for _, cert := range list {
				fp := cert.Fingerprint
				if len(fp) > 16 {
					fp = fp[:16] + "…"
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", cert.ID, cert.CAName, cert.Kind, cert.CommonName, cert.NotAfter, fp)
			}
			return w.Flush()
		},
	}
	certList.Flags().StringVar(&certCA, "ca", "", "filter by CA")
	cmd.AddCommand(certList)

	cmd.AddCommand(&cobra.Command{
		Use:   "tls-crypt [NAME]",
		Short: "Generate OpenVPN tls-crypt static key",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			name := "default"
			if len(args) == 1 {
				name = args[0]
			}
			out, err := c.GenerateTLSCrypt(context.Background(), name)
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	})
	return cmd
}

func binaryCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "binary", Short: "OpenVPN binary registry", Aliases: []string{"bin"}}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List binaries",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			list, err := c.ListBinaries(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPATH\tVERSION")
			for _, b := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\n", b.Name, b.Path, b.Version)
			}
			return w.Flush()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "add NAME PATH",
		Short: "Register a binary",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			out, err := c.CreateBinary(context.Background(), pkgapi.BinaryCreateRequest{Name: args[0], Path: args[1]})
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "delete NAME",
		Short: "Remove a binary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.DeleteBinary(context.Background(), args[0])
		},
	})
	return cmd
}

func statsCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show global stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			out, err := c.Stats(context.Background())
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
}

func reconcileCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Force reconcile",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			return c.Reconcile(context.Background())
		},
	}
}

func eventsCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "List recent events",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			list, err := c.ListEvents(context.Background())
			if err != nil {
				return err
			}
			return printJSON(list)
		},
	}
}

func systemCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Daemon system info and backup",
		Aliases: []string{"sys"},
	}
	cmd.AddCommand(systemInfoCmd(configPath), systemBackupCmd(configPath))
	return cmd
}

func systemInfoCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show daemon system info (or version+stats fallback)",
		Long: `Prefer GET /v1/system/info when the daemon exposes it.
If that route is missing, fall back to /v1/version + /v1/stats.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			ctx := context.Background()
			info, err := c.SystemInfo(ctx)
			if err == nil {
				return printJSON(info)
			}
			// Hard-fail only on explicit client errors other than missing/unimplemented route.
			// Transport failures and 404/501 fall through to version+stats.
			var ae *pkgapi.APIError
			if errors.As(err, &ae) && ae.Status != http.StatusNotFound && ae.Status != http.StatusNotImplemented && ae.Status < 500 {
				return err
			}
			ver, vErr := c.Version(ctx)
			st, sErr := c.Stats(ctx)
			if vErr != nil && sErr != nil {
				return fmt.Errorf("system info unavailable: %v (version: %v; stats: %v)", err, vErr, sErr)
			}
			out := map[string]any{
				"source": "fallback:version+stats",
			}
			if vErr == nil {
				out["version"] = ver
			}
			if sErr == nil {
				out["stats"] = st
			}
			if ready, rErr := c.Readyz(ctx); rErr == nil {
				out["readyz"] = ready
			}
			if err != nil {
				out["system_info_error"] = err.Error()
			}
			return printJSON(out)
		},
	}
}

func systemBackupCmd(configPath *string) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup daemon state via API (or print openvpnd backup instructions)",
		Long: `Calls POST /v1/system/backup with {"path": OUT} when the daemon supports it.

If the API route is not implemented, prints the host-side backup command:

  openvpnd backup --out FILE

See docs/PRODUCTION.md for restore steps.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(outPath) == "" {
				return fmt.Errorf("--out is required")
			}
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			resp, err := c.SystemBackup(context.Background(), outPath)
			if err == nil {
				return printJSON(resp)
			}
			if pkgapi.IsNotImplemented(err) {
				fmt.Fprintf(os.Stderr, "API backup not available on this daemon (%v).\n", err)
				fmt.Fprintf(os.Stderr, "Run on the openvpnd host instead:\n\n")
				fmt.Fprintf(os.Stderr, "  openvpnd backup --out %s\n\n", outPath)
				fmt.Fprintf(os.Stderr, "Restore (service stopped):\n")
				fmt.Fprintf(os.Stderr, "  systemctl stop openvpnd\n")
				fmt.Fprintf(os.Stderr, "  openvpnd restore --in %s\n", outPath)
				fmt.Fprintf(os.Stderr, "  systemctl start openvpnd\n\n")
				fmt.Fprintf(os.Stderr, "See docs/PRODUCTION.md for the full production checklist.\n")
				return fmt.Errorf("backup API unimplemented; use openvpnd backup --out %s", outPath)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "", "backup archive path on the daemon host")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func splitCSVFlag(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
