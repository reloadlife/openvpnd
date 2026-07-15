package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

const (
	tabInstances = 0
	tabClients   = 1
	tabPKI       = 2
	tabBinaries  = 3
	tabStats     = 4
	tabEvents    = 5
	tabCount     = 6

	modeList          = 0
	modeInstForm      = 1
	modeClientForm    = 2
	modeBinaryForm    = 3
	modeInstDetail    = 4
	modeClientDetail  = 5
	modeConfView      = 6
	modeProfileLink   = 7
	modeConfirm       = 8
	modeFilePick      = 9
	modeCAForm        = 10
	modeIssueCertForm = 11
	modePKIDetail     = 12
	modeDiscover      = 13
	modeAdoptForm     = 14
	modePrompt        = 15

	promptKillClient = "kill_client"
)

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmDelInst
	confirmDelClient
	confirmDelBinary
	confirmRevokeCert
)

// filePickKey special values (not form fields)
const filePickImportInstance = "__import_instance__"

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
	cas       []pkgapi.CA
	certs     []pkgapi.Certificate
	tlsCrypts []pkgapi.TLSCryptKey
	// sysStatus is a soft-fail line from /v1/system/info or /readyz.
	sysStatus string
	cursor    int

	// PKI list selection: "ca" | "cert" sections; pkiFilterCA filters certs
	pkiSection  string // "cas" | "certs"
	pkiFilterCA string
	detailCA    *pkgapi.CA
	detailCert  *pkgapi.Certificate

	err    string
	status string
	flash  string

	form formModel

	confirm     confirmKind
	confirmText string
	confirmArg  string
	confirmArg2 string

	// file picker overlay (returns to formReturnMode)
	filePick       filepicker.Model
	filePickKey    string // form field key to fill, or special import key
	formReturnMode int

	detailInst   *pkgapi.Instance
	detailClient *pkgapi.ServerClient
	confTitle    string
	confBody     string
	confQR       string
	profileLink  *pkgapi.ProfileLink
	scroll       int

	// Discover / adopt
	discoverCands  []pkgapi.OpenVPNCandidate
	discoverCursor int

	// Simple text prompt (e.g. kill CN)
	promptKind  string
	promptTitle string
	promptInput textinput.Model
	promptArg   string // instance name etc.

	fetchGen uint64
	busy     bool
	flashID  int
}

func newRootModel(cfg Config) rootModel {
	return rootModel{cfg: cfg, status: "connecting…", mode: modeList, pkiSection: "cas"}
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
		if m.isFormMode() {
			m.form.SetSize(msg.Width, m.formAreaHeight())
		}
		if m.mode == modeFilePick {
			m.filePick.SetHeight(max(8, m.formAreaHeight()-4))
		}
		return m, nil

	case tickMsg:
		if m.mode == modeList || m.mode == modeInstDetail || m.mode == modeClientDetail || m.mode == modePKIDetail {
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
			prevDiscover := len(m.discoverCands)
			m.instances = msg.instances
			m.clients = msg.clients
			m.binaries = msg.binaries
			m.stats = msg.stats
			m.events = msg.events
			m.cas = msg.cas
			m.certs = msg.certs
			m.tlsCrypts = msg.tlsCrypts
			m.sysStatus = msg.sysStatus
			m.discoverCands = filterUnmanaged(msg.discovered, msg.instances)
			m.status = "ok"
			n := len(m.discoverCands)
			if n > 0 && m.tab == tabInstances {
				m.status = fmt.Sprintf("ok · %d host openvpn live", n)
			}
			if m.cursor >= m.rowCount() {
				m.cursor = max(0, m.rowCount()-1)
			}
			m.refreshDetailPtrs()
			// Flash when auto-discover newly finds live unmanaged processes.
			if n > prevDiscover && n > 0 {
				m, flashCmd := m.setFlash(fmt.Sprintf("%d live openvpn unmanaged", n))
				return m, flashCmd
			}
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

	case clientCreatedMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.status = "error"
			// stay on form so user can fix
			if m.mode == modeClientForm {
				m.form.err = msg.err.Error()
			}
			return m, nil
		}
		m.err = ""
		m.confirm = confirmNone
		cl := msg.client
		m.detailClient = &cl
		var cmds []tea.Cmd
		m, flashCmd := m.setFlash(msg.flash)
		cmds = append(cmds, flashCmd)
		var fetch tea.Cmd
		m, fetch = m.beginFetch()
		cmds = append(cmds, fetch)
		if msg.link != nil {
			link := *msg.link
			m.profileLink = &link
			m.confQR = msg.qr
			m.mode = modeProfileLink
			if len(msg.warnings) > 0 {
				m.status = "warn: " + strings.Join(msg.warnings, "; ")
			}
		} else {
			m.mode = modeClientDetail
			if len(msg.warnings) > 0 {
				m.err = strings.Join(msg.warnings, "; ")
			}
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

	case discoverMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.mode = modeList
			return m, nil
		}
		m.discoverCands = msg.cands
		m.discoverCursor = 0
		m.err = ""
		m.mode = modeDiscover
		if len(msg.cands) == 0 {
			m.status = "no running openvpn processes"
		}
		return m, nil

	case adoptDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			if m.mode == modeAdoptForm {
				m.form.err = msg.err.Error()
			}
			return m, nil
		}
		m.err = ""
		flash := "adopted"
		if msg.resp.Instance != nil && msg.resp.Instance.Name != "" {
			flash += " " + msg.resp.Instance.Name
		}
		if len(msg.resp.Notes) > 0 {
			flash += " · " + msg.resp.Notes[0]
		}
		m.mode = modeList
		m, flashCmd := m.setFlash(flash)
		var fetch tea.Cmd
		m, fetch = m.beginFetch()
		return m, tea.Batch(flashCmd, fetch)

	case tea.KeyMsg:
		if m.mode == modeFilePick {
			return m.handleFilePickKey(msg)
		}
		if m.mode == modePrompt {
			return m.handlePromptKey(msg)
		}
		if m.mode == modeDiscover {
			return m.handleDiscoverKey(msg)
		}
		if m.isFormMode() {
			return m.handleFormKeyAll(msg)
		}
		return m.handleKey(msg)
	}

	// Non-key messages while file picking (dir reads, etc.)
	if m.mode == modeFilePick {
		var cmd tea.Cmd
		m.filePick, cmd = m.filePick.Update(msg)
		if did, path := m.filePick.DidSelectFile(msg); did {
			return m.applyPickedFile(path)
		}
		return m, cmd
	}
	return m, nil
}

func (m rootModel) isFormMode() bool {
	return m.mode == modeInstForm || m.mode == modeClientForm || m.mode == modeBinaryForm ||
		m.mode == modeCAForm || m.mode == modeIssueCertForm || m.mode == modeAdoptForm
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
	if m.detailCA != nil {
		for i := range m.cas {
			if m.cas[i].Name == m.detailCA.Name {
				ca := m.cas[i]
				m.detailCA = &ca
				break
			}
		}
	}
	if m.detailCert != nil {
		for i := range m.certs {
			if m.certs[i].ID == m.detailCert.ID {
				cert := m.certs[i]
				m.detailCert = &cert
				break
			}
		}
	}
}

func (m rootModel) filteredCerts() []pkgapi.Certificate {
	if m.pkiFilterCA == "" {
		return m.certs
	}
	var out []pkgapi.Certificate
	for _, c := range m.certs {
		if c.CAName == m.pkiFilterCA {
			out = append(out, c)
		}
	}
	return out
}

