package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

const (
	tabInstances = 0
	tabClients   = 1
	tabBinaries  = 2
	tabStats     = 3
	tabEvents    = 4

	modeList         = 0
	modeInstForm     = 1
	modeClientForm   = 2
	modeBinaryForm   = 3
	modeInstDetail   = 4
	modeClientDetail = 5
	modeConfView     = 6
	modeProfileLink  = 7
	modeConfirm      = 8
)

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmDelInst
	confirmDelClient
	confirmDelBinary
)

type rootModel struct {
	cfg    Config
	tab    int
	mode   int
	width  int
	height int

	instances []pkgapi.Instance
	clients   []pkgapi.ServerClient
	binaries  []pkgapi.Binary
	stats     pkgapi.Stats
	events    []pkgapi.Event
	cursor    int

	err    string
	status string
	flash  string

	form formModel

	confirm     confirmKind
	confirmText string
	confirmArg  string
	confirmArg2 string

	detailInst   *pkgapi.Instance
	detailClient *pkgapi.ServerClient
	confTitle    string
	confBody     string
	confQR       string
	profileLink  *pkgapi.ProfileLink
	scroll       int

	fetchGen uint64
	busy     bool
	flashID  int
}

func newRootModel(cfg Config) rootModel {
	return rootModel{cfg: cfg, status: "connecting…", mode: modeList}
}

func (m rootModel) beginFetch() (rootModel, tea.Cmd) {
	m.fetchGen++
	return m, fetchData(m.cfg.Client, m.fetchGen)
}

func (m rootModel) startMutate(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	m.busy = true
	return m, cmd
}

func (m rootModel) setFlash(s string) (rootModel, tea.Cmd) {
	m.flashID++
	m.flash = s
	return m, flashClearCmd(m.flashID)
}

func (m rootModel) Init() tea.Cmd {
	return tea.Batch(fetchData(m.cfg.Client, m.fetchGen), tickCmd(m.cfg.RefreshInterval))
}

func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.mode == modeInstForm || m.mode == modeClientForm || m.mode == modeBinaryForm {
			m.form.SetSize(msg.Width, m.formAreaHeight())
		}
		return m, nil

	case tickMsg:
		if m.mode == modeList || m.mode == modeInstDetail || m.mode == modeClientDetail {
			m, fetch := m.beginFetch()
			return m, tea.Batch(fetch, tickCmd(m.cfg.RefreshInterval))
		}
		return m, tickCmd(m.cfg.RefreshInterval)

	case flashClearMsg:
		if msg.id == m.flashID {
			m.flash = ""
		}
		return m, nil

	case dataMsg:
		if msg.gen != m.fetchGen {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err.Error()
			m.status = "error"
		} else {
			m.err = ""
			m.instances = msg.instances
			m.clients = msg.clients
			m.binaries = msg.binaries
			m.stats = msg.stats
			m.events = msg.events
			m.status = "ok"
			if m.cursor >= m.rowCount() {
				m.cursor = max(0, m.rowCount()-1)
			}
			m.refreshDetailPtrs()
		}
		return m, nil

	case actionDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.status = "error"
			return m, nil
		}
		m.err = ""
		m.mode = modeList
		m.confirm = confirmNone
		m, flashCmd := m.setFlash(msg.flash)
		cmds := []tea.Cmd{flashCmd}
		if msg.refresh {
			var fetch tea.Cmd
			m, fetch = m.beginFetch()
			cmds = append(cmds, fetch)
		}
		return m, tea.Batch(cmds...)

	case profileLinkMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		link := msg.link
		m.profileLink = &link
		m.confQR = msg.qr
		m.mode = modeProfileLink
		return m, nil

	case confViewMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.confTitle = msg.title
		m.confBody = msg.body
		m.confQR = msg.qr
		m.scroll = 0
		m.mode = modeConfView
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeInstForm || m.mode == modeClientForm || m.mode == modeBinaryForm {
			return m.handleFormKeyAll(msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *rootModel) refreshDetailPtrs() {
	if m.detailInst != nil {
		for i := range m.instances {
			if m.instances[i].Name == m.detailInst.Name {
				inst := m.instances[i]
				m.detailInst = &inst
				break
			}
		}
	}
	if m.detailClient != nil {
		for i := range m.clients {
			if m.clients[i].CommonName == m.detailClient.CommonName &&
				m.clients[i].InstanceName == m.detailClient.InstanceName {
				cl := m.clients[i]
				m.detailClient = &cl
				break
			}
		}
	}
}

func (m rootModel) rowCount() int {
	switch m.tab {
	case tabInstances:
		return len(m.instances)
	case tabClients:
		return len(m.clients)
	case tabBinaries:
		return len(m.binaries)
	case tabEvents:
		return len(m.events)
	default:
		return 0
	}
}

func (m rootModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeConfirm:
		return m.handleConfirm(key)
	case modeInstDetail:
		return m.handleInstDetailKey(key)
	case modeClientDetail:
		return m.handleClientDetailKey(key)
	case modeConfView, modeProfileLink:
		if key == "esc" || key == "q" || key == "enter" {
			if m.detailClient != nil {
				m.mode = modeClientDetail
			} else if m.detailInst != nil {
				m.mode = modeInstDetail
			} else {
				m.mode = modeList
			}
			m.confBody = ""
			m.confQR = ""
			m.profileLink = nil
			m.scroll = 0
		} else if key == "down" || key == "j" {
			m.scroll++
		} else if key == "up" || key == "k" {
			if m.scroll > 0 {
				m.scroll--
			}
		}
		return m, nil
	default:
		return m.handleListKey(key)
	}
}

