// Package confimport parses existing OpenVPN .conf / .ovpn files into
// instance create fields for adopt/import flows.
package confimport

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Remote is a client remote endpoint.
type Remote struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Proto string `json:"proto,omitempty"`
}

// Plugin is an OpenVPN --plugin module.
type Plugin struct {
	Path string   `json:"path"`
	Args []string `json:"args,omitempty"`
}

// Result is the structured view of a parsed OpenVPN conf.
type Result struct {
	Role            string   `json:"role,omitempty"`
	Proto           string   `json:"proto,omitempty"`
	Port            int      `json:"port,omitempty"`
	LocalBind       string   `json:"local_bind,omitempty"`
	DevType         string   `json:"dev_type,omitempty"`
	Device          string   `json:"device,omitempty"`
	ServerNetwork   string   `json:"server_network,omitempty"`
	Topology        string   `json:"topology,omitempty"`
	Remotes         []Remote `json:"remotes,omitempty"`
	AuthMode        string   `json:"auth_mode,omitempty"`
	PKICaPath       string   `json:"pki_ca_path,omitempty"`
	PKICertPath     string   `json:"pki_cert_path,omitempty"`
	PKIKeyPath      string   `json:"pki_key_path,omitempty"`
	PKIDHPath       string   `json:"pki_dh_path,omitempty"`
	TLSCryptPath    string   `json:"pki_tls_crypt_path,omitempty"`
	StaticKeyPath   string   `json:"static_key_path,omitempty"`
	Cipher          string   `json:"cipher,omitempty"`
	DataCiphers     string   `json:"data_ciphers,omitempty"`
	AuthDigest      string   `json:"auth_digest,omitempty"`
	ExtraDirectives string   `json:"extra_directives,omitempty"`
	RedirectGateway bool     `json:"redirect_gateway,omitempty"`
	PushDNS         []string `json:"push_dns,omitempty"`
	PushRoutes      []string `json:"push_routes,omitempty"`
	PushDomain      string   `json:"push_domain,omitempty"`
	MaxClients      int      `json:"max_clients,omitempty"`
	TLSVersionMin   string   `json:"tls_version_min,omitempty"`
	TunMTU          int      `json:"tun_mtu,omitempty"`
	Sndbuf          int      `json:"sndbuf,omitempty"`
	Rcvbuf          int      `json:"rcvbuf,omitempty"`
	ServerIPv6      string   `json:"server_ipv6,omitempty"`
	Plugins         []Plugin `json:"plugins,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// Directives openvpnd injects / owns — ignored on import.
var ignorePrefixes = []string{
	"management",
	"management-client-user",
	"management-client-group",
	"status",
	"writepid",
	"verb",
	"persist-key",
	"persist-tun",
	"persist-local-ip",
	"persist-remote-ip",
	"script-security", // often daemon-controlled
	"daemon",
	"user",
	"group",
	"chroot",
	"setenv FORWARD_COMPATIBLE",
	"setenv opt",
	"keepalive", // confgen injects keepalive 10 60
}

// Parse converts OpenVPN conf/ovpn content into structured Result.
func Parse(content string) (Result, error) {
	var r Result
	r.AuthMode = "pki"

	// Strip inline PEM/XML blocks; warn if present (file refs only for adopt).
	body, inlines := stripInlineBlocks(content)
	for tag := range inlines {
		r.Warnings = append(r.Warnings, fmt.Sprintf("inline <%s> block ignored; use file path directives or extract material first", tag))
	}

	var extra []string
	sawClient := false
	sawServer := false

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		// strip trailing inline comment (best-effort; ignore quoted #)
		if i := indexUnquoted(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
			if line == "" {
				continue
			}
		}

		low := strings.ToLower(line)
		if shouldIgnore(low) {
			continue
		}

		fields := splitFields(line)
		if len(fields) == 0 {
			continue
		}
		key := strings.ToLower(fields[0])

		switch key {
		case "client":
			sawClient = true
			r.Role = "client"
			continue
		case "server":
			// server NETWORK NETMASK
			sawServer = true
			r.Role = "server"
			if len(fields) >= 3 {
				cidr, err := networkMaskToCIDR(fields[1], fields[2])
				if err != nil {
					r.Warnings = append(r.Warnings, fmt.Sprintf("server %s %s: %v", fields[1], fields[2], err))
				} else {
					r.ServerNetwork = cidr
				}
			}
			continue
		case "mode":
			if len(fields) >= 2 && strings.EqualFold(fields[1], "server") {
				sawServer = true
				r.Role = "server"
			}
			continue
		case "tls-server":
			sawServer = true
			if r.Role == "" {
				r.Role = "server"
			}
			continue
		case "tls-client":
			sawClient = true
			if r.Role == "" {
				r.Role = "client"
			}
			continue
		case "proto":
			if len(fields) >= 2 {
				r.Proto = strings.ToLower(fields[1])
			}
			continue
		case "port", "lport":
			if len(fields) >= 2 {
				if p, err := strconv.Atoi(fields[1]); err == nil {
					r.Port = p
				}
			}
			continue
		case "local":
			if len(fields) >= 2 {
				r.LocalBind = fields[1]
			}
			continue
		case "dev":
			if len(fields) >= 2 {
				dev := fields[1]
				lowDev := strings.ToLower(dev)
				if lowDev == "tun" || lowDev == "tap" {
					r.DevType = lowDev
				} else if strings.HasPrefix(lowDev, "tun") {
					r.DevType = "tun"
					r.Device = dev
				} else if strings.HasPrefix(lowDev, "tap") {
					r.DevType = "tap"
					r.Device = dev
				} else {
					r.Device = dev
				}
			}
			continue
		case "dev-type":
			if len(fields) >= 2 {
				r.DevType = strings.ToLower(fields[1])
			}
			continue
		case "topology":
			if len(fields) >= 2 {
				r.Topology = strings.ToLower(fields[1])
			}
			continue
		case "remote":
			// remote host [port] [proto]
			if len(fields) < 2 {
				continue
			}
			rem := Remote{Host: fields[1], Port: 1194}
			if len(fields) >= 3 {
				if p, err := strconv.Atoi(fields[2]); err == nil {
					rem.Port = p
				} else {
					// host proto (no port)
					rem.Proto = strings.ToLower(fields[2])
				}
			}
			if len(fields) >= 4 {
				rem.Proto = strings.ToLower(fields[3])
			}
			if rem.Proto != "" && r.Proto == "" {
				r.Proto = rem.Proto
			}
			r.Remotes = append(r.Remotes, rem)
			continue
		case "cipher":
			if len(fields) >= 2 {
				r.Cipher = fields[1]
			}
			continue
		case "data-ciphers":
			if len(fields) >= 2 {
				r.DataCiphers = strings.Join(fields[1:], " ")
			}
			continue
		case "auth":
			if len(fields) >= 2 {
				r.AuthDigest = fields[1]
			}
			continue
		case "ca":
			if len(fields) >= 2 {
				r.PKICaPath = fields[1]
			}
			continue
		case "cert":
			if len(fields) >= 2 {
				r.PKICertPath = fields[1]
			}
			continue
		case "key":
			if len(fields) >= 2 {
				r.PKIKeyPath = fields[1]
			}
			continue
		case "dh":
			if len(fields) >= 2 && !strings.EqualFold(fields[1], "none") {
				r.PKIDHPath = fields[1]
			}
			continue
		case "tls-crypt":
			if len(fields) >= 2 {
				r.TLSCryptPath = fields[1]
			}
			continue
		case "tls-auth":
			if len(fields) >= 2 {
				r.StaticKeyPath = fields[1]
				// tls-auth is still PKI mode with extra key
			}
			continue
		case "secret":
			if len(fields) >= 2 {
				r.StaticKeyPath = fields[1]
				r.AuthMode = "static_key"
			}
			continue
		case "push":
			// push "directive ..."
			payload := extractPushPayload(line)
			if payload == "" {
				continue
			}
			parsePush(&r, payload)
			continue
		case "plugin":
			if len(fields) >= 2 {
				pl := Plugin{Path: fields[1]}
				if len(fields) > 2 {
					pl.Args = fields[2:]
				}
				r.Plugins = append(r.Plugins, pl)
			}
			continue
		case "max-clients":
			if len(fields) >= 2 {
				if n, err := strconv.Atoi(fields[1]); err == nil {
					r.MaxClients = n
				}
			}
			continue
		case "tls-version-min":
			if len(fields) >= 2 {
				r.TLSVersionMin = fields[1]
			}
			continue
		case "tun-mtu":
			if len(fields) >= 2 {
				if n, err := strconv.Atoi(fields[1]); err == nil {
					r.TunMTU = n
				}
			}
			continue
		case "sndbuf":
			if len(fields) >= 2 {
				if n, err := strconv.Atoi(fields[1]); err == nil {
					r.Sndbuf = n
				}
			}
			continue
		case "rcvbuf":
			if len(fields) >= 2 {
				if n, err := strconv.Atoi(fields[1]); err == nil {
					r.Rcvbuf = n
				}
			}
			continue
		case "server-ipv6":
			if len(fields) >= 2 {
				r.ServerIPv6 = fields[1]
			}
			continue
		case "ifconfig-pool":
			// ifconfig-pool start end [netmask]
			if len(fields) >= 3 {
				// stored only as extra; CreateInput has pool fields
				extra = append(extra, line)
			}
			continue
		case "redirect-gateway":
			r.RedirectGateway = true
			// keep args in extra if any beyond the bare flag
			if len(fields) > 1 {
				extra = append(extra, line)
			}
			continue
		default:
			// Known no-ops we already handled role-ish for
			if key == "nobind" || key == "pull" || key == "remote-cert-tls" ||
				key == "explicit-exit-notify" || key == "resolv-retry" ||
				key == "auth-nocache" || key == "comp-lzo" || key == "compress" ||
				key == "mssfix" || key == "tun-mtu-extra" || key == "fragment" ||
				key == "mute" || key == "fast-io" || key == "txqueuelen" ||
				key == "replay-window" || key == "hand-window" || key == "tran-window" ||
				key == "tls-cipher" || key == "tls-ciphersuites" || key == "tls-groups" ||
				key == "remote-cert-eku" || key == "verify-x509-name" || key == "ns-cert-type" ||
				key == "key-direction" || key == "auth-user-pass" || key == "auth-retry" ||
				key == "connect-retry" || key == "connect-retry-max" || key == "connect-timeout" ||
				key == "server-bridge" || key == "duplicate-cn" || key == "client-to-client" ||
				key == "client-config-dir" || key == "ccd-exclusive" || key == "username-as-common-name" ||
				key == "push-reset" || key == "ifconfig-pool-persist" || key == "status-version" ||
				key == "log" || key == "log-append" || key == "syslog" ||
				key == "up" || key == "down" || key == "route-up" || key == "route-pre-down" ||
				key == "learn-address" || key == "client-connect" || key == "client-disconnect" ||
				key == "tls-verify" || key == "auth-user-pass-verify" ||
				key == "tmp-dir" || key == "chroot" || key == "cd" ||
				key == "setenv" || key == "setenv-safe" ||
				key == "socket-flags" || key == "tcp-nodelay" ||
				key == "inactive" || key == "ping" || key == "ping-exit" || key == "ping-restart" ||
				key == "reneg-sec" || key == "reneg-bytes" || key == "reneg-pkts" ||
				key == "max-routes-per-client" || key == "ifconfig-ipv6" ||
				key == "route" || key == "route-ipv6" || key == "iroute" ||
				key == "topology" /* already handled */ ||
				strings.HasPrefix(key, "push-") {
				extra = append(extra, line)
				continue
			}
			// Unknown — preserve
			extra = append(extra, line)
		}
	}

	// Role inference
	if r.Role == "" {
		if sawServer {
			r.Role = "server"
		} else if sawClient || len(r.Remotes) > 0 {
			r.Role = "client"
		} else if r.ServerNetwork != "" {
			r.Role = "server"
		} else {
			return Result{}, fmt.Errorf("unable to determine role (need client, server, or remote)")
		}
	}
	// Conflict: both client and server markers
	if sawClient && sawServer {
		r.Warnings = append(r.Warnings, "conf has both client and server markers; using role="+r.Role)
	}

	if r.AuthMode == "" {
		r.AuthMode = "pki"
	}

	// Relative path warnings
	warnRel := func(label, p string) {
		if p == "" {
			return
		}
		if !strings.HasPrefix(p, "/") {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s path %q is not absolute; create will require absolute paths", label, p))
		}
	}
	warnRel("ca", r.PKICaPath)
	warnRel("cert", r.PKICertPath)
	warnRel("key", r.PKIKeyPath)
	warnRel("dh", r.PKIDHPath)
	warnRel("tls-crypt", r.TLSCryptPath)
	warnRel("static_key", r.StaticKeyPath)
	for i, pl := range r.Plugins {
		if pl.Path != "" && !strings.HasPrefix(pl.Path, "/") {
			r.Warnings = append(r.Warnings, fmt.Sprintf("plugins[%d] path %q is not absolute", i, pl.Path))
		}
	}

	// Merge line-scanned extras with push leftovers collected during parsePush.
	var parts []string
	if strings.TrimSpace(r.ExtraDirectives) != "" {
		parts = append(parts, strings.TrimRight(r.ExtraDirectives, "\n"))
	}
	if len(extra) > 0 {
		parts = append(parts, strings.Join(extra, "\n"))
	}
	if len(parts) > 0 {
		r.ExtraDirectives = strings.Join(parts, "\n") + "\n"
	}

	return r, nil
}

func shouldIgnore(low string) bool {
	for _, p := range ignorePrefixes {
		if low == p || strings.HasPrefix(low, p+" ") || strings.HasPrefix(low, p+"\t") {
			return true
		}
	}
	// also bare keys without args that match
	first := strings.Fields(low)
	if len(first) == 0 {
		return false
	}
	switch first[0] {
	case "management", "status", "writepid", "verb", "persist-key", "persist-tun",
		"persist-local-ip", "persist-remote-ip", "daemon", "keepalive":
		return true
	}
	return false
}

// networkMaskToCIDR converts OpenVPN "10.8.0.0 255.255.255.0" → "10.8.0.0/24".
func networkMaskToCIDR(network, mask string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(network))
	if ip == nil {
		return "", fmt.Errorf("invalid network IP %q", network)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", fmt.Errorf("only IPv4 server networks supported")
	}
	m := net.ParseIP(strings.TrimSpace(mask))
	if m == nil {
		return "", fmt.Errorf("invalid netmask %q", mask)
	}
	m4 := m.To4()
	if m4 == nil {
		return "", fmt.Errorf("invalid IPv4 netmask %q", mask)
	}
	ipMask := net.IPMask(m4)
	ones, bits := ipMask.Size()
	if bits == 0 {
		return "", fmt.Errorf("invalid netmask %q", mask)
	}
	// Canonical network address
	n := ip4.Mask(ipMask)
	return fmt.Sprintf("%s/%d", n.String(), ones), nil
}

// NetworkMaskToCIDR is exported for tests and callers.
func NetworkMaskToCIDR(network, mask string) (string, error) {
	return networkMaskToCIDR(network, mask)
}

func parsePush(r *Result, payload string) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return
	}
	low := strings.ToLower(payload)
	fields := splitFields(payload)
	if len(fields) == 0 {
		return
	}
	switch strings.ToLower(fields[0]) {
	case "redirect-gateway":
		r.RedirectGateway = true
		return
	case "dhcp-option":
		if len(fields) < 2 {
			return
		}
		opt := strings.ToUpper(fields[1])
		switch opt {
		case "DNS":
			if len(fields) >= 3 {
				r.PushDNS = append(r.PushDNS, fields[2])
			}
		case "DOMAIN", "DOMAIN-SEARCH", "ADAPTER_DOMAIN_SUFFIX":
			if len(fields) >= 3 {
				// Prefer DOMAIN; later DOMAIN-SEARCH overwrites only if empty DOMAIN
				if opt == "DOMAIN" || r.PushDomain == "" {
					r.PushDomain = fields[2]
				}
			}
		default:
			// keep unknown dhcp-option in extra as push line
			r.ExtraDirectives += "push \"" + payload + "\"\n"
		}
		return
	case "route":
		// route network netmask [gateway] [metric]
		if len(fields) >= 3 {
			cidr, err := networkMaskToCIDR(fields[1], fields[2])
			if err == nil {
				r.PushRoutes = append(r.PushRoutes, cidr)
				return
			}
		}
		if len(fields) >= 2 {
			// maybe already CIDR
			if strings.Contains(fields[1], "/") {
				r.PushRoutes = append(r.PushRoutes, fields[1])
				return
			}
		}
		r.ExtraDirectives += "push \"" + payload + "\"\n"
		return
	default:
		// preserve other push directives
		if !strings.HasPrefix(low, "block-outside-dns") {
			// still preserve everything unknown
		}
		r.ExtraDirectives += "push \"" + payload + "\"\n"
	}
}

func extractPushPayload(line string) string {
	// push "..."
	// push '...'
	// push bare-words (rare)
	s := strings.TrimSpace(line)
	if len(s) < 5 {
		return ""
	}
	// skip "push"
	rest := strings.TrimSpace(s[4:])
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		if j := strings.LastIndex(rest[1:], `"`); j >= 0 {
			return rest[1 : 1+j]
		}
		return strings.Trim(rest, `"`)
	}
	if rest[0] == '\'' {
		if j := strings.LastIndex(rest[1:], `'`); j >= 0 {
			return rest[1 : 1+j]
		}
		return strings.Trim(rest, `'`)
	}
	return rest
}