func (m rootModel) rowCount() int {
	switch m.tab {
	case tabInstances:
		// managed instances + live unmanaged host processes
		return len(m.instances) + len(m.discoverCands)
	case tabClients:
		return len(m.clients)
	case tabPKI:
		if m.pkiSection == "certs" {
			return len(m.filteredCerts())
		}
		return len(m.cas)
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
	case modePKIDetail:
		return m.handlePKIDetailKey(key)
	case modeConfView, modeProfileLink:
		if key == "esc" || key == "q" || key == "enter" {
			if m.detailClient != nil {
				m.mode = modeClientDetail
			} else if m.detailInst != nil {
				m.mode = modeInstDetail
			} else if m.detailCA != nil || m.detailCert != nil {
				m.mode = modePKIDetail
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
		case confirmRevokeCert:
			id, _ := parseIntField(m.confirmArg)
			m.confirm = confirmNone
			m.mode = modeList
			return m.startMutate(doRevokeCert(m.cfg.Client, int64(id), m.confirmArg2))
		}
	case "n", "N", "esc":
		m.confirm = confirmNone
		if m.detailCert != nil || m.detailCA != nil {
			m.mode = modePKIDetail
		} else {
			m.mode = modeList
		}
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
		// file fields: enter still saves the form (browse via space/ctrl+o)
		switch m.mode {
		case modeInstForm:
			return m.submitInstForm()
		case modeClientForm:
			return m.submitClientForm()
		case modeBinaryForm:
			return m.submitBinaryForm()
		case modeCAForm:
			return m.submitCAForm()
		case modeIssueCertForm:
			return m.submitIssueCertForm()
		case modeAdoptForm:
			return m.submitAdoptForm()
		}
	case "ctrl+o":
		if m.form.Focused().Kind == fieldFile {
			return m.openFilePickerForFocus()
		}
	case " ":
		if m.form.Focused().Kind == fieldFile {
			return m.openFilePickerForFocus()
		}
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	return m, cmd
}

func (m rootModel) openFilePickerForFocus() (tea.Model, tea.Cmd) {
	fd := m.form.Focused()
	if fd.Key == "" || fd.Kind != fieldFile {
		return m, nil
	}
	fp := filepicker.New()
	fp.FileAllowed = true
	fp.DirAllowed = false
	fp.ShowHidden = false
	fp.ShowPermissions = false
	fp.ShowSize = true
	fp.AutoHeight = false
	fp.SetHeight(max(8, m.formAreaHeight()-4))
	if len(fd.AllowedTypes) > 0 {
		fp.AllowedTypes = fd.AllowedTypes
	}
	// Start from existing value dir, else cwd / home
	start := strings.TrimSpace(m.form.Get(fd.Key))
	switch {
	case start != "":
		if st, err := os.Stat(start); err == nil && !st.IsDir() {
			fp.CurrentDirectory = filepath.Dir(start)
		} else if err == nil && st.IsDir() {
			fp.CurrentDirectory = start
		} else {
			fp.CurrentDirectory = filepath.Dir(start)
		}
	default:
		if wd, err := os.Getwd(); err == nil {
			fp.CurrentDirectory = wd
		} else if home, err := os.UserHomeDir(); err == nil {
			fp.CurrentDirectory = home
		} else {
			fp.CurrentDirectory = "/"
		}
	}
	if abs, err := filepath.Abs(fp.CurrentDirectory); err == nil {
		fp.CurrentDirectory = abs
	}
	m.filePick = fp
	m.filePickKey = fd.Key
	m.formReturnMode = m.mode
	m.mode = modeFilePick
	m.form.err = ""
	return m, m.filePick.Init()
}

func (m rootModel) handleFilePickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc cancels only when at root of stack is hard; use ctrl+c handled globally elsewhere.
	// Bubbletea filepicker uses esc as "back" directory — use ctrl+q / ctrl+g to cancel.
	switch msg.String() {
	case "ctrl+g", "ctrl+q":
		m.mode = m.formReturnMode
		return m, nil
	}
	var cmd tea.Cmd
	m.filePick, cmd = m.filePick.Update(msg)
	if did, path := m.filePick.DidSelectFile(msg); did {
		return m.applyPickedFile(path)
	}
	return m, cmd
}

func (m rootModel) applyPickedFile(path string) (tea.Model, tea.Cmd) {
	key := m.filePickKey
	m.mode = m.formReturnMode
	if path == "" {
		return m, nil
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}

	// Instance import from Instances tab (key I)
	if key == filePickImportInstance {
		data, err := os.ReadFile(path)
		if err != nil {
			m.err = "import read: " + err.Error()
			m.mode = modeList
			return m, nil
		}
		return m.startMutate(doImportInstance(m.cfg.Client, string(data), path))
	}

	m.form.SetValue(key, path)

	// Auto-import client .ovpn when picking profile
	if key == "profile" && m.formReturnMode == modeInstForm {
		p, err := parseClientProfileFile(path, importDestDir(path))
		if err != nil {
			m.form.err = "profile import: " + err.Error()
			m.form.note = "Selected " + path + " (manual remote/cert still required if parse failed)"
			return m, nil
		}
		patch := map[string]string{"profile": path}
		if p.Remotes != "" {
			patch["remote"] = p.Remotes
		}
		if p.Proto != "" {
			patch["proto"] = p.Proto
		}
		if p.DevType != "" {
			patch["dev_type"] = p.DevType
		}
		if p.AuthMode != "" {
			patch["auth_mode"] = p.AuthMode
		}
		if p.Cipher != "" {
			patch["cipher"] = p.Cipher
		}
		if p.Auth != "" {
			patch["auth"] = p.Auth
		}
		if p.DataCiphers != "" {
			patch["data_ciphers"] = p.DataCiphers
		}
		if p.CAPath != "" {
			patch["pki_ca"] = p.CAPath
		}
		if p.CertPath != "" {
			patch["pki_cert"] = p.CertPath
		}
		if p.KeyPath != "" {
			patch["pki_key"] = p.KeyPath
		}
		if p.TLSCrypt != "" {
			patch["tls_crypt_path"] = p.TLSCrypt
		}
		if p.StaticKey != "" {
			patch["static_key"] = p.StaticKey
		}
		if p.Extra != "" {
			// merge with existing extra
			prev := m.form.Get("extra")
			if prev != "" && !strings.Contains(prev, p.Extra) {
				patch["extra"] = strings.TrimSpace(prev+"\n"+p.Extra) + "\n"
			} else if prev == "" {
				patch["extra"] = p.Extra
			}
		}
		// Prefer explicit-exit-notify for UDP clients
		if m.form.Get("features") == "" {
			patch["features"] = "explicit_exit_notify"
		}
		m.form.ApplyValues(patch)
		m.form.err = ""
		m.form.note = "Imported profile → remotes + cert material filled. Review and enter to create."
	}
	return m, nil
}

func (m rootModel) handleListKey(key string) (tea.Model, tea.Cmd) {
	// PKI-specific keys first
	if m.tab == tabPKI {
		if handled, model, cmd := m.handlePKIListKey(key); handled {
			return model, cmd
		}
	}
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
		m.tab = tabPKI
		m.cursor = 0
		if m.pkiSection == "" {
			m.pkiSection = "cas"
		}
	case "4":
		m.tab = tabBinaries
		m.cursor = 0
	case "5":
		m.tab = tabStats
		m.cursor = 0
	case "6":
		m.tab = tabEvents
		m.cursor = 0
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		m.cursor = 0
	case "shift+tab":
		m.tab = (m.tab + tabCount - 1) % tabCount
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
	case "I":
		if m.tab == tabInstances {
			return m.openInstanceImportPicker()
		}
	case "A":
		if m.tab == tabInstances {
			return m.startDiscover()
		}
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

func (m rootModel) handlePKIListKey(key string) (bool, tea.Model, tea.Cmd) {
	switch key {
	case "c":
		m.pkiSection = "cas"
		m.cursor = 0
		return true, m, nil
	case "t":
		m.pkiSection = "certs"
		m.cursor = 0
		return true, m, nil
	case "f":
		// filter certs by selected CA (when on CAs section)
		if m.pkiSection == "cas" && m.cursor >= 0 && m.cursor < len(m.cas) {
			m.pkiFilterCA = m.cas[m.cursor].Name
			m.pkiSection = "certs"
			m.cursor = 0
			return true, m, nil
		}
		// clear filter when already on certs
		if m.pkiSection == "certs" {
			m.pkiFilterCA = ""
			m.cursor = 0
			return true, m, nil
		}
	case "n":
		m.form = newForm("Create CA", caCreateFields(), map[string]string{"name": "default"})
		m.form.note = "Creates a managed CA under openvpn.pki_dir."
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeCAForm
		return true, m, nil
	case "i":
		names := m.caNames()
		vals := map[string]string{"kind": "client"}
		if len(names) == 1 {
			vals["ca_name"] = names[0]
		} else if m.pkiFilterCA != "" {
			vals["ca_name"] = m.pkiFilterCA
		} else if m.pkiSection == "cas" && m.cursor >= 0 && m.cursor < len(m.cas) {
			vals["ca_name"] = m.cas[m.cursor].Name
		}
		m.form = newForm("Issue certificate", issueCertFields(names), vals)
		m.form.note = "Issues a leaf cert under the selected CA."
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeIssueCertForm
		return true, m, nil
	case "r":
		if m.pkiSection != "certs" {
			return false, m, nil
		}
		certs := m.filteredCerts()
		if m.cursor < 0 || m.cursor >= len(certs) {
			return true, m, nil
		}
		cert := certs[m.cursor]
		if cert.Revoked {
			m.err = "cert already revoked"
			return true, m, nil
		}
		m.confirm = confirmRevokeCert
		m.confirmArg = fmt.Sprintf("%d", cert.ID)
		m.confirmArg2 = "unspecified"
		m.confirmText = fmt.Sprintf("Revoke cert #%d %s (%s)? [y/n]", cert.ID, cert.CommonName, cert.Kind)
		m.mode = modeConfirm
		return true, m, nil
	case "R":
		caName := m.pkiFilterCA
		if caName == "" && m.pkiSection == "cas" && m.cursor >= 0 && m.cursor < len(m.cas) {
			caName = m.cas[m.cursor].Name
		}
		if caName == "" && len(m.cas) == 1 {
			caName = m.cas[0].Name
		}
		if caName == "" {
			m.err = "select a CA (or filter) to rebuild CRL"
			return true, m, nil
		}
		mod, cmd := m.startMutate(doRebuildCRL(m.cfg.Client, caName))
		return true, mod, cmd
	}
	return false, m, nil
}

func (m rootModel) caNames() []string {
	out := make([]string, 0, len(m.cas))
	for _, c := range m.cas {
		out = append(out, c.Name)
	}
	return out
}

func (m rootModel) openInstanceImportPicker() (tea.Model, tea.Cmd) {
	fp := filepicker.New()
	fp.FileAllowed = true
	fp.DirAllowed = false
	fp.ShowHidden = false
	fp.ShowPermissions = false
	fp.ShowSize = true
	fp.AutoHeight = false
	fp.SetHeight(max(8, m.formAreaHeight()-4))
	fp.AllowedTypes = []string{".conf", ".ovpn"}
	if wd, err := os.Getwd(); err == nil {
		fp.CurrentDirectory = wd
	} else if home, err := os.UserHomeDir(); err == nil {
		fp.CurrentDirectory = home
	} else {
		fp.CurrentDirectory = "/"
	}
	if abs, err := filepath.Abs(fp.CurrentDirectory); err == nil {
		fp.CurrentDirectory = abs
	}
	m.filePick = fp
	m.filePickKey = filePickImportInstance
	m.formReturnMode = modeList
	m.mode = modeFilePick
	return m, m.filePick.Init()
}

func (m rootModel) openCreateForm() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabInstances:
		bins := make([]string, 0, len(m.binaries))
		for _, b := range m.binaries {
			bins = append(bins, b.Name)
		}
		m.form = newForm("Create instance (←/→ Role switches fields)", instanceCreateFields(bins), map[string]string{
			"role": "server", "proto": "udp", "topology": "subnet", "dev_type": "tun",
			"auth_mode": "pki", "issue_cert": "y", "tls_crypt": "y", "create_ca": "y",
			"push_dns": "1.1.1.1",
		})
		m.form.note = "Server: leave name/port/network empty for auto. issue_cert+create_ca → full mTLS."
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeInstForm
	case tabClients:
		servers := m.serverNames()
		vals := map[string]string{
			"issue_cert": "y", "mint_link": "y", "link_ttl": "24h", "link_uses": "1",
		}
		if len(servers) == 1 {
			vals["instance"] = servers[0]
		}
		m.form = newForm("New VPN user — cert + install link in one step", clientCreateFields(servers), vals)
		m.form.note = "Enter a username (CN). We auto-issue a cert, pick a free IP, and show a QR install link."
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeClientForm
	case tabPKI:
		// same as n on PKI list
		m.form = newForm("Create CA", caCreateFields(), map[string]string{"name": "default"})
		m.form.note = "Creates a managed CA under openvpn.pki_dir."
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeCAForm
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
		// Unmanaged live processes sit after managed instances in the list.
		if m.cursor >= len(m.instances) {
			idx := m.cursor - len(m.instances)
			if idx < 0 || idx >= len(m.discoverCands) {
				return m, nil
			}
			c := m.discoverCands[idx]
			return m.openAdoptForm(c.ConfPath, c.PID, c.Binary)
		}
		if m.cursor < 0 || m.cursor >= len(m.instances) {
			return m, nil
		}
		inst := m.instances[m.cursor]
		m.detailInst = &inst
		m.detailClient = nil
		m.detailCA = nil
		m.detailCert = nil
		m.mode = modeInstDetail
	case tabClients:
		if m.cursor < 0 || m.cursor >= len(m.clients) {
			return m, nil
		}
		cl := m.clients[m.cursor]
		m.detailClient = &cl
		m.detailInst = nil
		m.detailCA = nil
		m.detailCert = nil
		m.mode = modeClientDetail
	case tabPKI:
		if m.pkiSection == "certs" {
			certs := m.filteredCerts()
			if m.cursor < 0 || m.cursor >= len(certs) {
				return m, nil
			}
			cert := certs[m.cursor]
			m.detailCert = &cert
			m.detailCA = nil
			m.detailInst = nil
			m.detailClient = nil
			m.mode = modePKIDetail
		} else {
			if m.cursor < 0 || m.cursor >= len(m.cas) {
				return m, nil
			}
			ca := m.cas[m.cursor]
			m.detailCA = &ca
			m.detailCert = nil
			m.detailInst = nil
			m.detailClient = nil
			m.mode = modePKIDetail
		}
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
	case "s":
		// Soft reload via management SIGUSR1 (reconnect / re-read)
		return m.startMutate(doMgmtSignal(m.cfg.Client, name, "SIGUSR1"))
	case "m":
		// Live management status dump
		if m.busy {
			return m, nil
		}
		m.busy = true
		return m, doMgmtStatusDump(m.cfg.Client, name)
	case "k":
		// Kill connected client by CN
		ti := textinput.New()
		ti.Placeholder = "common name or IP:port"
		ti.CharLimit = 256
		ti.Width = max(24, m.width-20)
		ti.Focus()
		m.promptKind = promptKillClient
		m.promptTitle = "Kill connected client on " + name
		m.promptInput = ti
		m.promptArg = name
		m.mode = modePrompt
		return m, textinput.Blink
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
			m.form = newForm("New VPN user on "+name, clientCreateFields([]string{name}), map[string]string{
				"instance": name, "issue_cert": "y", "mint_link": "y", "link_ttl": "24h", "link_uses": "1",
			})
			m.form.note = "Username (CN) only required. Cert + free IP + install QR are automatic."
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

func (m rootModel) startDiscover() (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	m.busy = true
	m.status = "discovering openvpn…"
	return m, doDiscoverOpenVPN(m.cfg.Client)
}

func (m rootModel) handleDiscoverKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q":
		m.mode = modeList
		m.discoverCands = nil
		return m, nil
	case "j", "down":
		if m.discoverCursor < len(m.discoverCands)-1 {
			m.discoverCursor++
		}
	case "k", "up":
		if m.discoverCursor > 0 {
			m.discoverCursor--
		}
	case "g":
		m.discoverCursor = 0
	case "G":
		m.discoverCursor = max(0, len(m.discoverCands)-1)
	case "r":
		return m.startDiscover()
	case "n":
		// manual adopt without a discover pick
		return m.openAdoptForm("", 0, "")
	case "enter", "l", "a":
		if len(m.discoverCands) == 0 {
			return m.openAdoptForm("", 0, "")
		}
		if m.discoverCursor < 0 || m.discoverCursor >= len(m.discoverCands) {
			return m, nil
		}
		c := m.discoverCands[m.discoverCursor]
		return m.openAdoptForm(c.ConfPath, c.PID, c.Binary)
	}
	return m, nil
}