func (m rootModel) handleConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		if m.busy {
			return m, nil
		}
		switch m.confirm {
		case confirmDelInst:
			name := m.confirmArg
			m.confirm = confirmNone
			m.mode = modeList
			return m.startMutate(doDeleteInstance(m.cfg.Client, name))
		case confirmDelClient:
			inst, cn := m.confirmArg, m.confirmArg2
			m.confirm = confirmNone
			m.mode = modeList
			return m.startMutate(doDeleteClient(m.cfg.Client, inst, cn))
		case confirmDelBinary:
			name := m.confirmArg
			m.confirm = confirmNone
			m.mode = modeList
			return m.startMutate(doDeleteBinary(m.cfg.Client, name))
		}
	case "n", "N", "esc":
		m.confirm = confirmNone
		m.mode = modeList
	}
	return m, nil
}

func (m rootModel) formAreaHeight() int {
	h := m.height - 1
	if h < 10 {
		h = 10
	}
	return h
}

func (m rootModel) handleFormKeyAll(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.mode = modeList
		return m, nil
	case "enter":
		switch m.mode {
		case modeInstForm:
			return m.submitInstForm()
		case modeClientForm:
			return m.submitClientForm()
		case modeBinaryForm:
			return m.submitBinaryForm()
		}
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	return m, cmd
}

func (m rootModel) handleListKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "1":
		m.tab = tabInstances
		m.cursor = 0
	case "2":
		m.tab = tabClients
		m.cursor = 0
	case "3":
		m.tab = tabBinaries
		m.cursor = 0
	case "4":
		m.tab = tabStats
		m.cursor = 0
	case "5":
		m.tab = tabEvents
		m.cursor = 0
	case "tab":
		m.tab = (m.tab + 1) % 5
		m.cursor = 0
	case "shift+tab":
		m.tab = (m.tab + 4) % 5
		m.cursor = 0
	case "j", "down":
		if m.cursor < m.rowCount()-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(0, m.rowCount()-1)
	case "r":
		m, fetch := m.beginFetch()
		return m, fetch
	case "R":
		return m.startMutate(doReconcile(m.cfg.Client))
	case "n":
		return m.openCreateForm()
	case "enter", "l":
		return m.openDetail()
	case "u":
		return m.listUpDown(true)
	case "d":
		return m.listUpDown(false)
	case "D", "x":
		return m.listDeleteConfirm()
	case "s":
		return m.listSuspend(true)
	case "S":
		return m.listSuspend(false)
	}
	return m, nil
}

