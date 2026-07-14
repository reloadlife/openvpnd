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
)

type fieldDef struct {
	Key     string
	Label   string
	Hint    string
	Width   int
	Kind    string
	Options []string
}

type formModel struct {
	title  string
	fields []fieldDef
	inputs []textinput.Model
	selIdx []int
	focus  int
	err    string
	width  int
	height int
	help   string
	note   string
}

func newForm(title string, fields []fieldDef, values map[string]string) formModel {
	inputs := make([]textinput.Model, len(fields))
	selIdx := make([]int, len(fields))
	for i, f := range fields {
		kind := f.Kind
		if kind == "" {
			kind = fieldText
		}
		fields[i].Kind = kind
		ti := textinput.New()
		ti.Placeholder = f.Hint
		w := f.Width
		if w <= 0 {
			w = 56
		}
		ti.CharLimit = 1024
		ti.Width = w
		ti.Prompt = ""
		if values != nil {
			if v, ok := values[f.Key]; ok {
				switch kind {
				case fieldSelect:
					selIdx[i] = indexOf(f.Options, v)
					if selIdx[i] < 0 && len(f.Options) > 0 {
						selIdx[i] = 0
					}
				case fieldBool:
					if truthy(v) {
						selIdx[i] = 1
					}
				default:
					ti.SetValue(v)
				}
			}
		}
		inputs[i] = ti
	}
	f := formModel{title: title, fields: fields, inputs: inputs, selIdx: selIdx, focus: 0}
	_ = f.focusInput()
	return f
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
			f.focus = (f.focus + 1) % len(f.fields)
			return f, f.focusInput()
		case "shift+tab", "up":
			f.focus = (f.focus + len(f.fields) - 1) % len(f.fields)
			return f, f.focusInput()
		case "left", "h":
			if f.fields[f.focus].Kind == fieldSelect || f.fields[f.focus].Kind == fieldBool {
				f.cycleSelect(f.focus, -1)
				return f, nil
			}
		case "right", "l", " ":
			if f.fields[f.focus].Kind == fieldSelect || f.fields[f.focus].Kind == fieldBool {
				f.cycleSelect(f.focus, +1)
				return f, nil
			}
		}
	}
	if f.fields[f.focus].Kind == fieldText {
		var cmd tea.Cmd
		f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
		return f, cmd
	}
	return f, nil
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
		if i == f.focus && f.fields[i].Kind == fieldText {
			f.inputs[i].Focus()
		} else {
			f.inputs[i].Blur()
		}
	}
	return textinput.Blink
}

func (f formModel) Values() map[string]string {
	out := make(map[string]string, len(f.fields))
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
		help = "tab/↑↓ move  ·  ←/→ or space change  ·  enter save  ·  esc cancel"
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
	// Fill the entire main pane (caller already reserved chrome rows).
	box := panelStyle.Width(w).Height(h).MaxHeight(h)
	return box.Render(inner)
}

func instanceCreateFields(binaries []string) []fieldDef {
	bins := append([]string{}, binaries...)
	if len(bins) == 0 {
		bins = []string{"default"}
	}
	return []fieldDef{
		{Key: "name", Label: "Name", Hint: "empty/auto → ovpn0, ovpn1…"},
		{Key: "role", Label: "Role", Kind: fieldSelect, Options: []string{"server", "client"}},
		{Key: "binary", Label: "Binary", Kind: fieldSelect, Options: bins},
		{Key: "proto", Label: "Proto", Kind: fieldSelect, Options: []string{"udp", "tcp", "udp4", "tcp4", "udp6", "tcp6"}},
		{Key: "port", Label: "Port", Hint: "empty → next free from 1194"},
		{Key: "local_bind", Label: "Local bind", Hint: "optional IP to bind"},
		{Key: "dev_type", Label: "Dev type", Kind: fieldSelect, Options: []string{"tun", "tap"}},
		{Key: "device", Label: "Device", Hint: "optional e.g. tun0"},
		// server
		{Key: "network", Label: "Server net", Hint: "empty → free 10.x.0.0/24"},
		{Key: "topology", Label: "Topology", Kind: fieldSelect, Options: []string{"subnet", "net30", "p2p"}},
		{Key: "public_endpoint", Label: "Public EP", Hint: "vpn.example.com:1194 (profiles)"},
		{Key: "push_dns", Label: "Push DNS", Hint: "1.1.1.1,8.8.8.8"},
		{Key: "push_routes", Label: "Push routes", Hint: "10.0.0.0/8,192.168.0.0/16"},
		{Key: "push_domain", Label: "Push domain", Hint: "internal.lan"},
		{Key: "redirect_gw", Label: "Redirect GW", Kind: fieldBool},
		// client
		{Key: "remote", Label: "Remote(s)", Hint: "host:port or host:port:proto, CSV"},
		// crypto / auto PKI
		{Key: "auth_mode", Label: "Auth mode", Kind: fieldSelect, Options: []string{"pki", "static_key"}},
		{Key: "issue_cert", Label: "Issue cert", Hint: "server: auto mTLS from CA", Kind: fieldBool},
		{Key: "create_ca", Label: "Create CA", Hint: "if no CA exists, create default", Kind: fieldBool},
		{Key: "ca_name", Label: "CA name", Hint: "default first CA"},
		{Key: "server_cn", Label: "Server CN", Hint: "default public EP host / name"},
		{Key: "tls_crypt", Label: "TLS-crypt", Hint: "generate with issue", Kind: fieldBool},
		{Key: "data_ciphers", Label: "Data ciphers", Hint: "empty → modern GCM set"},
		{Key: "auth", Label: "Auth digest", Hint: "empty → SHA256"},
		{Key: "cipher", Label: "Cipher", Hint: "legacy single cipher (optional)"},
		// manual paths (override auto issue)
		{Key: "pki_ca", Label: "CA path", Hint: "manual override"},
		{Key: "pki_cert", Label: "Cert path", Hint: "manual override"},
		{Key: "pki_key", Label: "Key path", Hint: "manual override"},
		// extensions / custom openvpn
		{Key: "features", Label: "Features", Hint: "CSV: udp_stuffing,mssfix,explicit_exit_notify,…"},
		{Key: "plugin", Label: "Plugin", Hint: "absolute path to .so (optional args after space)"},
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
		{Key: "cert_path", Label: "Cert path", Hint: "client.crt for profiles"},
		{Key: "key_path", Label: "Key path", Hint: "client.key for profiles"},
	}
}

func binaryCreateFields() []fieldDef {
	return []fieldDef{
		{Key: "name", Label: "Name", Hint: "v26, legacy, …"},
		{Key: "path", Label: "Path", Hint: "/opt/openvpn/sbin/openvpn"},
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
	// Width + Height force the content box to occupy the full terminal region.
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		MaxWidth(width).
		Render(content)
}