func (m rootModel) openAdoptForm(confPath string, pid int, binaryPath string) (tea.Model, tea.Cmd) {
	vals := map[string]string{
		"conf_path": confPath,
		"take_over": "y",
	}
	if pid > 0 {
		vals["pid"] = fmt.Sprintf("%d", pid)
	}
	if binaryPath != "" {
		vals["binary_path"] = binaryPath
	}
	m.form = newForm("Adopt instance from host conf", adoptInstanceFields(), vals)
	m.form.note = "Uses conf + binary from host. take_over stops verified openvpn PID then starts under openvpnd with management socket."
	m.form.SetSize(m.width, m.formAreaHeight())
	m.mode = modeAdoptForm
	return m, nil
}

func (m rootModel) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.mode = modeInstDetail
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.promptInput.Value())
		if val == "" {
			m.err = "value required"
			return m, nil
		}
		switch m.promptKind {
		case promptKillClient:
			name := m.promptArg
			m.mode = modeInstDetail
			return m.startMutate(doMgmtKillClient(m.cfg.Client, name, val))
		}
		m.mode = modeList
		return m, nil
	}
	var cmd tea.Cmd
	m.promptInput, cmd = m.promptInput.Update(msg)
	return m, cmd
}

func (m rootModel) submitAdoptForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	confPath := strings.TrimSpace(v["conf_path"])
	if confPath == "" {
		m.form.err = "conf_path required (absolute path on daemon host)"
		return m, nil
	}
	if !filepath.IsAbs(confPath) {
		m.form.err = "conf_path must be absolute"
		return m, nil
	}
	pid, _ := parseIntField(v["pid"])
	req := pkgapi.AdoptInstanceRequest{
		ConfPath:       confPath,
		Name:           strings.TrimSpace(v["name"]),
		PublicEndpoint: strings.TrimSpace(v["public_endpoint"]),
		BinaryPath:     strings.TrimSpace(v["binary_path"]),
		TakeOver:       truthy(v["take_over"]),
		PID:            pid,
	}
	return m.startMutate(doAdoptInstance(m.cfg.Client, req))
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
	case "i":
		// Issue client cert when paths missing
		if cl.ClientCertPath != "" && cl.ClientKeyPath != "" {
			m.err = "client already has cert paths — delete/recreate or use API to re-issue"
			return m, nil
		}
		return m.startMutate(doIssueClientCert(m.cfg.Client, cl.InstanceName, cl.CommonName))
	case "D":
		m.confirm = confirmDelClient
		m.confirmArg = cl.InstanceName
		m.confirmArg2 = cl.CommonName
		m.confirmText = "Delete client " + cl.CommonName + "? [y/n]"
		m.mode = modeConfirm
	}
	return m, nil
}