func (m rootModel) openCreateForm() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabInstances:
		bins := make([]string, 0, len(m.binaries))
		for _, b := range m.binaries {
			bins = append(bins, b.Name)
		}
		m.form = newForm("Create instance", instanceCreateFields(bins), map[string]string{
			"port": "1194", "proto": "udp", "topology": "subnet", "network": "10.8.0.0/24", "role": "server",
		})
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeInstForm
	case tabClients:
		servers := m.serverNames()
		m.form = newForm("Create client", clientCreateFields(servers), nil)
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeClientForm
	case tabBinaries:
		m.form = newForm("Register binary", binaryCreateFields(), nil)
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeBinaryForm
	}
	return m, nil
}

func (m rootModel) serverNames() []string {
	var out []string
	for _, i := range m.instances {
		if i.Role == "server" {
			out = append(out, i.Name)
		}
	}
	return out
}

func (m rootModel) openDetail() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabInstances:
		if m.cursor < 0 || m.cursor >= len(m.instances) {
			return m, nil
		}
		inst := m.instances[m.cursor]
		m.detailInst = &inst
		m.detailClient = nil
		m.mode = modeInstDetail
	case tabClients:
		if m.cursor < 0 || m.cursor >= len(m.clients) {
			return m, nil
		}
		cl := m.clients[m.cursor]
		m.detailClient = &cl
		m.mode = modeClientDetail
	case tabBinaries:
		// no detail — show in list
	}
	return m, nil
}

func (m rootModel) listUpDown(up bool) (tea.Model, tea.Cmd) {
	if m.tab != tabInstances || m.cursor < 0 || m.cursor >= len(m.instances) {
		return m, nil
	}
	name := m.instances[m.cursor].Name
	return m.startMutate(doInstanceUpDown(m.cfg.Client, name, up))
}

func (m rootModel) listSuspend(suspend bool) (tea.Model, tea.Cmd) {
	if m.tab != tabClients || m.cursor < 0 || m.cursor >= len(m.clients) {
		return m, nil
	}
	cl := m.clients[m.cursor]
	return m.startMutate(doSuspendClient(m.cfg.Client, cl.InstanceName, cl.CommonName, suspend))
}

func (m rootModel) listDeleteConfirm() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabInstances:
		if m.cursor < 0 || m.cursor >= len(m.instances) {
			return m, nil
		}
		name := m.instances[m.cursor].Name
		m.confirm = confirmDelInst
		m.confirmArg = name
		m.confirmText = "Delete instance " + name + "? [y/n]"
		m.mode = modeConfirm
	case tabClients:
		if m.cursor < 0 || m.cursor >= len(m.clients) {
			return m, nil
		}
		cl := m.clients[m.cursor]
		m.confirm = confirmDelClient
		m.confirmArg = cl.InstanceName
		m.confirmArg2 = cl.CommonName
		m.confirmText = "Delete client " + cl.CommonName + " on " + cl.InstanceName + "? [y/n]"
		m.mode = modeConfirm
	case tabBinaries:
		if m.cursor < 0 || m.cursor >= len(m.binaries) {
			return m, nil
		}
		name := m.binaries[m.cursor].Name
		m.confirm = confirmDelBinary
		m.confirmArg = name
		m.confirmText = "Delete binary " + name + "? [y/n]"
		m.mode = modeConfirm
	}
	return m, nil
}

