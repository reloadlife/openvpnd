package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	fieldText   = "text"
	fieldSelect = "select"
	fieldBool   = "bool"
	fieldFile   = "file"
)

// fieldDef describes one form input. Roles filters visibility:
// empty = both server and client; otherwise only listed roles.
type fieldDef struct {
	Key          string
	Label        string
	Hint         string // short placeholder / example
	Tip          string // longer “what is this?” shown when focused
	Section      string // section header when this field starts a group
	Width        int
	Kind         string
	Options      []string
	Roles        []string // "server", "client"
	AllowedTypes []string // for fieldFile: e.g. .ovpn, .crt
}

type formModel struct {
	title   string
	defs    []fieldDef // full catalog
	fields  []fieldDef // currently visible
	inputs  []textinput.Model
	selIdx  []int
	focus   int
	err     string
	width   int
	height  int
	help    string
	note    string
	roleKey string // usually "role"
	// stash keeps values for fields not currently visible (role switch / import).
	stash map[string]string
}

func newForm(title string, defs []fieldDef, values map[string]string) formModel {
	if values == nil {
		values = map[string]string{}
	}
	role := values["role"]
	if role == "" {
		role = "server"
	}
	f := formModel{title: title, defs: defs, roleKey: "role", stash: map[string]string{}}
	for k, v := range values {
		f.stash[k] = v
	}
	f.rebuild(role, values)
	return f
}

func fieldVisible(f fieldDef, role string) bool {
	if len(f.Roles) == 0 {
		return true
	}
	role = strings.ToLower(strings.TrimSpace(role))
	for _, r := range f.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

func (f *formModel) rebuild(role string, values map[string]string) {
	if f.stash == nil {
		f.stash = map[string]string{}
	}
	if values == nil {
		values = map[string]string{}
	}
	for k, v := range values {
		f.stash[k] = v
	}
	f.stash["role"] = role
	var fields []fieldDef
	for _, d := range f.defs {
		kind := d.Kind
		if kind == "" {
			kind = fieldText
		}
		d.Kind = kind
		if fieldVisible(d, role) {
			fields = append(fields, d)
		}
	}
	inputs := make([]textinput.Model, len(fields))
	selIdx := make([]int, len(fields))
	for i, field := range fields {
		ti := textinput.New()
		ti.Placeholder = field.Hint
		w := field.Width
		if w <= 0 {
			w = 56
		}
		ti.CharLimit = 2048
		ti.Width = w
		ti.Prompt = ""
		v := f.stash[field.Key]
		switch field.Kind {
		case fieldSelect:
			selIdx[i] = indexOf(field.Options, v)
			if selIdx[i] < 0 && len(field.Options) > 0 {
				if field.Key == f.roleKey {
					selIdx[i] = indexOf(field.Options, role)
				}
				if selIdx[i] < 0 {
					selIdx[i] = 0
				}
			}
		case fieldBool:
			if truthy(v) {
				selIdx[i] = 1
			}
		default:
			ti.SetValue(v)
		}
		inputs[i] = ti
	}
	f.fields = fields
	f.inputs = inputs
	f.selIdx = selIdx
	if f.focus >= len(f.fields) {
		f.focus = 0
	}
	_ = f.focusInput()
}

func indexOf(opts []string, v string) int {
	v = strings.TrimSpace(v)
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return -1
}

func (f formModel) Init() tea.Cmd { return textinput.Blink }

func (f formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			if len(f.fields) == 0 {
				return f, nil
			}
			f.focus = (f.focus + 1) % len(f.fields)
			return f, f.focusInput()
		case "shift+tab", "up":
			if len(f.fields) == 0 {
				return f, nil
			}
			f.focus = (f.focus + len(f.fields) - 1) % len(f.fields)
			return f, f.focusInput()
		case "left", "h":
			if f.fields[f.focus].Kind == fieldSelect || f.fields[f.focus].Kind == fieldBool {
				changedRole := f.fields[f.focus].Key == f.roleKey
				f.cycleSelect(f.focus, -1)
				if changedRole {
					f.onRoleChanged()
				}
				return f, f.focusInput()
			}
		case "right", "l":
			if f.fields[f.focus].Kind == fieldSelect || f.fields[f.focus].Kind == fieldBool {
				changedRole := f.fields[f.focus].Key == f.roleKey
				f.cycleSelect(f.focus, +1)
				if changedRole {
					f.onRoleChanged()
				}
				return f, f.focusInput()
			}
		case " ":
			// space toggles bool/select; for file fields handled by root (browse)
			if f.fields[f.focus].Kind == fieldSelect || f.fields[f.focus].Kind == fieldBool {
				changedRole := f.fields[f.focus].Key == f.roleKey
				f.cycleSelect(f.focus, +1)
				if changedRole {
					f.onRoleChanged()
				}
				return f, f.focusInput()
			}
		}
	}
	kind := ""
	if len(f.fields) > 0 {
		kind = f.fields[f.focus].Kind
	}
	if kind == fieldText || kind == fieldFile {
		var cmd tea.Cmd
		f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
		return f, cmd
	}
	return f, nil
}