func (m rootModel) handlePKIDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "h":
		m.mode = modeList
		m.detailCA = nil
		m.detailCert = nil
	case "r":
		if m.detailCert == nil {
			return m, nil
		}
		if m.detailCert.Revoked {
			m.err = "cert already revoked"
			return m, nil
		}
		m.confirm = confirmRevokeCert
		m.confirmArg = fmt.Sprintf("%d", m.detailCert.ID)
		m.confirmArg2 = "unspecified"
		m.confirmText = fmt.Sprintf("Revoke cert #%d %s? [y/n]", m.detailCert.ID, m.detailCert.CommonName)
		m.mode = modeConfirm
	case "R":
		caName := ""
		if m.detailCA != nil {
			caName = m.detailCA.Name
		} else if m.detailCert != nil {
			caName = m.detailCert.CAName
		}
		if caName == "" {
			return m, nil
		}
		return m.startMutate(doRebuildCRL(m.cfg.Client, caName))
	case "i":
		// issue from CA detail
		names := m.caNames()
		vals := map[string]string{"kind": "client"}
		if m.detailCA != nil {
			vals["ca_name"] = m.detailCA.Name
		}
		m.form = newForm("Issue certificate", issueCertFields(names), vals)
		m.form.SetSize(m.width, m.formAreaHeight())
		m.mode = modeIssueCertForm
	case "f":
		if m.detailCA != nil {
			m.pkiFilterCA = m.detailCA.Name
			m.pkiSection = "certs"
			m.cursor = 0
			m.mode = modeList
			m.detailCA = nil
		}
	}
	return m, nil
}

func (m rootModel) submitInstForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	role := strings.ToLower(strings.TrimSpace(v["role"]))
	if role == "" {
		role = "server"
	}
	port, _ := parseIntField(v["port"])
	req := pkgapi.InstanceCreateRequest{
		Name: v["name"], Role: role, BinaryName: v["binary"], Port: port, Proto: v["proto"],
		LocalBind: v["local_bind"], DevType: v["dev_type"], Device: v["device"],
		AuthMode: v["auth_mode"], DataCiphers: v["data_ciphers"], AuthDigest: v["auth"], Cipher: v["cipher"],
		PKICaPath: v["pki_ca"], PKICertPath: v["pki_cert"], PKIKeyPath: v["pki_key"],
		PKITLSCryptPath: v["tls_crypt_path"], StaticKeyPath: v["static_key"],
		ExtraDirectives: v["extra"],
		FeatureSets:     splitCSV(v["features"]),
		IfconfigIPv6:    v["ifconfig_ipv6"],
	}
	if pl := strings.TrimSpace(v["plugin"]); pl != "" {
		parts := strings.Fields(pl)
		p := pkgapi.Plugin{Path: parts[0]}
		if len(parts) > 1 {
			p.Args = parts[1:]
		}
		req.Plugins = []pkgapi.Plugin{p}
	}

	if n, err := parseInt64Field(v["inst_bandwidth_rx"]); err == nil {
		req.BandwidthRxBps = n
	}
	if n, err := parseInt64Field(v["inst_bandwidth_tx"]); err == nil {
		req.BandwidthTxBps = n
	}

	if role == "server" {
		issue := truthy(v["issue_cert"])
		tlsCrypt := truthy(v["tls_crypt"])
		req.ServerNetwork = v["network"]
		req.Topology = v["topology"]
		req.PublicEndpoint = v["public_endpoint"]
		req.PushDNS = splitCSV(v["push_dns"])
		req.PushRoutes = splitCSV(v["push_routes"])
		req.PushDomain = v["push_domain"]
		req.RedirectGateway = truthy(v["redirect_gw"])
		req.CAName = v["ca_name"]
		req.ServerCN = v["server_cn"]
		req.CreateCAIfEmpty = truthy(v["create_ca"])
		req.IssueServerCert = &issue
		req.GenerateTLSCrypt = &tlsCrypt

		req.MaxClients, _ = parseIntField(v["max_clients"])
		req.TLSVersionMin = v["tls_version_min"]
		req.TLSGroups = v["tls_groups"]
		req.TLSCipher = v["tls_cipher"]
		req.TLSCiphersuites = v["tls_ciphersuites"]
		req.TLSCertProfile = v["tls_cert_profile"]
		req.TunMTU, _ = parseIntField(v["tun_mtu"])
		req.ServerIPv6 = v["server_ipv6"]
		req.BridgeMode = truthy(v["bridge_mode"])
		req.BridgeGateway = v["bridge_gateway"]
		req.BridgePoolStart = v["bridge_pool_start"]
		req.BridgePoolEnd = v["bridge_pool_end"]
		req.BridgeNetmask = v["bridge_netmask"]
		req.AuthUserPassVerify = v["auth_user_pass_verify"]
		req.ScriptSecurity, _ = parseIntField(v["script_security"])
		req.UsernameAsCommonName = truthy(v["username_as_cn"])
	} else {
		// client instance: outbound connection (whole-tunnel BW via inst_bandwidth_*)
		req.Remote = v["remote"]
		req.AuthUserPass = truthy(v["auth_user_pass"])
		req.AuthUserPassFile = v["auth_user_pass_file"]
		// Don't auto-issue server certs for clients
		f := false
		req.IssueServerCert = &f
		req.GenerateTLSCrypt = &f
		req.CreateCAIfEmpty = false
		if req.FeatureSets == nil && v["features"] == "" {
			// mild default for UDP clients
			req.FeatureSets = []string{"explicit_exit_notify"}
		}
	}

	// Client needs remotes — surface form error instead of opaque API fail
	if role == "client" && strings.TrimSpace(v["remote"]) == "" {
		m.form.err = "client requires Remote(s) or a Profile .ovpn with remotes"
		return m, nil
	}
	return m.startMutate(doCreateInstance(m.cfg.Client, req))
}