func (m rootModel) handleInstDetailKey(key string) (tea.Model, tea.Cmd) {
	if m.detailInst == nil {
		m.mode = modeList
		return m, nil
	}
	name := m.detailInst.Name
	switch key {
	case "esc", "q", "h":
		m.mode = modeList
		m.detailInst = nil
	case "u":
		return m.startMutate(doInstanceUpDown(m.cfg.Client, name, true))
	case "d":
		return m.startMutate(doInstanceUpDown(m.cfg.Client, name, false))
	case "r":
		return m.startMutate(doInstanceRestart(m.cfg.Client, name))
	case "e", "E":
		return m.startMutate(doExportInstance(m.cfg.Client, name))
	case "D":
		m.confirm = confirmDelInst
		m.confirmArg = name
		m.confirmText = "Delete instance " + name + "? [y/n]"
		m.mode = modeConfirm
	case "n":
		// create client on this server
		if m.detailInst.Role == "server" {
			m.form = newForm("Create client", clientCreateFields([]string{name}), map[string]string{"instance": name})
			m.form.SetSize(m.width, m.formAreaHeight())
			m.mode = modeClientForm
			return m, nil
		}
	case "2":
		m.tab = tabClients
		m.mode = modeList
		m.detailInst = nil
	}
	return m, nil
}

func (m rootModel) handleClientDetailKey(key string) (tea.Model, tea.Cmd) {
	if m.detailClient == nil {
		m.mode = modeList
		return m, nil
	}
	cl := m.detailClient
	switch key {
	case "esc", "q", "h":
		m.mode = modeList
		m.detailClient = nil
	case "s":
		return m.startMutate(doSuspendClient(m.cfg.Client, cl.InstanceName, cl.CommonName, true))
	case "S":
		return m.startMutate(doSuspendClient(m.cfg.Client, cl.InstanceName, cl.CommonName, false))
	case "t":
		return m.startMutate(doResetClientTraffic(m.cfg.Client, cl.InstanceName, cl.CommonName))
	case "c":
		m.busy = true
		return m, doClientConfig(m.cfg.Client, cl.InstanceName, cl.CommonName)
	case "L", "p":
		m.busy = true
		return m, doProfileLink(m.cfg.Client, cl.InstanceName, cl.CommonName)
	case "D":
		m.confirm = confirmDelClient
		m.confirmArg = cl.InstanceName
		m.confirmArg2 = cl.CommonName
		m.confirmText = "Delete client " + cl.CommonName + "? [y/n]"
		m.mode = modeConfirm
	}
	return m, nil
}

func (m rootModel) submitInstForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	name := strings.TrimSpace(v["name"])
	if name == "" {
		m.form.err = "name required"
		return m, nil
	}
	port, _ := parseIntField(v["port"])
	req := pkgapi.InstanceCreateRequest{
		Name: name, Role: v["role"], BinaryName: v["binary"], Port: port, Proto: v["proto"],
		ServerNetwork: v["network"], Topology: v["topology"], PublicEndpoint: v["public_endpoint"],
		PushDNS: splitCSV(v["push_dns"]), RedirectGateway: truthy(v["redirect_gw"]),
		PKICaPath: v["pki_ca"], PKICertPath: v["pki_cert"], PKIKeyPath: v["pki_key"],
		PKIDHPath: v["pki_dh"], PKITLSCryptPath: v["pki_tls"],
	}
	if v["role"] == "client" && v["remote"] != "" {
		req.Remotes = []pkgapi.Remote{{Host: v["remote"], Port: port}}
		if port == 0 {
			req.Remotes[0].Port = 1194
		}
	}
	return m.startMutate(doCreateInstance(m.cfg.Client, req))
}

func (m rootModel) submitClientForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	inst := v["instance"]
	cn := strings.TrimSpace(v["cn"])
	if inst == "" || strings.HasPrefix(inst, "(") || cn == "" {
		m.form.err = "instance and common name required"
		return m, nil
	}
	req := pkgapi.ClientCreateRequest{
		CommonName: cn, Name: v["name"], StaticIP: v["static_ip"],
		ClientCertPath: v["cert_path"], ClientKeyPath: v["key_path"],
	}
	return m.startMutate(doCreateClient(m.cfg.Client, inst, req))
}

func (m rootModel) submitBinaryForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	if v["name"] == "" || v["path"] == "" {
		m.form.err = "name and path required"
		return m, nil
	}
	return m.startMutate(doCreateBinary(m.cfg.Client, pkgapi.BinaryCreateRequest{
		Name: v["name"], Path: v["path"], Notes: v["notes"],
	}))
}