// onRoleChanged rebuilds visible fields from current values + new role.
func (f *formModel) onRoleChanged() {
	vals := f.Values()
	role := vals[f.roleKey]
	if role == "" {
		role = "server"
	}
	// Keep focus on role field after rebuild if still present.
	f.rebuild(role, vals)
	for i, field := range f.fields {
		if field.Key == f.roleKey {
			f.focus = i
			break
		}
	}
	// Role-specific note defaults
	if role == "client" {
		if f.note == "" || strings.Contains(f.note, "mTLS server") || strings.Contains(f.note, "server") {
			f.note = "Client: set remote(s) or browse a .ovpn profile (space / ctrl+o on Profile)."
		}
	} else if strings.Contains(f.note, "Client:") {
		f.note = "Server: leave name/port/network empty for auto. issue_cert+create_ca → full mTLS."
	}
}

func (f *formModel) cycleSelect(i, delta int) {
	opts := f.fields[i].Options
	if f.fields[i].Kind == fieldBool {
		opts = []string{"n", "y"}
	}
	if len(opts) == 0 {
		return
	}
	f.selIdx[i] = (f.selIdx[i] + delta + len(opts)) % len(opts)
}

func (f *formModel) focusInput() tea.Cmd {
	for i := range f.inputs {
		kind := f.fields[i].Kind
		if i == f.focus && (kind == fieldText || kind == fieldFile) {
			f.inputs[i].Focus()
		} else {
			f.inputs[i].Blur()
		}
	}
	return textinput.Blink
}

func (f formModel) Values() map[string]string {
	out := make(map[string]string, len(f.stash)+len(f.fields)+2)
	for k, v := range f.stash {
		out[k] = v
	}
	for i, field := range f.fields {
		switch field.Kind {
		case fieldSelect:
			if len(field.Options) > 0 {
				idx := f.selIdx[i]
				if idx < 0 || idx >= len(field.Options) {
					idx = 0
				}
				out[field.Key] = field.Options[idx]
			}
		case fieldBool:
			if f.selIdx[i] == 1 {
				out[field.Key] = "y"
			} else {
				out[field.Key] = "n"
			}
		default:
			out[field.Key] = strings.TrimSpace(f.inputs[i].Value())
		}
	}
	return out
}

func (f formModel) Get(key string) string { return f.Values()[key] }

func (f formModel) Focused() fieldDef {
	if len(f.fields) == 0 || f.focus < 0 || f.focus >= len(f.fields) {
		return fieldDef{}
	}
	return f.fields[f.focus]
}

func (f *formModel) SetValue(key, value string) {
	if f.stash == nil {
		f.stash = map[string]string{}
	}
	f.stash[key] = value
	for i, field := range f.fields {
		if field.Key != key {
			continue
		}
		switch field.Kind {
		case fieldSelect:
			f.selIdx[i] = indexOf(field.Options, value)
			if f.selIdx[i] < 0 {
				f.selIdx[i] = 0
			}
		case fieldBool:
			if truthy(value) {
				f.selIdx[i] = 1
			} else {
				f.selIdx[i] = 0
			}
		default:
			f.inputs[i].SetValue(value)
		}
		return
	}
}

// ApplyValues merges a map into the form (visible fields updated; others kept for role switch).
func (f *formModel) ApplyValues(patch map[string]string) {
	vals := f.Values()
	for k, v := range patch {
		if v != "" {
			vals[k] = v
		}
	}
	role := vals[f.roleKey]
	if role == "" {
		role = "server"
	}
	focusKey := ""
	if len(f.fields) > 0 && f.focus >= 0 && f.focus < len(f.fields) {
		focusKey = f.fields[f.focus].Key
	}
	f.rebuild(role, vals)
	if focusKey != "" {
		for i, field := range f.fields {
			if field.Key == focusKey {
				f.focus = i
				break
			}
		}
	}
	_ = f.focusInput()
}

func (f *formModel) SetSize(w, h int) {
	f.width = w
	f.height = h
	iw := w - 26
	if iw < 24 {
		iw = 24
	}
	if iw > 100 {
		iw = 100
	}
	for i := range f.inputs {
		f.inputs[i].Width = iw
	}
}

