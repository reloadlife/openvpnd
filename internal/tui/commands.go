package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

type tickMsg time.Time

type flashClearMsg struct{ id int }

type dataMsg struct {
	gen       uint64
	instances []pkgapi.Instance
	clients   []pkgapi.ServerClient
	binaries  []pkgapi.Binary
	stats     pkgapi.Stats
	events    []pkgapi.Event
	cas       []pkgapi.CA
	certs     []pkgapi.Certificate
	tlsCrypts []pkgapi.TLSCryptKey
	err       error
}

type actionDoneMsg struct {
	err     error
	flash   string
	refresh bool
}

type profileLinkMsg struct {
	link pkgapi.ProfileLink
	qr   string
	err  error
}

type clientCreatedMsg struct {
	client   pkgapi.ServerClient
	link     *pkgapi.ProfileLink
	qr       string
	flash    string
	warnings []string
	err      error
}

type confViewMsg struct {
	title string
	body  string
	qr    string
	err   error
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func flashClearCmd(id int) tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return flashClearMsg{id: id} })
}

func fetchData(c *pkgapi.Client, gen uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		msg := dataMsg{gen: gen}
		insts, err := c.ListInstances(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.instances = insts
		clients, err := c.ListAllClients(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.clients = clients
		bins, err := c.ListBinaries(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.binaries = bins
		stats, err := c.Stats(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.stats = stats
		events, err := c.ListEvents(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.events = events
		// PKI is optional for older daemons — soft-fail empty lists
		if cas, err := c.ListCAs(ctx); err == nil {
			msg.cas = cas
		}
		if certs, err := c.ListCertificates(ctx, ""); err == nil {
			msg.certs = certs
		}
		if keys, err := c.ListTLSCrypt(ctx); err == nil {
			msg.tlsCrypts = keys
		}
		return msg
	}
}

func doAction(fn func(ctx context.Context) error, flash string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := fn(ctx)
		return actionDoneMsg{err: err, flash: flash, refresh: err == nil}
	}
}

func doCreateInstance(c *pkgapi.Client, req pkgapi.InstanceCreateRequest) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		_, err := c.CreateInstance(ctx, req)
		return err
	}, "instance "+req.Name+" created")
}

func doDeleteInstance(c *pkgapi.Client, name string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.DeleteInstance(ctx, name)
	}, "deleted "+name)
}

func doInstanceUpDown(c *pkgapi.Client, name string, up bool) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		if up {
			return c.InstanceUp(ctx, name)
		}
		return c.InstanceDown(ctx, name)
	}, map[bool]string{true: name + " up", false: name + " down"}[up])
}

func doInstanceRestart(c *pkgapi.Client, name string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.InstanceRestart(ctx, name)
	}, name+" restarted")
}

func doCreateClient(c *pkgapi.Client, inst string, req pkgapi.ClientCreateRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := c.CreateClient(ctx, inst, req)
		if err != nil {
			return clientCreatedMsg{err: err}
		}
		flash := "client " + out.CommonName + " created"
		if out.StaticIP != "" {
			flash += " · " + out.StaticIP
		}
		if len(out.AutoFilled) > 0 {
			flash += " · auto: " + strings.Join(out.AutoFilled, ", ")
		}
		msg := clientCreatedMsg{
			client:   out.ServerClient,
			flash:    flash,
			warnings: out.Warnings,
		}
		if out.ProfileLink != nil {
			msg.link = out.ProfileLink
			msg.qr, _ = RenderQR(out.ProfileLink.ImportURL)
		}
		return msg
	}
}

func doDeleteClient(c *pkgapi.Client, inst, cn string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.DeleteClient(ctx, inst, cn)
	}, "deleted client "+cn)
}

func doSuspendClient(c *pkgapi.Client, inst, cn string, suspend bool) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		if suspend {
			return c.SuspendClient(ctx, inst, cn)
		}
		return c.ResumeClient(ctx, inst, cn)
	}, map[bool]string{true: cn + " suspended", false: cn + " resumed"}[suspend])
}