// ─── View ───────────────────────────────────────────────────────────

func (m rootModel) View() string {
	if m.width == 0 {
		m.width = 100
		m.height = 30
	}
	var body string
	switch m.mode {
	case modeInstForm, modeClientForm, modeBinaryForm:
		body = m.form.View()
	case modeConfirm:
		body = panelStyle.Width(m.width-2).Render(
			titleStyle.Render("Confirm") + "\n\n" + warnStyle.Render(m.confirmText) + "\n\n" +
				helpStyle.Render("y confirm · n/esc cancel"),
		)
	case modeInstDetail:
		body = m.viewInstDetail()
	case modeClientDetail:
		body = m.viewClientDetail()
	case modeConfView:
		body = m.viewConf()
	case modeProfileLink:
		body = m.viewProfileLink()
	default:
		body = m.viewList()
	}
	header := m.viewHeader()
	status := m.viewStatus()
	// full layout
	innerH := m.height - 2
	if innerH < 5 {
		innerH = 5
	}
	main := fillHeight(body, m.width, innerH)
	return lipgloss.JoinVertical(lipgloss.Left, header, main, status)
}

func (m rootModel) viewHeader() string {
	tabs := []string{"1 Instances", "2 Clients", "3 Binaries", "4 Stats", "5 Events"}
	var parts []string
	for i, t := range tabs {
		if i == m.tab && m.mode == modeList {
			parts = append(parts, tabActive.Render(t))
		} else {
			parts = append(parts, tabInactive.Render(t))
		}
	}
	left := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	right := dimStyle.Render("openvpnd  " + m.cfg.Endpoint)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m rootModel) viewStatus() string {
	var bits []string
	bits = append(bits, " "+m.status+" ")
	if m.busy {
		bits = append(bits, " busy ")
	}
	if m.flash != "" {
		bits = append(bits, " "+m.flash+" ")
	}
	if m.err != "" {
		return errStyle.Render(" "+m.err+" ") + statusStyle.Width(m.width).Render(strings.Join(bits, "·"))
	}
	line := statusStyle.Width(m.width).Render(strings.Join(bits, " · "))
	return line
}

func (m rootModel) viewList() string {
	switch m.tab {
	case tabInstances:
		return m.viewInstanceList()
	case tabClients:
		return m.viewClientList()
	case tabBinaries:
		return m.viewBinaryList()
	case tabStats:
		return m.viewStats()
	case tabEvents:
		return m.viewEvents()
	}
	return ""
}