func (f formModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(f.title))
	role := f.Get(f.roleKey)
	if role != "" {
		b.WriteString("  ")
		if role == "client" {
			b.WriteString(badgeCli.Render("CLIENT"))
		} else {
			b.WriteString(badgeSrv.Render("SERVER"))
		}
	}
	b.WriteString("\n\n")
	if f.err != "" {
		b.WriteString(errStyle.Render("✗  " + f.err))
		b.WriteString("\n\n")
	}
	if f.note != "" {
		b.WriteString(dimStyle.Render(truncRunes(f.note, max(20, f.width-6))))
		b.WriteString("\n")
	}

	lastSection := ""
	for i, field := range f.fields {
		if field.Section != "" && field.Section != lastSection {
			lastSection = field.Section
			b.WriteString(sectionStyle.Render("▸ " + field.Section))
			b.WriteString("\n")
		}
		focused := i == f.focus
		var label string
		if focused {
			label = focusStyle.Render(fmt.Sprintf(" %-16s ", field.Label))
		} else {
			label = labelStyle.Width(18).Render(" " + field.Label)
		}
		var val string
		switch field.Kind {
		case fieldSelect:
			opts := field.Options
			if len(opts) == 0 {
				val = dimStyle.Render("(none)")
			} else {
				idx := f.selIdx[i]
				if idx < 0 || idx >= len(opts) {
					idx = 0
				}
				cur := opts[idx]
				if focused {
					val = selStyle.Render(" ◀ " + cur + " ▶ ")
				} else {
					val = valueStyle.Render(cur)
				}
			}
		case fieldBool:
			on := f.selIdx[i] == 1
			if focused {
				if on {
					val = okStyle.Render(" [ ON  ] ")
				} else {
					val = dimStyle.Render(" [ off ] ")
				}
			} else if on {
				val = okStyle.Render("on")
			} else {
				val = dimStyle.Render("off")
			}
		case fieldFile:
			raw := f.inputs[i].View()
			if focused {
				val = raw + dimStyle.Render("  [browse]")
			} else {
				val = raw
			}
		default:
			val = f.inputs[i].View()
		}
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(val)
		b.WriteString("\n")
		// One short line under the focused field only — no big tip panel.
		if focused {
			if line := compactTip(field, max(24, f.width-8)); line != "" {
				b.WriteString(dimStyle.Render("  " + line))
				b.WriteString("\n")
			}
		}
	}
	help := f.help
	if help == "" {
		help = "tab/↑↓  ·  ←/→  ·  space browse  ·  enter save  ·  esc"
	}
	b.WriteString(helpStyle.Render(help))
	inner := b.String()
	w := f.width
	if w < 40 {
		w = 80
	}
	h := f.height
	if h < 1 {
		h = 10
	}
	box := panelStyle.Width(w).Height(h).MaxHeight(h)
	return box.Render(inner)
}

// compactTip is a single truncated line for the focused field (hint preferred).
func compactTip(field fieldDef, maxW int) string {
	s := strings.TrimSpace(field.Hint)
	if s == "" {
		s = strings.TrimSpace(field.Tip)
	}
	if s == "" {
		return ""
	}
	// Prefer first sentence of tip if using Tip as fallback
	if field.Hint == "" {
		if i := strings.IndexAny(s, ".!?"); i > 12 && i < 90 {
			s = s[:i+1]
		}
	}
	return truncRunes(s, maxW)
}