func doResetClientTraffic(c *pkgapi.Client, inst, cn string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.ResetClientTraffic(ctx, inst, cn)
	}, "traffic reset")
}

func doCreateBinary(c *pkgapi.Client, req pkgapi.BinaryCreateRequest) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		_, err := c.CreateBinary(ctx, req)
		return err
	}, "binary "+req.Name+" registered")
}

func doDeleteBinary(c *pkgapi.Client, name string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.DeleteBinary(ctx, name)
	}, "deleted binary "+name)
}

func doReconcile(c *pkgapi.Client) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.Reconcile(ctx)
	}, "reconcile complete")
}

func doProfileLink(c *pkgapi.Client, inst, cn string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		link, err := c.CreateProfileLink(ctx, inst, cn, pkgapi.ProfileLinkRequest{})
		if err != nil {
			return profileLinkMsg{err: err}
		}
		qr, _ := RenderQR(link.ImportURL)
		return profileLinkMsg{link: link, qr: qr}
	}
}

func doClientConfig(c *pkgapi.Client, inst, cn string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		body, err := c.ClientConfig(ctx, inst, cn)
		if err != nil {
			return confViewMsg{err: err}
		}
		return confViewMsg{title: inst + " / " + cn + " .ovpn", body: body}
	}
}

func doExportInstance(c *pkgapi.Client, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		body, err := c.ExportInstance(ctx, name)
		if err != nil {
			return confViewMsg{err: err}
		}
		return confViewMsg{title: name + " conf", body: body}
	}
}

func doCreateCA(c *pkgapi.Client, req pkgapi.CreateCARequest) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		_, err := c.CreateCA(ctx, req)
		return err
	}, "CA "+req.Name+" created")
}

func doIssueCert(c *pkgapi.Client, req pkgapi.IssueCertRequest) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		_, err := c.IssueCert(ctx, req)
		return err
	}, "cert "+req.CommonName+" issued")
}

func doRevokeCert(c *pkgapi.Client, id int64, reason string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.RevokeCert(ctx, id, reason)
	}, fmt.Sprintf("cert #%d revoked", id))
}

func doRebuildCRL(c *pkgapi.Client, caName string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		_, err := c.RebuildCRL(ctx, caName)
		return err
	}, "CRL rebuilt for "+caName)
}

func doIssueClientCert(c *pkgapi.Client, inst, cn string) tea.Cmd {
	return doAction(func(ctx context.Context) error {
		return c.IssueClientCert(ctx, inst, cn, pkgapi.IssueClientCertRequest{})
	}, "cert issued for "+cn)
}

func doImportInstance(c *pkgapi.Client, content, sourcePath string) tea.Cmd {
	create := true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := c.ImportInstance(ctx, pkgapi.ImportInstanceRequest{
			Content: content, SourcePath: sourcePath, Create: &create,
		})
		if err != nil {
			return actionDoneMsg{err: err}
		}
		flash := "imported"
		if out.Instance.Name != "" {
			flash += " " + out.Instance.Name
		} else if sourcePath != "" {
			flash += " " + sourcePath
		}
		return actionDoneMsg{err: nil, flash: flash, refresh: true}
	}
}

func parseIntField(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func splitCSV(s string) []string {
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

func formatBps(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1f GB/s", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1f MB/s", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1f KB/s", v/1e3)
	default:
		return fmt.Sprintf("%.0f B/s", v)
	}
}

func formatBytes(v int64) string {
	f := float64(v)
	switch {
	case f >= 1e12:
		return fmt.Sprintf("%.1f TB", f/1e12)
	case f >= 1e9:
		return fmt.Sprintf("%.1f GB", f/1e9)
	case f >= 1e6:
		return fmt.Sprintf("%.1f MB", f/1e6)
	case f >= 1e3:
		return fmt.Sprintf("%.1f KB", f/1e3)
	default:
		return fmt.Sprintf("%d B", v)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