func (m rootModel) viewInstanceList() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("NAME            ROLE     STATE   BINARY     PORT   CLIENTS   RX         TX"))
	b.WriteString("\n")
	if len(m.instances) == 0 {
		b.WriteString(dimStyle.Render("\n  No instances. Press n to create.\n"))
	}
	for i, inst := range m.instances {
		state := badgeDown.Render("DOWN")
		if inst.Up {
			state = badgeUp.Render(" UP ")
		} else if inst.Enabled {
			state = warnStyle.Render("WAIT")
		}
		role := badgeSrv.Render("srv")
		if inst.Role == "client" {
			role = badgeCli.Render("cli")
		}
		line := fmt.Sprintf("%-15s %-8s %s  %-10s %-6d %-8d %-10s %-10s",
			trunc(inst.Name, 15), role, state, trunc(inst.BinaryName, 10), inst.Port,
			inst.ConnectedClients, formatBytes(inst.RxBytes), formatBytes(inst.TxBytes))
		if i == m.cursor {
			b.WriteString(selStyle.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("n new · enter detail · u/d up/down · D delete · r refresh · R reconcile · 1-5 tabs · q quit"))
	return b.String()
}

func (m rootModel) viewClientList() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("INSTANCE        CN              NAME         IP            STATE      RX         TX"))
	b.WriteString("\n")
	if len(m.clients) == 0 {
		b.WriteString(dimStyle.Render("\n  No clients. Open a server and press n, or tab Clients + n.\n"))
	}
	for i, cl := range m.clients {
		state := dimStyle.Render("idle")
		if cl.Suspended {
			state = badgeSusp.Render("SUSP")
		} else if cl.Connected {
			state = badgeConn.Render("CONN")
		}
		line := fmt.Sprintf("%-15s %-15s %-12s %-13s %s  %-10s %-10s",
			trunc(cl.InstanceName, 15), trunc(cl.CommonName, 15), trunc(cl.Name, 12),
			trunc(cl.StaticIP, 13), state, formatBytes(cl.RxBytes), formatBytes(cl.TxBytes))
		if i == m.cursor {
			b.WriteString(selStyle.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("n new · enter detail · s/S suspend/resume · D delete · r refresh · q quit"))
	return b.String()
}

func (m rootModel) viewBinaryList() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("NAME            PATH                                      VERSION"))
	b.WriteString("\n")
	if len(m.binaries) == 0 {
		b.WriteString(dimStyle.Render("\n  No binaries registered.\n"))
	}
	for i, bin := range m.binaries {
		line := fmt.Sprintf("%-15s %-41s %s", trunc(bin.Name, 15), trunc(bin.Path, 41), trunc(bin.Version, 40))
		if i == m.cursor {
			b.WriteString(selStyle.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("n register · D delete · r refresh · q quit"))
	return b.String()
}

func (m rootModel) viewStats() string {
	s := m.stats
	var b strings.Builder
	b.WriteString(titleStyle.Render("Global stats"))
	b.WriteString("\n\n")
	kv := func(k, v string) {
		b.WriteString(labelStyle.Render(k))
		b.WriteString(valueStyle.Render(v))
		b.WriteString("\n")
	}
	kv("Instances", fmt.Sprintf("%d total · %d up", s.InstancesTotal, s.InstancesUp))
	kv("Clients", fmt.Sprintf("%d", len(m.clients)))
	kv("Binaries", fmt.Sprintf("%d", len(m.binaries)))
	kv("RX total", formatBytes(s.RxBytes)+"  ("+formatBps(s.RxBps)+")")
	kv("TX total", formatBytes(s.TxBytes)+"  ("+formatBps(s.TxBps)+")")
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("auto-refresh · r force · R reconcile · q quit"))
	return panelStyle.Width(m.width - 4).Render(b.String())
}