func (m rootModel) submitClientForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	inst := v["instance"]
	cn := strings.TrimSpace(v["cn"])
	if inst == "" || strings.HasPrefix(inst, "(") || cn == "" {
		m.form.err = "server instance and username (CN) required"
		return m, nil
	}
	issue := truthy(v["issue_cert"])
	mint := truthy(v["mint_link"])
	req := pkgapi.ClientCreateRequest{
		CommonName: cn, Name: v["name"], StaticIP: v["static_ip"], Notes: v["notes"],
		IRoutes:         splitCSV(v["iroutes"]),
		PushDNS:         splitCSV(v["push_dns"]),
		PushDomain:      v["push_domain"],
		RedirectGateway: truthy(v["redirect_gw"]),
		DisablePush:     splitCSV(v["disable_push"]),
		ClientCertPath:  v["cert_path"], ClientKeyPath: v["key_path"],
		IssueCert: &issue, MintProfileLink: mint,
		ProfileLinkTTL: v["link_ttl"], ProfileLinkNote: v["notes"],
	}
	if n, err := parseInt64Field(v["bandwidth_rx"]); err == nil {
		req.BandwidthRxBps = n
	}
	if n, err := parseInt64Field(v["bandwidth_tx"]); err == nil {
		req.BandwidthTxBps = n
	}
	if n, err := parseInt64Field(v["traffic_limit"]); err == nil {
		req.TrafficLimitBytes = n
	}
	if uses := strings.TrimSpace(v["link_uses"]); uses != "" {
		if n, err := parseIntField(uses); err == nil {
			req.ProfileLinkMaxUses = &n
		}
	}
	// Manual paths imply no auto-issue unless user forced both (API rejects)
	if !issue && (v["cert_path"] == "" || v["key_path"] == "") && mint {
		m.form.err = "Profile link needs certs — turn Issue cert ON, or set cert+key paths"
		return m, nil
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

func (m rootModel) submitCAForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	cn := strings.TrimSpace(v["common_name"])
	if cn == "" {
		m.form.err = "common_name required"
		return m, nil
	}
	name := strings.TrimSpace(v["name"])
	if name == "" {
		name = "default"
	}
	return m.startMutate(doCreateCA(m.cfg.Client, pkgapi.CreateCARequest{
		Name: name, CommonName: cn, Org: strings.TrimSpace(v["org"]),
	}))
}

func (m rootModel) submitIssueCertForm() (tea.Model, tea.Cmd) {
	v := m.form.Values()
	ca := strings.TrimSpace(v["ca_name"])
	cn := strings.TrimSpace(v["common_name"])
	kind := strings.TrimSpace(v["kind"])
	if ca == "" || strings.HasPrefix(ca, "(") || cn == "" {
		m.form.err = "CA and common_name required"
		return m, nil
	}
	if kind == "" {
		kind = "client"
	}
	return m.startMutate(doIssueCert(m.cfg.Client, pkgapi.IssueCertRequest{
		CAName: ca, Kind: kind, CommonName: cn,
	}))
}

// ─── View ───────────────────────────────────────────────────────────

func (m rootModel) View() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}

	// Top bar: full width status chrome
	status := fmt.Sprintf(" openvpnctl  ·  %s  ·  %s ", m.cfg.Endpoint, m.status)
	if m.flash != "" {
		status += " ✓ " + m.flash + " "
	}
	if m.busy {
		status += " … "
	}
	if m.err != "" && m.mode == modeList {
		status += "  err: " + trunc(m.err, max(10, w/4)) + " "
	}
	header := statusStyle.Width(w).Render(status)

	// Bottom bar: full width contextual help
	footer := helpStyle.Width(w).Background(cBarBg).Foreground(cBarFg).Padding(0, 1).Render(" " + m.chromeHelp() + " ")

	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	mainH := h - headerH - footerH
	if mainH < 1 {
		mainH = 1
	}

	var mid string
	switch m.mode {
	case modeConfirm:
		mid = panelStyle.Width(w).Height(mainH).MaxHeight(mainH).Render(
			warnStyle.Render("Confirm") + "\n\n" + m.confirmText + "\n\n" +
				helpStyle.Render("[y] yes   [n / esc] cancel"),
		)
	case modeInstForm, modeClientForm, modeBinaryForm, modeCAForm, modeIssueCertForm, modeAdoptForm:
		m.form.SetSize(w, mainH)
		mid = m.form.View()
	case modeFilePick:
		title := "Select file → " + m.filePickKey
		if m.filePickKey == filePickImportInstance {
			title = "Import instance · pick .conf / .ovpn"
		}
		var b strings.Builder
		b.WriteString(titleStyle.Render(title))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(m.filePick.CurrentDirectory))
		b.WriteString("\n\n")
		b.WriteString(m.filePick.View())
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("j/k move  ·  enter select  ·  h/backspace parent  ·  ctrl+g cancel"))
		mid = panelStyle.Width(w).Height(mainH).MaxHeight(mainH).Render(b.String())
	case modeDiscover:
		mid = fillHeight(m.viewDiscover(w, mainH), w, mainH)
	case modePrompt:
		mid = fillHeight(m.viewPrompt(w, mainH), w, mainH)
	case modeInstDetail:
		mid = fillHeight(m.viewInstDetail(w, mainH), w, mainH)
	case modeClientDetail:
		mid = fillHeight(m.viewClientDetail(w, mainH), w, mainH)
	case modePKIDetail:
		mid = fillHeight(m.viewPKIDetail(w, mainH), w, mainH)
	case modeConfView:
		mid = fillHeight(m.viewConf(w, mainH), w, mainH)
	case modeProfileLink:
		mid = fillHeight(m.viewProfileLink(w, mainH), w, mainH)
	default:
		var b strings.Builder
		b.WriteString(m.renderTabs())
		b.WriteString("\n")
		if m.err != "" {
			b.WriteString(errStyle.Render("error: " + m.err))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		switch m.tab {
		case tabInstances:
			b.WriteString(m.viewInstanceList(w))
		case tabClients:
			b.WriteString(m.viewClientList(w))
		case tabPKI:
			b.WriteString(m.viewPKIList(w, mainH))
		case tabBinaries:
			b.WriteString(m.viewBinaryList(w))
		case tabStats:
			b.WriteString(m.viewStats(w))
		case tabEvents:
			b.WriteString(m.viewEvents(w, mainH))
		}
		mid = fillHeight(b.String(), w, mainH)
	}

	mid = fillHeight(mid, w, mainH)
	return lipgloss.JoinVertical(lipgloss.Left, header, mid, footer)
}

func (m rootModel) chromeHelp() string {
	switch m.mode {
	case modeInstForm, modeClientForm, modeBinaryForm, modeCAForm, modeIssueCertForm, modeAdoptForm:
		return "tab/↑↓ fields  ·  ←/→ role & toggles  ·  space/ctrl+o browse file  ·  enter save  ·  esc cancel"
	case modeFilePick:
		return "j/k navigate  ·  enter select file  ·  h parent dir  ·  ctrl+g cancel"
	case modeConfirm:
		return "y confirm  ·  n/esc cancel"
	case modeDiscover:
		return "j/k pick process  ·  enter/a adopt  ·  n manual path  ·  r refresh  ·  esc back"
	case modePrompt:
		return "type value  ·  enter submit  ·  esc cancel"
	case modeInstDetail:
		return "u/d up/down  ·  r restart  ·  s SIGUSR1  ·  m status  ·  k kill CN  ·  e export  ·  n client  ·  D delete  ·  esc"
	case modeClientDetail:
		return "s/S suspend/resume  ·  t reset traffic  ·  c .ovpn  ·  p/L profile link  ·  i issue-cert  ·  D delete  ·  esc back"
	case modePKIDetail:
		return "r revoke  ·  R rebuild CRL  ·  i issue  ·  f filter certs  ·  esc back"
	case modeConfView, modeProfileLink:
		return "↑↓ scroll  ·  esc/enter back"
	default:
		return m.listHelp()
	}
}

func (m rootModel) listHelp() string {
	base := "1-6 tabs  ·  j/k  ·  enter detail  ·  n new  ·  r refresh  ·  R reconcile  ·  q quit"
	switch m.tab {
	case tabInstances:
		return base + "  ·  u/d up/down  ·  I import  ·  A discover  ·  enter adopt live  ·  D delete  ·  live unmanaged auto-discover"
	case tabClients:
		return base + "  ·  s/S suspend/resume  ·  D delete"
	case tabPKI:
		return "1-6 tabs  ·  c/t CAs|certs  ·  n create CA  ·  i issue  ·  r revoke  ·  R rebuild CRL  ·  f filter  ·  enter detail  ·  q quit"
	case tabBinaries:
		return base + "  ·  D delete"
	default:
		return base
	}
}

