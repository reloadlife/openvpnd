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
	Hint         string
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
		b.WriteString(okStyle.Render("  " + f.note))
		b.WriteString("\n\n")
	}
	for i, field := range f.fields {
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
					val = selStyle.Render(" ◀ "+cur+" ▶ ")
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
				val = raw + dimStyle.Render("  [space/ctrl+o browse]")
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
		if field.Hint != "" && focused {
			b.WriteString(dimStyle.Render("                    " + field.Hint))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	help := f.help
	if help == "" {
		help = "tab/↑↓ move  ·  ←/→ role & toggles  ·  space/ctrl+o file browse  ·  enter save  ·  esc cancel"
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

func instanceCreateFields(binaries []string) []fieldDef {
	bins := append([]string{}, binaries...)
	if len(bins) == 0 {
		bins = []string{"default"}
	}
	return []fieldDef{
		// common
		{Key: "name", Label: "Name", Hint: "empty/auto → ovpn0, ovpn1…"},
		{Key: "role", Label: "Role", Kind: fieldSelect, Options: []string{"server", "client"}, Hint: "←/→ switches field set"},
		{Key: "binary", Label: "Binary", Kind: fieldSelect, Options: bins},
		{Key: "proto", Label: "Proto", Kind: fieldSelect, Options: []string{"udp", "tcp", "udp4", "tcp4", "udp6", "tcp6"}},
		{Key: "dev_type", Label: "Dev type", Kind: fieldSelect, Options: []string{"tun", "tap"}},
		{Key: "device", Label: "Device", Hint: "optional e.g. tun0 / ovpnc0"},
		{Key: "auth_mode", Label: "Auth mode", Kind: fieldSelect, Options: []string{"pki", "static_key"}},

		// server-only
		{Key: "port", Label: "Listen port", Hint: "empty → next free from 1194", Roles: []string{"server"}},
		{Key: "local_bind", Label: "Local bind", Hint: "optional IP to bind", Roles: []string{"server"}},
		{Key: "network", Label: "Server net", Hint: "empty → free 10.x.0.0/24", Roles: []string{"server"}},
		{Key: "topology", Label: "Topology", Kind: fieldSelect, Options: []string{"subnet", "net30", "p2p"}, Roles: []string{"server"}},
		{Key: "public_endpoint", Label: "Public EP", Hint: "vpn.example.com:1194 (client profiles)", Roles: []string{"server"}},
		{Key: "push_dns", Label: "Push DNS", Hint: "1.1.1.1,8.8.8.8", Roles: []string{"server"}},
		{Key: "push_routes", Label: "Push routes", Hint: "10.0.0.0/8,192.168.0.0/16", Roles: []string{"server"}},
		{Key: "push_domain", Label: "Push domain", Hint: "internal.lan", Roles: []string{"server"}},
		{Key: "redirect_gw", Label: "Redirect GW", Kind: fieldBool, Roles: []string{"server"}},
		{Key: "issue_cert", Label: "Issue cert", Hint: "auto mTLS server cert from CA", Kind: fieldBool, Roles: []string{"server"}},
		{Key: "create_ca", Label: "Create CA", Hint: "if no CA exists, create default", Kind: fieldBool, Roles: []string{"server"}},
		{Key: "ca_name", Label: "CA name", Hint: "default first CA", Roles: []string{"server"}},
		{Key: "server_cn", Label: "Server CN", Hint: "default public EP host / name", Roles: []string{"server"}},
		{Key: "tls_crypt", Label: "TLS-crypt", Hint: "generate with issue", Kind: fieldBool, Roles: []string{"server"}},
		{Key: "data_ciphers", Label: "Data ciphers", Hint: "empty → modern GCM set", Roles: []string{"server"}},
		{Key: "auth", Label: "Auth digest", Hint: "empty → SHA256", Roles: []string{"server"}},
		{Key: "cipher", Label: "Cipher", Hint: "legacy single cipher (optional)", Roles: []string{"server"}},
		{Key: "pki_ca", Label: "CA path", Hint: "manual override", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem", ".cer"}, Roles: []string{"server"}},
		{Key: "pki_cert", Label: "Cert path", Hint: "manual override", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem"}, Roles: []string{"server"}},
		{Key: "pki_key", Label: "Key path", Hint: "manual override", Kind: fieldFile, AllowedTypes: []string{".key", ".pem"}, Roles: []string{"server"}},

		// client-only (outbound OpenVPN client instance)
		{Key: "profile", Label: "Profile", Hint: ".ovpn / .conf — browse and auto-fill remotes + certs", Kind: fieldFile, AllowedTypes: []string{".ovpn", ".conf"}, Roles: []string{"client"}},
		{Key: "remote", Label: "Remote(s)", Hint: "host:port or host:port:proto, CSV", Roles: []string{"client"}},
		{Key: "pki_ca", Label: "CA path", Hint: "ca.crt", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem", ".cer"}, Roles: []string{"client"}},
		{Key: "pki_cert", Label: "Cert path", Hint: "client.crt", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem"}, Roles: []string{"client"}},
		{Key: "pki_key", Label: "Key path", Hint: "client.key", Kind: fieldFile, AllowedTypes: []string{".key", ".pem"}, Roles: []string{"client"}},
		{Key: "tls_crypt_path", Label: "TLS-crypt", Hint: "optional tls-crypt key file", Kind: fieldFile, AllowedTypes: []string{".key", ".pem", ".txt"}, Roles: []string{"client"}},
		{Key: "static_key", Label: "Static key", Hint: "for auth_mode=static_key", Kind: fieldFile, AllowedTypes: []string{".key"}, Roles: []string{"client"}},
		{Key: "data_ciphers", Label: "Data ciphers", Hint: "optional; from profile if set", Roles: []string{"client"}},
		{Key: "auth", Label: "Auth digest", Hint: "optional; from profile if set", Roles: []string{"client"}},
		{Key: "cipher", Label: "Cipher", Hint: "optional; from profile if set", Roles: []string{"client"}},
		{Key: "features", Label: "Features", Hint: "CSV: explicit_exit_notify,mssfix,…", Roles: []string{"client"}},

		// common extensions
		{Key: "features", Label: "Features", Hint: "CSV: udp_stuffing,mssfix,…", Roles: []string{"server"}},
		{Key: "plugin", Label: "Plugin", Hint: "path to .so — browse or type path + args", Kind: fieldFile},
		{Key: "extra", Label: "Extra conf", Hint: "raw openvpn directives (fork options)"},
	}
}

func clientCreateFields(servers []string) []fieldDef {
	opts := append([]string{}, servers...)
	if len(opts) == 0 {
		opts = []string{"(no servers)"}
	}
	return []fieldDef{
		{Key: "instance", Label: "Instance", Kind: fieldSelect, Options: opts},
		{Key: "cn", Label: "Common name", Hint: "certificate CN"},
		{Key: "name", Label: "Display name", Hint: "alice, phone, …"},
		{Key: "static_ip", Label: "Static IP", Hint: "empty = auto from pool"},
		{Key: "cert_path", Label: "Cert path", Hint: "client.crt for profiles", Kind: fieldFile, AllowedTypes: []string{".crt", ".pem"}},
		{Key: "key_path", Label: "Key path", Hint: "client.key for profiles", Kind: fieldFile, AllowedTypes: []string{".key", ".pem"}},
	}
}

func binaryCreateFields() []fieldDef {
	return []fieldDef{
		{Key: "name", Label: "Name", Hint: "v26, legacy, …"},
		{Key: "path", Label: "Path", Hint: "/opt/openvpn/sbin/openvpn", Kind: fieldFile},
		{Key: "notes", Label: "Notes", Hint: "optional"},
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