func truncRunes(s string, maxW int) string {
	if maxW < 4 {
		maxW = 4
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	// crude byte trim is ok for ASCII tips; keep simple
	for lipgloss.Width(s) > maxW-1 && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s + "…"
}

func instanceCreateFields(binaries []string) []fieldDef {
	bins := append([]string{}, binaries...)
	if len(bins) == 0 {
		bins = []string{"default"}
	}
	return []fieldDef{
		// ── Basics ──
		{
			Key: "name", Label: "Name", Section: "Basics",
			Hint: "empty → ovpn0, ovpn1…",
			Tip:  "Short unique id for this instance (letters, numbers, _ -). Leave empty and openvpnd assigns ovpn0, ovpn1, … automatically.",
		},
		{
			Key: "role", Label: "Role", Kind: fieldSelect, Options: []string{"server", "client"},
			Hint: "←/→ switches the whole form",
			Tip:  "server = accept VPN peers (listen + tunnel pool). client = this host dials out to a remote OpenVPN server. Changing role swaps which fields you see.",
		},
		{
			Key: "binary", Label: "Binary", Kind: fieldSelect, Options: bins,
			Hint: "registered openvpn builds",
			Tip:  "Which OpenVPN executable runs this instance. Use the default system build, or a custom/forked binary (e.g. UDP stuffing) registered under Binaries.",
		},
		{
			Key: "proto", Label: "Proto", Kind: fieldSelect, Options: []string{"udp", "tcp", "udp4", "tcp4", "udp6", "tcp6"},
			Hint: "udp is usual",
			Tip:  "Transport protocol for the VPN tunnel. UDP is faster/lower latency; TCP can help on hostile networks. Must match the peer (server and clients).",
		},
		{
			Key: "dev_type", Label: "Dev type", Kind: fieldSelect, Options: []string{"tun", "tap"},
			Hint: "tun = layer-3 (usual)",
			Tip:  "tun routes IP packets (normal VPN). tap bridges Ethernet frames (rarer; needed for some LAN-style use). Prefer tun unless you know you need bridge mode.",
		},
		{
			Key: "device", Label: "Device",
			Hint: "optional e.g. tun0",
			Tip:  "Optional fixed interface name (tun0, ovpns0…). Leave empty and OpenVPN picks one. Set only if you need a stable name for firewall rules.",
		},
		{
			Key: "auth_mode", Label: "Auth mode", Kind: fieldSelect, Options: []string{"pki", "static_key"},
			Hint: "pki = certs (recommended)",
			Tip:  "pki = modern TLS with CA/cert/key (recommended). static_key = shared secret only (simple site-to-site; weaker operational story).",
		},

		// ── Server listen / pool ──
		{
			Key: "port", Label: "Listen port", Section: "Server listen & pool", Roles: []string{"server"},
			Hint: "empty → next free from 1194",
			Tip:  "UDP/TCP port OpenVPN listens on. Leave empty to auto-pick the next free port starting at 1194. Clients must reach Public EP on this port (or your NAT mapping).",
		},
		{
			Key: "local_bind", Label: "Local bind", Roles: []string{"server"},
			Hint: "optional host IP",
			Tip:  "Optional local address to bind (multi-homed hosts). Empty = listen on all interfaces. Use a specific IP if only one NIC should accept VPN traffic.",
		},
		{
			Key: "network", Label: "Server net", Roles: []string{"server"},
			Hint: "empty → free 10.x.0.0/24",
			Tip:  "Tunnel IPv4 pool in CIDR form (e.g. 10.8.0.0/24). Server takes .1; clients get addresses from the pool. Leave empty for an auto free 10.x.0.0/24 that does not overlap other instances.",
		},
		{
			Key: "topology", Label: "Topology", Kind: fieldSelect, Options: []string{"subnet", "net30", "p2p"}, Roles: []string{"server"},
			Hint: "subnet is modern default",
			Tip:  "How tunnel IPs are assigned. subnet = one address per client (recommended). net30 = old point-to-point /30 pairs. p2p = point-to-point without full server mode.",
		},
		{
			Key: "public_endpoint", Label: "Public EP", Roles: []string{"server"},
			Hint: "vpn.example.com:1194",
			Tip:  "Hostname or host:port clients use to connect — written into downloadable .ovpn profiles. Use your public DNS or IP (and real port if different from listen via NAT).",
		},

		// ── Server push ──
		{
			Key: "push_dns", Label: "Push DNS", Section: "Push to clients", Roles: []string{"server"},
			Hint: "1.1.1.1,8.8.8.8",
			Tip:  "DNS resolvers pushed to connected clients (CSV of IPs). They use these while the VPN is up. Empty = do not push DNS.",
		},
		{
			Key: "push_routes", Label: "Push routes", Roles: []string{"server"},
			Hint: "10.0.0.0/8,192.168.0.0/16",
			Tip:  "Extra LAN/CIDR routes pushed so clients can reach internal nets through the tunnel. Full-tunnel is Redirect GW instead (or in addition).",
		},
		{
			Key: "push_domain", Label: "Push domain", Roles: []string{"server"},
			Hint: "internal.lan",
			Tip:  "Search domain pushed to clients (DHCP option style). Helps resolve short names like “fileserver” inside your network.",
		},
		{
			Key: "redirect_gw", Label: "Redirect GW", Kind: fieldBool, Roles: []string{"server"},
			Hint: "full-tunnel all traffic",
			Tip:  "When ON, clients send all internet traffic through the VPN (full tunnel). When OFF, only Server net + Push routes go via VPN (split tunnel).",
		},

		// ── Server PKI auto ──
		{
			Key: "issue_cert", Label: "Issue cert", Kind: fieldBool, Section: "PKI / certificates (server)", Roles: []string{"server"},
			Hint: "auto server cert from CA",
			Tip:  "ON = mint a server certificate (and wire paths) from a managed CA after create. Leave ON for a zero-touch mTLS server. OFF if you already have cert files to paste below.",
		},
		{
			Key: "create_ca", Label: "Create CA", Kind: fieldBool, Roles: []string{"server"},
			Hint: "mint CA if none exists",
			Tip:  "If no Certificate Authority exists yet, create a default CA so Issue cert can work. Safe for first-time setups; turn OFF if you already manage CAs.",
		},
		{
			Key: "ca_name", Label: "CA name", Roles: []string{"server"},
			Hint: "default = first CA",
			Tip:  "Which managed CA to use when issuing the server cert. Empty = first available CA (or the one Create CA makes).",
		},
		{
			Key: "server_cn", Label: "Server CN", Roles: []string{"server"},
			Hint: "defaults from Public EP / name",
			Tip:  "Common Name on the server certificate. Empty = derived from Public EP host or instance name. Should match how clients address the server when possible.",
		},
		{
			Key: "tls_crypt", Label: "TLS-crypt", Kind: fieldBool, Roles: []string{"server"},
			Hint: "generate with issue",
			Tip:  "ON = also generate a tls-crypt key (control-channel wrap; hides TLS handshake metadata). Recommended with Issue cert. Clients need the same key in their profile.",
		},
		{
			Key: "data_ciphers", Label: "Data ciphers", Roles: []string{"server"},
			Hint: "empty → AES-256-GCM:…",
			Tip:  "Allowed data-channel ciphers (OpenVPN 2.5+ list). Empty = modern GCM/ChaCha set. Only change if peers require a specific suite.",
		},
		{
			Key: "auth", Label: "Auth digest", Roles: []string{"server"},
			Hint: "empty → SHA256",
			Tip:  "HMAC digest for the data channel (legacy/control use). Empty defaults to SHA256. Keep matching on clients if you set it.",
		},
		{
			Key: "cipher", Label: "Cipher", Roles: []string{"server"},
			Hint: "legacy single cipher",
			Tip:  "Old-style single cipher directive. Prefer Data ciphers on modern OpenVPN. Use only for compatibility with very old clients.",
		},
		{
			Key: "pki_ca", Label: "CA path", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem", ".cer"}, Roles: []string{"server"},
			Hint: "optional manual ca.crt",
			Tip:  "Absolute path to CA certificate if you are NOT using Issue cert. Leave empty when auto-issue is ON — openvpnd fills this for you.",
		},
		{
			Key: "pki_cert", Label: "Cert path", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem"}, Roles: []string{"server"},
			Hint: "optional server.crt",
			Tip:  "Absolute path to the server certificate file (manual PKI). Leave empty with Issue cert ON.",
		},
		{
			Key: "pki_key", Label: "Key path", Kind: fieldFile, AllowedTypes: []string{".key", ".pem"}, Roles: []string{"server"},
			Hint: "optional server.key",
			Tip:  "Absolute path to the server private key (manual PKI). Leave empty with Issue cert ON. Must be readable by the openvpnd host.",
		},

		// ── Server advanced ──
		{
			Key: "max_clients", Label: "Max clients", Section: "Advanced (server)", Roles: []string{"server"},
			Hint: "empty → OpenVPN default",
			Tip:  "Maximum simultaneous VPN peers (OpenVPN --max-clients). Empty = daemon/OpenVPN default.",
		},
		{
			Key: "tls_version_min", Label: "TLS min", Roles: []string{"server"},
			Hint: "1.2 or 1.3",
			Tip:  "Minimum TLS version for the control channel (tls-version-min). Prefer 1.2+.",
		},
		{
			Key: "tls_groups", Label: "TLS groups", Roles: []string{"server"},
			Hint: "X25519:secp256r1",
			Tip:  "ECDH groups for TLS (tls-groups). Empty = OpenVPN defaults.",
		},
		{
			Key: "tls_cipher", Label: "TLS cipher", Roles: []string{"server"},
			Hint: "TLS 1.2 cipher list",
			Tip:  "tls-cipher for TLS ≤1.2 control channel. Prefer Feature tls_modern for a sane preset.",
		},
		{
			Key: "tls_ciphersuites", Label: "TLS suites", Roles: []string{"server"},
			Hint: "TLS 1.3 ciphersuites",
			Tip:  "tls-ciphersuites for TLS 1.3. Empty = OpenVPN defaults.",
		},
		{
			Key: "tls_cert_profile", Label: "TLS profile", Roles: []string{"server"},
			Hint: "preferred, suiteb-128,…",
			Tip:  "tls-cert-profile constrains peer certificates (preferred/legacy/suiteb-*).",
		},
		{
			Key: "tun_mtu", Label: "TUN MTU", Roles: []string{"server"},
			Hint: "e.g. 1500",
			Tip:  "Tunnel interface MTU (tun-mtu). Leave empty unless you tune path MTU.",
		},
		{
			Key: "server_ipv6", Label: "Server IPv6", Roles: []string{"server"},
			Hint: "fd00::/64",
			Tip:  "IPv6 server network (server-ipv6). Enables dual-stack pool when set.",
		},
		{
			Key: "ifconfig_ipv6", Label: "Ifconfig v6", Roles: []string{"server"},
			Hint: "local/prefix [remote]",
			Tip:  "Point-to-point IPv6 ifconfig-ipv6 when not using server-ipv6 pool mode.",
		},
		{
			Key: "bridge_mode", Label: "Bridge mode", Kind: fieldBool, Roles: []string{"server"},
			Hint: "tap + server-bridge",
			Tip:  "ON = Ethernet bridge pool (usually with Dev type tap). Set gateway and pool below.",
		},
		{
			Key: "bridge_gateway", Label: "Br gateway", Roles: []string{"server"},
			Hint: "192.168.1.1",
			Tip:  "LAN gateway IP for server-bridge (bridge_mode).",
		},
		{
			Key: "bridge_pool_start", Label: "Br pool start", Roles: []string{"server"},
			Hint: "192.168.1.100",
			Tip:  "First DHCP-style address handed to bridge clients.",
		},
		{
			Key: "bridge_pool_end", Label: "Br pool end", Roles: []string{"server"},
			Hint: "192.168.1.200",
			Tip:  "Last address in the bridge client pool.",
		},
		{
			Key: "bridge_netmask", Label: "Br netmask", Roles: []string{"server"},
			Hint: "255.255.255.0",
			Tip:  "Netmask for server-bridge (dotted quad).",
		},
		{
			Key: "auth_user_pass_verify", Label: "Auth script", Roles: []string{"server"},
			Hint: "/path/to/verify via-env",
			Tip:  "auth-user-pass-verify script path + method (via-env / via-file). Pair with script_security ≥2.",
		},
		{
			Key: "script_security", Label: "Script sec", Roles: []string{"server"},
			Hint: "0–3 (2 common with scripts)",
			Tip:  "script-security level (0–3). Raise only when using external auth/up scripts.",
		},
		{
			Key: "username_as_cn", Label: "User as CN", Kind: fieldBool, Roles: []string{"server"},
			Hint: "username-as-common-name",
			Tip:  "ON = treat auth username as CN for CCD/auth (username-as-common-name).",
		},

		// ── Client connect ──
		{
			Key: "profile", Label: "Profile", Section: "Connect (client)", Kind: fieldFile, AllowedTypes: []string{".ovpn", ".conf"}, Roles: []string{"client"},
			Hint: "browse .ovpn / .conf",
			Tip:  "Import an existing OpenVPN client profile. We parse remotes, proto, and cert material (including inline <ca>/<cert>/<key>). Easiest path: browse a .ovpn, review auto-filled fields, then save.",
		},
		{
			Key: "remote", Label: "Remote(s)", Roles: []string{"client"},
			Hint: "vpn.example.com:1194 or host:port:udp",
			Tip:  "Where this client connects — required. CSV of host:port or host:port:proto. Filled automatically from Profile if present. Example: vpn.example.com:1194,backup:1194:udp",
		},
		{
			Key: "auth_user_pass", Label: "User/pass", Kind: fieldBool, Roles: []string{"client"},
			Hint: "prompt or file for username/password",
			Tip:  "ON = enable auth-user-pass (username/password in addition to certs if required by server).",
		},
		{
			Key: "auth_user_pass_file", Label: "Pass file", Kind: fieldFile, AllowedTypes: []string{".txt", ".pass", ".auth"}, Roles: []string{"client"},
			Hint: "optional 2-line user/pass file",
			Tip:  "Path to a 2-line credentials file for auth-user-pass. Leave empty to prompt at start (when supported).",
		},
		{
			Key: "ifconfig_ipv6", Label: "Ifconfig v6", Roles: []string{"client"},
			Hint: "local/prefix [remote]",
			Tip:  "Client ifconfig-ipv6 when the server expects a fixed IPv6 endpoint.",
		},
		{
			Key: "pki_ca", Label: "CA path", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem", ".cer"}, Roles: []string{"client"},
			Hint: "ca.crt",
			Tip:  "Path to the CA that signed the server (and usually the client) certificate. Required for mTLS unless using static key. Auto-filled from Profile when possible.",
		},
		{
			Key: "pki_cert", Label: "Cert path", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem"}, Roles: []string{"client"},
			Hint: "client.crt",
			Tip:  "This machine’s client certificate file. Needed for PKI auth. Import from Profile or browse to the .crt on the openvpnd host.",
		},
		{
			Key: "pki_key", Label: "Key path", Kind: fieldFile, AllowedTypes: []string{".key", ".pem"}, Roles: []string{"client"},
			Hint: "client.key",
			Tip:  "Private key matching Cert path. Keep permissions tight (0600). Extracted from inline .ovpn when you import a Profile.",
		},
		{
			Key: "tls_crypt_path", Label: "TLS-crypt", Kind: fieldFile, AllowedTypes: []string{".key", ".pem", ".txt"}, Roles: []string{"client"},
			Hint: "optional shared tls-crypt key",
			Tip:  "Optional tls-crypt key file — must match the server if the server uses tls-crypt. Often embedded in provider .ovpn files and extracted on import.",
		},
		{
			Key: "static_key", Label: "Static key", Kind: fieldFile, AllowedTypes: []string{".key"}, Roles: []string{"client"},
			Hint: "only if auth_mode=static_key",
			Tip:  "Shared secret for static_key mode (no PKI). Leave empty for normal certificate auth.",
		},
		{
			Key: "data_ciphers", Label: "Data ciphers", Roles: []string{"client"},
			Hint: "optional; from profile",
			Tip:  "Client data-ciphers list if the server requires a specific set. Usually left empty or taken from the imported Profile.",
		},
		{
			Key: "auth", Label: "Auth digest", Roles: []string{"client"},
			Hint: "optional; from profile",
			Tip:  "HMAC digest if required by the peer. Prefer matching the server; empty is fine for modern defaults.",
		},
		{
			Key: "cipher", Label: "Cipher", Roles: []string{"client"},
			Hint: "optional legacy",
			Tip:  "Legacy single cipher for old peers. Prefer Data ciphers / profile defaults on modern OpenVPN.",
		},
		{
			Key: "features", Label: "Features", Section: "Extensions", Roles: []string{"client"},
			Hint: "explicit_exit_notify,mssfix",
			Tip:  "Named feature presets (CSV) expanded into conf/plugins/env. For clients, explicit_exit_notify is a good UDP default. See GET /v1/features for the full list.",
		},

		// ── Extensions ──
		{
			Key: "features", Label: "Features", Section: "Extensions", Roles: []string{"server"},
			Hint: "udp_stuffing_env,auth_script_template,tls_modern,…",
			Tip:  "Named feature presets (CSV): directives/plugins/env (udp_stuffing, udp_stuffing_env, auth_script_template, tls_modern, mssfix…). List with GET /v1/features.",
		},
		{
			Key: "plugin", Label: "Plugin", Kind: fieldFile, Section: "Extensions",
			Hint: "/opt/foo/plugin.so arg=1",
			Tip:  "OpenVPN --plugin module path (optional args after the path). Use for auth scripts, custom stuffing .so modules, etc. Binary must support the plugin ABI.",
		},
		{
			Key: "extra", Label: "Extra conf",
			Hint: "raw openvpn lines",
			Tip:  "Escape hatch: raw OpenVPN config lines appended to the generated conf (one directive per line). For fork-specific options not yet first-class fields. Use carefully — bad lines can prevent start.",
		},
	}
}

func adoptInstanceFields() []fieldDef {
	return []fieldDef{
		{
			Key: "conf_path", Label: "Conf path", Section: "Adopt existing OpenVPN", Kind: fieldFile, AllowedTypes: []string{".conf", ".ovpn"},
			Hint: "absolute path on daemon host",
			Tip:  "On-disk OpenVPN conf the daemon will read (must be absolute and readable by openvpnd).",
		},
		{
			Key: "name", Label: "Name",
			Hint: "empty → from conf / auto",
			Tip:  "Instance name after adopt. Empty lets the daemon choose from conf basename or auto.",
		},
		{
			Key: "public_endpoint", Label: "Public EP",
			Hint: "vpn.example.com:1194",
			Tip:  "Optional host:port written into future client profiles for this server.",
		},
		{
			Key: "take_over", Label: "Take over", Kind: fieldBool,
			Hint: "document intent to manage process",
			Tip:  "ON notes that you intend to stop the foreign process and run under openvpnd (v1 does not force-kill).",
		},
		{
			Key: "pid", Label: "PID",
			Hint: "optional from discover",
			Tip:  "Optional process id from discover (stored in notes only).",
		},
	}
}

func clientCreateFields(servers []string) []fieldDef {
	opts := append([]string{}, servers...)
	if len(opts) == 0 {
		opts = []string{"(no servers)"}
	}
	return []fieldDef{
		{
			Key: "instance", Label: "Server", Kind: fieldSelect, Options: opts, Section: "Who / where",
			Hint: "server instance",
			Tip:  "Server instance this user connects to. Tunnel IP comes from that server’s pool; cert is issued under the server’s CA when possible.",
		},
		{
			Key: "cn", Label: "Username (CN)",
			Hint: "alice, phone, laptop1",
			Tip:  "Unique login identity (certificate Common Name). Required. Used in CCD, suspend, and .ovpn filename. Letters, digits, . _ - @.",
		},
		{
			Key: "name", Label: "Display name",
			Hint: "empty → same as CN",
			Tip:  "Friendly label in lists (e.g. “Alice phone”). Optional — defaults to the CN.",
		},
		{
			Key: "static_ip", Label: "Static IP",
			Hint: "empty → next free from pool",
			Tip:  "Fixed tunnel address inside the server network. Leave empty to auto-pick the next free host (recommended).",
		},
		{
			Key: "iroutes", Label: "Iroutes",
			Hint: "192.168.1.0/24,10.20.0.0/16",
			Tip:  "Subnets behind this client (site-to-site). Emitted as CCD iroute so the server routes those nets via the client. CSV of CIDRs; optional.",
		},
		{
			Key: "push_dns", Label: "Push DNS", Section: "Per-client push / limits",
			Hint: "1.1.1.1,8.8.8.8",
			Tip:  "Per-client DNS push (overrides/adds to server push). CSV of IPs; empty = server defaults only.",
		},
		{
			Key: "push_domain", Label: "Push domain",
			Hint: "internal.lan",
			Tip:  "Per-client search domain pushed via CCD.",
		},
		{
			Key: "redirect_gw", Label: "Redirect GW", Kind: fieldBool,
			Hint: "full-tunnel for this user",
			Tip:  "ON = push redirect-gateway for this client only (full tunnel).",
		},
		{
			Key: "disable_push", Label: "Disable push",
			Hint: "redirect-gateway,route-ipv6,…",
			Tip:  "CSV of push options to strip for this client (push-remove style disable list).",
		},
		{
			Key: "bandwidth_rx", Label: "BW RX bps",
			Hint: "bytes/sec receive cap",
			Tip:  "Soft receive bandwidth limit in bytes/sec (0/empty = unlimited).",
		},
		{
			Key: "bandwidth_tx", Label: "BW TX bps",
			Hint: "bytes/sec transmit cap",
			Tip:  "Soft transmit bandwidth limit in bytes/sec (0/empty = unlimited).",
		},
		{
			Key: "traffic_limit", Label: "Traffic cap",
			Hint: "total bytes before suspend",
			Tip:  "Lifetime limit in bytes; daemon may suspend when exceeded. 0/empty = none.",
		},
		{
			Key: "issue_cert", Label: "Issue cert", Kind: fieldBool, Section: "One-shot provisioning",
			Hint: "mint mTLS client cert",
			Tip:  "ON (default) = create a client certificate under the managed CA and wire paths. Turn OFF only if you paste existing cert/key paths below.",
		},
		{
			Key: "mint_link", Label: "Profile link", Kind: fieldBool,
			Hint: "one-click install URL + QR",
			Tip:  "ON (default) = after create, mint a time-limited download URL and openvpn://import-profile/ deep link (shown with QR). Needs server public_endpoint + certs.",
		},
		{
			Key: "link_ttl", Label: "Link TTL",
			Hint: "24h, 7d, 15m…",
			Tip:  "How long the install link stays valid (Go duration). Default 24h. Shorter is safer for shared links.",
		},
		{
			Key: "link_uses", Label: "Link max uses",
			Hint: "1 = single download",
			Tip:  "How many times the link can be downloaded. 1 = single use (recommended for sharing). 0 = unlimited until expiry.",
		},
		{
			Key: "notes", Label: "Notes",
			Hint: "optional operator note",
			Tip:  "Optional note stored on the client record (who this is for, ticket id, etc.). Not sent to the VPN peer.",
		},
		{
			Key: "cert_path", Label: "Cert path", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem"}, Section: "Manual PKI (optional)",
			Hint: "only if Issue cert is OFF",
			Tip:  "Existing client certificate on this host. Leave empty when Issue cert is ON. Using both is rejected.",
		},
		{
			Key: "key_path", Label: "Key path", Kind: fieldFile, AllowedTypes: []string{".key", ".pem"},
			Hint: "only if Issue cert is OFF",
			Tip:  "Existing client private key. Pair with Cert path when not using Issue cert.",
		},
	}
}

func binaryCreateFields() []fieldDef {
	return []fieldDef{
		{
			Key: "name", Label: "Name", Section: "OpenVPN binary",
			Hint: "default, stuffing, v2.6",
			Tip:  "Short registry name you will pin on instances (e.g. default, stuffing). This is the binary_name, not the file path.",
		},
		{
			Key: "path", Label: "Path", Kind: fieldFile,
			Hint: "/usr/sbin/openvpn",
			Tip:  "Absolute path to the openvpn executable on this host. openvpnd will probe --version when you register it. Use a custom build path for forks.",
		},
		{
			Key: "notes", Label: "Notes",
			Hint: "optional description",
			Tip:  "Free-form note for operators (build flags, “has UDP stuffing”, package version). Not used by OpenVPN itself.",
		},
	}
}

func caCreateFields() []fieldDef {
	return []fieldDef{
		{
			Key: "name", Label: "Name", Section: "Certificate Authority",
			Hint: "default, home, work",
			Tip:  "Short CA id used in API paths and when issuing certs. Letters, digits, _ -.",
		},
		{
			Key: "common_name", Label: "Common Name",
			Hint: "My VPN CA",
			Tip:  "Subject CN on the CA certificate (shown to operators and in cert chains).",
		},
		{
			Key: "org", Label: "Org",
			Hint: "optional organization",
			Tip:  "Optional O= field on the CA certificate.",
		},
	}
}

func issueCertFields(caNames []string) []fieldDef {
	opts := append([]string{}, caNames...)
	if len(opts) == 0 {
		opts = []string{"(no CAs)"}
	}
	return []fieldDef{
		{
			Key: "ca_name", Label: "CA", Kind: fieldSelect, Options: opts, Section: "Issue certificate",
			Hint: "signing CA",
			Tip:  "Managed CA that will sign this leaf certificate.",
		},
		{
			Key: "kind", Label: "Kind", Kind: fieldSelect, Options: []string{"client", "server"},
			Hint: "client or server EKU",
			Tip:  "client = VPN user/machine; server = OpenVPN listener identity.",
		},
		{
			Key: "common_name", Label: "Common Name",
			Hint: "alice, vpn.example.com",
			Tip:  "Leaf certificate CN. For clients this is usually the VPN username; for servers often the public hostname.",
		},
	}
}

func truthy(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes" || s == "true" || s == "1" || s == "on"
}

func fillHeight(content string, width, height int) string {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		MaxWidth(width).
		Render(content)
}