func (m rootModel) renderTabs() string {
	names := []string{"Instances", "Clients", "PKI", "Binaries", "Stats", "Events"}
	parts := make([]string, len(names))
	for i, n := range names {
		label := fmt.Sprintf("%d %s", i+1, n)
		if i == m.tab {
			parts[i] = tabActive.Render(label)
		} else {
			parts[i] = tabInactive.Render(label)
		}
	}
	tabs := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	// stretch tabs row to full width
	pad := m.width - lipgloss.Width(tabs)
	if pad < 0 {
		pad = 0
	}
	return tabs + strings.Repeat(" ", pad)
}

// colWidths distributes terminal width across column min widths; remainder goes to last flexible col.
func colWidths(total int, mins []int, flex int) []int {
	if total < 1 {
		total = 80
	}
	out := make([]int, len(mins))
	copy(out, mins)
	sum := 0
	for _, n := range out {
		sum += n
	}
	// spaces between columns
	gaps := len(out) - 1
	if gaps < 0 {
		gaps = 0
	}
	avail := total - gaps
	if avail < sum {
		// shrink from the end
		deficit := sum - avail
		for i := len(out) - 1; i >= 0 && deficit > 0; i-- {
			cut := min(deficit, max(0, out[i]-4))
			out[i] -= cut
			deficit -= cut
		}
		return out
	}
	if flex < 0 || flex >= len(out) {
		flex = len(out) - 1
	}
	out[flex] += avail - sum
	return out
}