func (m rootModel) viewEvents() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("TIME                  LVL    KIND           INSTANCE        MESSAGE"))
	b.WriteString("\n")
	if len(m.events) == 0 {
		b.WriteString(dimStyle.Render("\n  No events yet.\n"))
	}
	for i, e := range m.events {
		line := fmt.Sprintf("%-21s %-6s %-14s %-15s %s",
			e.TS.Format("2006-01-02 15:04:05"), trunc(e.Level, 6), trunc(e.Kind, 14),
			trunc(e.Instance, 15), trunc(e.Message, 50))
		if i == m.cursor {
			b.WriteString(selStyle.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("r refresh · q quit"))
	return b.String()
}

func (m rootModel) viewInstDetail() string {
	inst := m.detailInst
	if inst == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Instance · " + inst.Name))
	b.WriteString("\n\n")
	kv := func(k, v string) {
		b.WriteString(labelStyle.Render(k))
		b.WriteString(valueStyle.Render(v))
		b.WriteString("\n")
	}
	up := "down"
	if inst.Up {
		up = "up"
	}
	kv("Role", inst.Role)
	kv("State", fmt.Sprintf("%s · enabled=%v · pid=%d", up, inst.Enabled, inst.PID))
	kv("Binary", inst.BinaryName+" "+inst.BinaryPath)
	kv("Listen", fmt.Sprintf("%s %s:%d", inst.Proto, orDash(inst.LocalBind), inst.Port))
	kv("Network", orDash(inst.ServerNetwork)+"  topology="+orDash(inst.Topology))
	kv("Public EP", orDash(inst.PublicEndpoint))
	kv("PKI CA", orDash(inst.PKICaPath))
	kv("Clients", fmt.Sprintf("%d connected (live)", inst.ConnectedClients))
	kv("Traffic", formatBytes(inst.RxBytes)+" / "+formatBytes(inst.TxBytes))
	if inst.LastError != "" {
		kv("Error", inst.LastError)
	}
	// related clients
	var related []pkgapi.ServerClient
	for _, cl := range m.clients {
		if cl.InstanceName == inst.Name {
			related = append(related, cl)
		}
	}
	if len(related) > 0 {
		b.WriteString("\n")
		b.WriteString(headerStyle.Render("Clients on this instance"))
		b.WriteString("\n")
		for _, cl := range related {
			b.WriteString(fmt.Sprintf("  · %-16s %-12s %s\n", cl.CommonName, cl.StaticIP, map[bool]string{true: "SUSP", false: ""}[cl.Suspended]))
		}
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("u/d up/down · r restart · e export conf · n new client · D delete · esc back"))
	return panelStyle.Width(m.width - 4).Render(b.String())
}

func (m rootModel) viewClientDetail() string {
	cl := m.detailClient
	if cl == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Client · " + cl.CommonName))
	b.WriteString("\n\n")
	kv := func(k, v string) {
		b.WriteString(labelStyle.Render(k))
		b.WriteString(valueStyle.Render(v))
		b.WriteString("\n")
	}
	kv("Instance", cl.InstanceName)
	kv("Name", orDash(cl.Name))
	kv("Static IP", orDash(cl.StaticIP))
	kv("Suspended", fmt.Sprintf("%v", cl.Suspended))
	kv("Connected", fmt.Sprintf("%v  %s", cl.Connected, orDash(cl.ConnectedSince)))
	kv("Real addr", orDash(cl.RealAddress))
	kv("Virt addr", orDash(cl.VirtualAddress))
	kv("Cert path", orDash(cl.ClientCertPath))
	kv("Key path", orDash(cl.ClientKeyPath))
	kv("Traffic", formatBytes(cl.RxBytes)+" / "+formatBytes(cl.TxBytes)+"  ("+formatBps(cl.RxBps)+" / "+formatBps(cl.TxBps)+")")
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("s/S suspend/resume · t reset traffic · c .ovpn · p/L profile link · D delete · esc back"))
	return panelStyle.Width(m.width - 4).Render(b.String())
}

func (m rootModel) viewConf() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.confTitle))
	b.WriteString("\n\n")
	lines := strings.Split(m.confBody, "\n")
	maxLines := m.height - 10
	if maxLines < 5 {
		maxLines = 5
	}
	start := m.scroll
	if start > len(lines) {
		start = max(0, len(lines)-1)
	}
	end := min(len(lines), start+maxLines)
	for _, line := range lines[start:end] {
		b.WriteString(dimStyle.Render(line))
		b.WriteString("\n")
	}
	if m.confQR != "" {
		b.WriteString("\n")
		b.WriteString(m.confQR)
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑↓ scroll · esc/enter back"))
	return panelStyle.Width(m.width - 4).Render(b.String())
}

func (m rootModel) viewProfileLink() string {
	link := m.profileLink
	if link == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Profile link · " + link.CommonName))
	b.WriteString("\n\n")
	kv := func(k, v string) {
		b.WriteString(labelStyle.Render(k))
		b.WriteString(valueStyle.Render(v))
		b.WriteString("\n")
	}
	kv("Download", link.DownloadURL)
	kv("Import", link.ImportURL)
	kv("Expires", link.ExpiresAt.Format("2006-01-02 15:04:05"))
	kv("Max uses", fmt.Sprintf("%d (used %d)", link.MaxUses, link.UseCount))
	if m.confQR != "" {
		b.WriteString("\n")
		b.WriteString(headerStyle.Render("QR (OpenVPN Connect import URL)"))
		b.WriteString("\n")
		b.WriteString(m.confQR)
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("share import URL with user · esc/enter back"))
	return panelStyle.Width(m.width - 4).Render(b.String())
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
