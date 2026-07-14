package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/reloadlife/openvpnd/internal/config"
	"github.com/reloadlife/openvpnd/internal/tui"
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
		tuiCmd(&configPath),
		instanceCmd(&configPath),
		clientCmd(&configPath),
		binaryCmd(&configPath),
		pkiCmd(&configPath),
		statsCmd(&configPath),
		reconcileCmd(&configPath),
		eventsCmd(&configPath),
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
	var role, binary, network, remote string
	var port int
	create := &cobra.Command{
		Use:   "create NAME",
		Short: "Create an instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			req := pkgapi.InstanceCreateRequest{
				Name: args[0], Role: role, BinaryName: binary, Port: port, ServerNetwork: network,
			}
			if remote != "" && role == "client" {
				req.Remotes = []pkgapi.Remote{{Host: remote, Port: port}}
				if port == 0 {
					req.Remotes[0].Port = 1194
				}
			}
			out, err := c.CreateInstance(context.Background(), req)
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	create.Flags().StringVar(&role, "role", "server", "server|client")
	create.Flags().StringVar(&binary, "binary", "", "binary registry name")
	create.Flags().IntVar(&port, "port", 1194, "listen/remote port")
	create.Flags().StringVar(&network, "network", "", "server network CIDR (e.g. 10.8.0.0/24)")
	create.Flags().StringVar(&remote, "remote", "", "client remote host")
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
	var caName, serverCN string
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
				CAName: caName, CommonName: serverCN, GenerateTLSCrypt: genTC,
			})
		},
	}
	issueSrv.Flags().StringVar(&caName, "ca", "", "CA name (default first)")
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
	var displayName, staticIP string
	create := &cobra.Command{
		Use:   "create INSTANCE CN",
		Short: "Create a server client (auto IP if --ip empty)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, c, err := loadClient(*configPath)
			if err != nil {
				return err
			}
			out, err := c.CreateClient(context.Background(), args[0], pkgapi.ClientCreateRequest{
				CommonName: args[1], Name: displayName, StaticIP: staticIP,
			})
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	create.Flags().StringVar(&displayName, "name", "", "display name")
	create.Flags().StringVar(&staticIP, "ip", "", "static IP (empty=auto)")
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

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