func padCell(s string, width int) string {
	if width <= 0 {
		return s
	}
	// visible width for plain text; ANSI-free truncation first
	s = trunc(s, width)
	n := lipgloss.Width(s)
	if n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func (m rootModel) viewInstanceList(w int) string {
	// NAME ROLE STATE BINARY PORT CLIENTS RX TX RX/s TX/s
	cw := colWidths(w-2, []int{16, 8, 6, 12, 6, 8, 10, 10, 10, 10}, 0)
	var b strings.Builder
	hdr := fmt.Sprintf("%s %s %s %s %s %s %s %s %s %s",
		padCell("NAME", cw[0]), padCell("ROLE", cw[1]), padCell("STATE", cw[2]),
		padCell("BINARY", cw[3]), padCell("PORT", cw[4]), padCell("CLIENTS", cw[5]),
		padCell("RX", cw[6]), padCell("TX", cw[7]), padCell("RX/s", cw[8]), padCell("TX/s", cw[9]))
	b.WriteString(headerStyle.Render(hdr))
	b.WriteString("\n")
	if len(m.instances) == 0 && len(m.discoverCands) == 0 {
		b.WriteString(dimStyle.Render("(no instances — press n to create · live openvpn auto-discovers here)"))
		b.WriteString("\n")
		return b.String()
	}
	for i, inst := range m.instances {
		state := "DOWN"
		if inst.Up {
			state = "UP"
		} else if inst.Enabled {
			state = "WAIT"
		}
		line := fmt.Sprintf("%s %s %s %s %s %s %s %s %s %s",
			padCell(inst.Name, cw[0]),
			padCell(inst.Role, cw[1]),
			padCell(state, cw[2]),
			padCell(inst.BinaryName, cw[3]),
			padCell(fmt.Sprintf("%d", inst.Port), cw[4]),
			padCell(fmt.Sprintf("%d", inst.ConnectedClients), cw[5]),
			padCell(formatBytes(inst.RxBytes), cw[6]),
			padCell(formatBytes(inst.TxBytes), cw[7]),
			padCell(formatBps(inst.RxBps), cw[8]),
			padCell(formatBps(inst.TxBps), cw[9]),
		)
		if i == m.cursor {
			b.WriteString(selStyle.Width(w).Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	// Live host processes not yet managed by openvpnd (auto-discovered each refresh).
	if len(m.discoverCands) > 0 {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render(fmt.Sprintf("▸ Host OpenVPN (live, unmanaged · %d) — enter to adopt", len(m.discoverCands))))
		b.WriteString("\n")
		cw2 := colWidths(w-2, []int{8, 12, 28, 40}, 3)
		hdr2 := fmt.Sprintf("%s %s %s %s",
			padCell("PID", cw2[0]), padCell("BINARY", cw2[1]),
			padCell("CONF", cw2[2]), padCell("CMDLINE", cw2[3]))
		b.WriteString(headerStyle.Render(hdr2))
		b.WriteString("\n")
		for i, c := range m.discoverCands {
			bin := filepath.Base(c.Binary)
			conf := c.ConfPath
			if conf == "" {
				conf = "(no conf path)"
			}
			line := fmt.Sprintf("%s %s %s %s",
				padCell(fmt.Sprintf("%d", c.PID), cw2[0]),
				padCell(bin, cw2[1]),
				padCell(conf, cw2[2]),
				padCell(c.Cmdline, cw2[3]),
			)
			row := len(m.instances) + i
			if row == m.cursor {
				b.WriteString(selStyle.Width(w).Render(line))
			} else {
				b.WriteString(warnStyle.Render(line))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// filterUnmanaged drops discover candidates already represented by managed instances
// (matched via "# adopted from <path>" breadcrumb or same basename as instance name).
func filterUnmanaged(cands []pkgapi.OpenVPNCandidate, insts []pkgapi.Instance) []pkgapi.OpenVPNCandidate {
	if len(cands) == 0 {
		return cands
	}
	adopted := map[string]struct{}{}
	names := map[string]struct{}{}
	for _, i := range insts {
		names[strings.ToLower(i.Name)] = struct{}{}
		if i.PID > 0 && i.Up {
			// live managed PID — skip same pid
			// handled below
		}
		// "# adopted from /path"
		const prefix = "# adopted from "
		for _, line := range strings.Split(i.ExtraDirectives, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, prefix) {
				adopted[strings.TrimSpace(strings.TrimPrefix(line, prefix))] = struct{}{}
			}
		}
	}
	managedPID := map[int]struct{}{}
	for _, i := range insts {
		if i.PID > 0 {
			managedPID[i.PID] = struct{}{}
		}
	}
	var out []pkgapi.OpenVPNCandidate
	for _, c := range cands {
		if _, ok := managedPID[c.PID]; ok {
			continue
		}
		if c.ConfPath != "" {
			if _, ok := adopted[c.ConfPath]; ok {
				continue
			}
			base := strings.TrimSuffix(strings.ToLower(filepath.Base(c.ConfPath)), filepath.Ext(c.ConfPath))
			if _, ok := names[base]; ok {
				// likely already imported under same name
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func (m rootModel) viewClientList(w int) string {
	cw := colWidths(w-2, []int{14, 16, 14, 14, 6, 10, 10, 10, 10}, 1)
	var b strings.Builder
	hdr := fmt.Sprintf("%s %s %s %s %s %s %s %s %s",
		padCell("INSTANCE", cw[0]), padCell("CN", cw[1]), padCell("NAME", cw[2]),
		padCell("IP", cw[3]), padCell("STATE", cw[4]),
		padCell("RX", cw[5]), padCell("TX", cw[6]), padCell("RX/s", cw[7]), padCell("TX/s", cw[8]))
	b.WriteString(headerStyle.Render(hdr))
	b.WriteString("\n")
	if len(m.clients) == 0 {
		b.WriteString(dimStyle.Render("(no clients — open a server and press n)"))
		b.WriteString("\n")
		return b.String()
	}
	for i, cl := range m.clients {
		state := "idle"
		if cl.Suspended {
			state = "SUSP"
		} else if cl.Connected {
			state = "CONN"
		}
		line := fmt.Sprintf("%s %s %s %s %s %s %s %s %s",
			padCell(cl.InstanceName, cw[0]),
			padCell(cl.CommonName, cw[1]),
			padCell(cl.Name, cw[2]),
			padCell(cl.StaticIP, cw[3]),
			padCell(state, cw[4]),
			padCell(formatBytes(cl.RxBytes), cw[5]),
			padCell(formatBytes(cl.TxBytes), cw[6]),
			padCell(formatBps(cl.RxBps), cw[7]),
			padCell(formatBps(cl.TxBps), cw[8]),
		)
		if i == m.cursor {
			b.WriteString(selStyle.Width(w).Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m rootModel) viewBinaryList(w int) string {
	cw := colWidths(w-2, []int{16, 40, 24}, 1)
	var b strings.Builder
	hdr := fmt.Sprintf("%s %s %s",
		padCell("NAME", cw[0]), padCell("PATH", cw[1]), padCell("VERSION", cw[2]))
	b.WriteString(headerStyle.Render(hdr))
	b.WriteString("\n")
	if len(m.binaries) == 0 {
		b.WriteString(dimStyle.Render("(no binaries — press n to register)"))
		b.WriteString("\n")
		return b.String()
	}
	for i, bin := range m.binaries {
		line := fmt.Sprintf("%s %s %s",
			padCell(bin.Name, cw[0]),
			padCell(bin.Path, cw[1]),
			padCell(bin.Version, cw[2]),
		)
		if i == m.cursor {
			b.WriteString(selStyle.Width(w).Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m rootModel) viewStats(w int) string {
	s := m.stats
	var body strings.Builder
	body.WriteString(titleStyle.Render("Global stats"))
	body.WriteString("\n\n")
	if m.sysStatus != "" {
		body.WriteString(fmt.Sprintf("%s %s\n\n", labelStyle.Render("System"), valueStyle.Render(m.sysStatus)))
	} else {
		body.WriteString(fmt.Sprintf("%s %s\n\n", labelStyle.Render("System"), dimStyle.Render("(unavailable)")))
	}
	kv := func(k, v string) {
		body.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render(k), valueStyle.Render(v)))
	}
	kv("Instances", fmt.Sprintf("%d total · %d up", s.InstancesTotal, s.InstancesUp))
	kv("Clients", fmt.Sprintf("%d", len(m.clients)))
	kv("Binaries", fmt.Sprintf("%d", len(m.binaries)))
	kv("RX total", formatBytes(s.RxBytes)+"  ("+formatBps(s.RxBps)+")")
	kv("TX total", formatBytes(s.TxBytes)+"  ("+formatBps(s.TxBps)+")")
	return panelStyle.Width(w).Render(body.String())
}

func (m rootModel) viewEvents(w, mainH int) string {
	cw := colWidths(w-2, []int{19, 6, 12, 14, 20}, 4)
	var b strings.Builder
	if m.sysStatus != "" {
		b.WriteString(dimStyle.Render("system · " + m.sysStatus))
		b.WriteString("\n")
	}
	hdr := fmt.Sprintf("%s %s %s %s %s",
		padCell("TIME", cw[0]), padCell("LVL", cw[1]), padCell("KIND", cw[2]),
		padCell("INSTANCE", cw[3]), padCell("MESSAGE", cw[4]))
	b.WriteString(headerStyle.Render(hdr))
	b.WriteString("\n")
	limit := mainH - 6
	if limit < 5 {
		limit = 5
	}
	if len(m.events) == 0 {
		b.WriteString(dimStyle.Render("(no events)"))
		b.WriteString("\n")
		return b.String()
	}
	for i, e := range m.events {
		if i >= limit {
			break
		}
		line := fmt.Sprintf("%s %s %s %s %s",
			padCell(e.TS.Format("2006-01-02 15:04:05"), cw[0]),
			padCell(e.Level, cw[1]),
			padCell(e.Kind, cw[2]),
			padCell(e.Instance, cw[3]),
			padCell(e.Message, cw[4]),
		)
		if i == m.cursor {
			b.WriteString(selStyle.Width(w).Render(line))
		} else if e.Level == "warn" || e.Level == "error" {
			b.WriteString(warnStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m rootModel) viewDiscover(w, h int) string {
	var body strings.Builder
	body.WriteString(titleStyle.Render("Discover running OpenVPN"))
	body.WriteString("\n")
	body.WriteString(dimStyle.Render("Pick a process (conf or PID) → adopt form. n = manual conf path."))
	body.WriteString("\n\n")
	if len(m.discoverCands) == 0 {
		body.WriteString(dimStyle.Render("(no openvpn processes found on daemon host)"))
		body.WriteString("\n")
		return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
	}
	cw := colWidths(w-2, []int{8, 28, 16, 20}, 3)
	hdr := fmt.Sprintf("%s %s %s %s",
		padCell("PID", cw[0]), padCell("CONF", cw[1]), padCell("BINARY", cw[2]), padCell("CMDLINE", cw[3]))
	body.WriteString(headerStyle.Render(hdr))
	body.WriteString("\n")
	for i, c := range m.discoverCands {
		line := fmt.Sprintf("%s %s %s %s",
			padCell(fmt.Sprintf("%d", c.PID), cw[0]),
			padCell(orDash(c.ConfPath), cw[1]),
			padCell(orDash(c.Binary), cw[2]),
			padCell(c.Cmdline, cw[3]),
		)
		if i == m.discoverCursor {
			body.WriteString(selStyle.Width(w).Render(line))
		} else {
			body.WriteString(line)
		}
		body.WriteString("\n")
	}
	return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
}

func (m rootModel) viewPrompt(w, h int) string {
	var body strings.Builder
	body.WriteString(titleStyle.Render(m.promptTitle))
	body.WriteString("\n\n")
	body.WriteString(labelStyle.Render("Value"))
	body.WriteString("  ")
	body.WriteString(m.promptInput.View())
	body.WriteString("\n\n")
	body.WriteString(dimStyle.Render("enter submit · esc cancel"))
	return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
}

func (m rootModel) viewInstDetail(w, h int) string {
	inst := m.detailInst
	if inst == nil {
		return ""
	}
	var body strings.Builder
	body.WriteString(titleStyle.Render("Instance · " + inst.Name))
	body.WriteString("\n\n")
	kv := func(k, v string) {
		body.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render(k), valueStyle.Render(v)))
	}
	up := "down"
	if inst.Up {
		up = "up"
	}
	kv("Role", inst.Role)
	kv("State", fmt.Sprintf("%s · enabled=%v · pid=%d", up, inst.Enabled, inst.PID))
	kv("Binary", strings.TrimSpace(inst.BinaryName+" "+inst.BinaryPath))
	kv("Listen", fmt.Sprintf("%s %s:%d", inst.Proto, orDash(inst.LocalBind), inst.Port))
	kv("Network", orDash(inst.ServerNetwork)+"  topology="+orDash(inst.Topology))
	kv("Public EP", orDash(inst.PublicEndpoint))
	kv("PKI CA", orDash(inst.PKICaPath))
	if inst.MaxClients > 0 {
		kv("Max clients", fmt.Sprintf("%d", inst.MaxClients))
	}
	if inst.TLSVersionMin != "" {
		kv("TLS min", inst.TLSVersionMin)
	}
	if inst.BridgeMode {
		kv("Bridge", fmt.Sprintf("%s pool %s–%s mask %s",
			orDash(inst.BridgeGateway), orDash(inst.BridgePoolStart), orDash(inst.BridgePoolEnd), orDash(inst.BridgeNetmask)))
	}
	if inst.ServerIPv6 != "" {
		kv("Server IPv6", inst.ServerIPv6)
	}
	if len(inst.FeatureSets) > 0 {
		kv("Features", strings.Join(inst.FeatureSets, ", "))
	}
	kv("Clients", fmt.Sprintf("%d connected (live)", inst.ConnectedClients))
	kv("Traffic", formatBytes(inst.RxBytes)+" / "+formatBytes(inst.TxBytes))
	if inst.LastError != "" {
		kv("Error", inst.LastError)
	}
	body.WriteString("\n")
	body.WriteString(dimStyle.Render("m status · k kill · s SIGUSR1 · r restart · e export"))
	body.WriteString("\n")
	var related []pkgapi.ServerClient
	for _, cl := range m.clients {
		if cl.InstanceName == inst.Name {
			related = append(related, cl)
		}
	}
	if len(related) > 0 {
		body.WriteString("\n")
		body.WriteString(headerStyle.Render("Clients on this instance"))
		body.WriteString("\n")
		for _, cl := range related {
			body.WriteString(fmt.Sprintf("  · %-20s %-16s %s\n", cl.CommonName, cl.StaticIP, map[bool]string{true: "SUSP", false: ""}[cl.Suspended]))
		}
	}
	return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
}

func (m rootModel) viewClientDetail(w, h int) string {
	cl := m.detailClient
	if cl == nil {
		return ""
	}
	var body strings.Builder
	body.WriteString(titleStyle.Render("Client · " + cl.CommonName))
	body.WriteString("\n\n")
	kv := func(k, v string) {
		body.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render(k), valueStyle.Render(v)))
	}
	kv("Instance", cl.InstanceName)
	kv("Name", orDash(cl.Name))
	kv("Static IP", orDash(cl.StaticIP))
	kv("Push routes", orDash(strings.Join(cl.PushRoutes, ", ")))
	kv("Iroutes", orDash(strings.Join(cl.IRoutes, ", ")))
	kv("Push DNS", orDash(strings.Join(cl.PushDNS, ", ")))
	kv("Push domain", orDash(cl.PushDomain))
	kv("Redirect GW", fmt.Sprintf("%v", cl.RedirectGateway))
	if len(cl.DisablePush) > 0 {
		kv("Disable push", strings.Join(cl.DisablePush, ", "))
	}
	if cl.BandwidthRxBps > 0 || cl.BandwidthTxBps > 0 {
		kv("Bandwidth", fmt.Sprintf("rx=%d tx=%d bps", cl.BandwidthRxBps, cl.BandwidthTxBps))
	}
	if cl.TrafficLimitBytes > 0 {
		kv("Traffic cap", formatBytes(cl.TrafficLimitBytes))
	}
	kv("Suspended", fmt.Sprintf("%v", cl.Suspended))
	kv("Connected", fmt.Sprintf("%v  %s", cl.Connected, orDash(cl.ConnectedSince)))
	kv("Real addr", orDash(cl.RealAddress))
	kv("Virt addr", orDash(cl.VirtualAddress))
	kv("Cert path", orDash(cl.ClientCertPath))
	kv("Key path", orDash(cl.ClientKeyPath))
	if cl.ClientCertPath == "" || cl.ClientKeyPath == "" {
		kv("Cert", "missing — press i to issue")
	}
	kv("Traffic", formatBytes(cl.RxBytes)+" / "+formatBytes(cl.TxBytes)+"  ("+formatBps(cl.RxBps)+" / "+formatBps(cl.TxBps)+")")
	return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
}

func (m rootModel) viewPKIList(w, mainH int) string {
	var b strings.Builder
	// section switcher
	casLbl, certsLbl := " CAs ", " Certs "
	if m.pkiSection == "certs" {
		certsLbl = tabActive.Render(certsLbl)
		casLbl = tabInactive.Render(casLbl)
	} else {
		casLbl = tabActive.Render(casLbl)
		certsLbl = tabInactive.Render(certsLbl)
	}
	b.WriteString(casLbl)
	b.WriteString(" ")
	b.WriteString(certsLbl)
	if m.pkiFilterCA != "" {
		b.WriteString("  ")
		b.WriteString(dimStyle.Render("filter CA=" + m.pkiFilterCA + " (f clear)"))
	}
	if len(m.tlsCrypts) > 0 {
		b.WriteString("  ")
		b.WriteString(dimStyle.Render(fmt.Sprintf("tls-crypt keys: %d", len(m.tlsCrypts))))
	}
	b.WriteString("\n\n")

	if m.pkiSection == "certs" {
		cw := colWidths(w-2, []int{8, 12, 10, 20, 8, 12}, 3)
		hdr := fmt.Sprintf("%s %s %s %s %s %s",
			padCell("ID", cw[0]), padCell("CA", cw[1]), padCell("KIND", cw[2]),
			padCell("CN", cw[3]), padCell("REV", cw[4]), padCell("SERIAL", cw[5]))
		b.WriteString(headerStyle.Render(hdr))
		b.WriteString("\n")
		certs := m.filteredCerts()
		if len(certs) == 0 {
			b.WriteString(dimStyle.Render("(no certificates — press i to issue)"))
			b.WriteString("\n")
			return b.String()
		}
		for i, c := range certs {
			rev := ""
			if c.Revoked {
				rev = "yes"
			}
			line := fmt.Sprintf("%s %s %s %s %s %s",
				padCell(fmt.Sprintf("%d", c.ID), cw[0]),
				padCell(c.CAName, cw[1]),
				padCell(c.Kind, cw[2]),
				padCell(c.CommonName, cw[3]),
				padCell(rev, cw[4]),
				padCell(fmt.Sprintf("%d", c.Serial), cw[5]),
			)
			if i == m.cursor {
				b.WriteString(selStyle.Width(w).Render(line))
			} else if c.Revoked {
				b.WriteString(warnStyle.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	// CAs
	cw := colWidths(w-2, []int{16, 24, 22, 20}, 1)
	hdr := fmt.Sprintf("%s %s %s %s",
		padCell("NAME", cw[0]), padCell("CN", cw[1]), padCell("NOT AFTER", cw[2]), padCell("CERT", cw[3]))
	b.WriteString(headerStyle.Render(hdr))
	b.WriteString("\n")
	if len(m.cas) == 0 {
		b.WriteString(dimStyle.Render("(no CAs — press n to create)"))
		b.WriteString("\n")
		return b.String()
	}
	for i, ca := range m.cas {
		line := fmt.Sprintf("%s %s %s %s",
			padCell(ca.Name, cw[0]),
			padCell(ca.CommonName, cw[1]),
			padCell(ca.NotAfter, cw[2]),
			padCell(ca.CertPath, cw[3]),
		)
		if i == m.cursor {
			b.WriteString(selStyle.Width(w).Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	_ = mainH
	return b.String()
}

func (m rootModel) viewPKIDetail(w, h int) string {
	var body strings.Builder
	kv := func(k, v string) {
		body.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render(k), valueStyle.Render(v)))
	}
	if m.detailCert != nil {
		c := m.detailCert
		body.WriteString(titleStyle.Render(fmt.Sprintf("Certificate · #%d %s", c.ID, c.CommonName)))
		body.WriteString("\n\n")
		kv("CA", c.CAName)
		kv("Kind", c.Kind)
		kv("CN", c.CommonName)
		kv("Serial", fmt.Sprintf("%d", c.Serial))
		kv("Fingerprint", orDash(c.Fingerprint))
		kv("Not before", orDash(c.NotBefore))
		kv("Not after", orDash(c.NotAfter))
		kv("Revoked", fmt.Sprintf("%v", c.Revoked))
		kv("Cert path", orDash(c.CertPath))
		kv("Key path", orDash(c.KeyPath))
		kv("Instance", orDash(c.InstanceName))
		kv("Notes", orDash(c.Notes))
	} else if m.detailCA != nil {
		ca := m.detailCA
		body.WriteString(titleStyle.Render("CA · " + ca.Name))
		body.WriteString("\n\n")
		kv("Name", ca.Name)
		kv("CN", ca.CommonName)
		kv("Org", orDash(ca.Org))
		kv("Not after", orDash(ca.NotAfter))
		kv("Cert path", orDash(ca.CertPath))
		kv("Key path", orDash(ca.KeyPath))
		// count certs under this CA
		n := 0
		for _, c := range m.certs {
			if c.CAName == ca.Name {
				n++
			}
		}
		kv("Certificates", fmt.Sprintf("%d", n))
	} else {
		return ""
	}
	return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
}

func (m rootModel) viewConf(w, h int) string {
	var body strings.Builder
	body.WriteString(titleStyle.Render(m.confTitle))
	body.WriteString("\n\n")
	lines := strings.Split(m.confBody, "\n")
	maxLines := h - 8
	if maxLines < 5 {
		maxLines = 5
	}
	start := m.scroll
	if start > len(lines) {
		start = max(0, len(lines)-1)
	}
	end := min(len(lines), start+maxLines)
	for _, line := range lines[start:end] {
		body.WriteString(dimStyle.Render(trunc(line, max(20, w-6))))
		body.WriteString("\n")
	}
	if m.confQR != "" {
		body.WriteString("\n")
		body.WriteString(m.confQR)
	}
	return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
}

func (m rootModel) viewProfileLink(w, h int) string {
	link := m.profileLink
	if link == nil {
		return ""
	}
	var body strings.Builder
	body.WriteString(titleStyle.Render("Profile link · " + link.CommonName))
	body.WriteString("\n\n")
	kv := func(k, v string) {
		body.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render(k), valueStyle.Render(v)))
	}
	kv("Download", link.DownloadURL)
	kv("Import", link.ImportURL)
	kv("Expires", link.ExpiresAt.Format("2006-01-02 15:04:05"))
	kv("Max uses", fmt.Sprintf("%d (used %d)", link.MaxUses, link.UseCount))
	if m.confQR != "" {
		body.WriteString("\n")
		body.WriteString(headerStyle.Render("QR (OpenVPN Connect import URL)"))
		body.WriteString("\n")
		body.WriteString(m.confQR)
	}
	return panelStyle.Width(w).Height(h).MaxHeight(h).Render(body.String())
}

func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	// rune-safe truncate by visible width
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := 1
		if r > 127 {
			rw = lipgloss.Width(string(r))
			if rw < 1 {
				rw = 1
			}
		}
		if w+rw >= n {
			b.WriteRune('…')
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