func splitFields(line string) []string {
	var out []string
	var b strings.Builder
	inQ := false
	var q byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQ {
			if c == q {
				inQ = false
				continue
			}
			if c == '\\' && i+1 < len(line) {
				b.WriteByte(line[i+1])
				i++
				continue
			}
			b.WriteByte(c)
			continue
		}
		if c == '"' || c == '\'' {
			inQ = true
			q = c
			continue
		}
		if c == ' ' || c == '\t' {
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteByte(c)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func indexUnquoted(s string, ch byte) int {
	inQ := false
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQ {
			if c == q {
				inQ = false
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQ = true
			q = c
			continue
		}
		if c == ch {
			return i
		}
	}
	return -1
}

var inlineTags = []string{"ca", "cert", "key", "tls-crypt", "tls-auth", "secret", "dh", "extra-certs", "tls-crypt-v2"}

func stripInlineBlocks(content string) (body string, blocks map[string]string) {
	blocks = map[string]string{}
	body = content
	for _, tag := range inlineTags {
		open := "<" + tag + ">"
		closeT := "</" + tag + ">"
		for {
			low := strings.ToLower(body)
			i := strings.Index(low, open)
			if i < 0 {
				break
			}
			j := strings.Index(low[i+len(open):], closeT)
			if j < 0 {
				break
			}
			start := i + len(open)
			end := i + len(open) + j
			blocks[tag] = strings.TrimSpace(body[start:end]) + "\n"
			body = body[:i] + "\n" + body[end+len(closeT):]
		}
	}
	return body, blocks
}
