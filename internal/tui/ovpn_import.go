package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// clientProfile is a minimal parse of an OpenVPN client .ovpn/.conf for TUI import.
type clientProfile struct {
	Remotes   string // CSV host:port[:proto]
	Proto     string
	DevType   string
	AuthMode  string
	Cipher    string
	Auth      string
	DataCiphers string
	CAPath    string
	CertPath  string
	KeyPath   string
	TLSCrypt  string
	StaticKey string
	Extra     string // leftover interesting directives
}

var (
	reRemote   = regexp.MustCompile(`(?i)^\s*remote\s+(\S+)(?:\s+(\d+))?(?:\s+(\S+))?\s*$`)
	reProto    = regexp.MustCompile(`(?i)^\s*proto\s+(\S+)\s*$`)
	reDev      = regexp.MustCompile(`(?i)^\s*dev(?:-type)?\s+(\S+)\s*$`)
	reCipher   = regexp.MustCompile(`(?i)^\s*cipher\s+(\S+)\s*$`)
	reAuth     = regexp.MustCompile(`(?i)^\s*auth\s+(\S+)\s*$`)
	reDataCiph = regexp.MustCompile(`(?i)^\s*data-ciphers\s+(.+)\s*$`)
	rePathDir  = regexp.MustCompile(`(?i)^\s*(ca|cert|key|tls-crypt|tls-auth|secret)\s+(\S+)\s*$`)
)

var inlineTags = []string{"ca", "cert", "key", "tls-crypt", "tls-auth", "secret"}

// extractInlineBlocks pulls <tag>...</tag> PEM sections (RE2 has no backrefs).
func extractInlineBlocks(content string) (map[string]string, string) {
	inline := map[string]string{}
	body := content
	for _, tag := range inlineTags {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		for {
			low := strings.ToLower(body)
			i := strings.Index(low, open)
			if i < 0 {
				break
			}
			j := strings.Index(low[i+len(open):], close)
			if j < 0 {
				break
			}
			start := i + len(open)
			end := i + len(open) + j
			inline[tag] = strings.TrimSpace(body[start:end]) + "\n"
			body = body[:i] + "\n" + body[end+len(close):]
			low = strings.ToLower(body) // loop again if duplicates
			_ = low
		}
	}
	return inline, body
}

// parseClientProfileFile reads a client profile and extracts remotes + material paths.
// Inline PEM blocks are written under destDir (created if needed).
func parseClientProfileFile(path, destDir string) (clientProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return clientProfile{}, err
	}
	return parseClientProfile(string(data), filepath.Dir(path), destDir)
}

func parseClientProfile(content, baseDir, destDir string) (clientProfile, error) {
	var p clientProfile
	p.AuthMode = "pki"

	// Extract inline blocks first so line scan ignores them.
	inline, body := extractInlineBlocks(content)

	var remotes []string
	var extra []string
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, ";") {
			continue
		}
		if m := reRemote.FindStringSubmatch(line); m != nil {
			host := m[1]
			port := m[2]
			proto := m[3]
			if port == "" {
				port = "1194"
			}
			r := host + ":" + port
			if proto != "" {
				r += ":" + proto
				if p.Proto == "" {
					p.Proto = strings.ToLower(proto)
				}
			}
			remotes = append(remotes, r)
			continue
		}
		if m := reProto.FindStringSubmatch(line); m != nil {
			p.Proto = strings.ToLower(m[1])
			continue
		}
		if m := reDev.FindStringSubmatch(line); m != nil {
			d := strings.ToLower(m[1])
			if strings.HasPrefix(d, "tun") {
				p.DevType = "tun"
			} else if strings.HasPrefix(d, "tap") {
				p.DevType = "tap"
			}
			continue
		}
		if m := reCipher.FindStringSubmatch(line); m != nil {
			p.Cipher = m[1]
			continue
		}
		if m := reAuth.FindStringSubmatch(line); m != nil {
			p.Auth = m[1]
			continue
		}
		if m := reDataCiph.FindStringSubmatch(line); m != nil {
			p.DataCiphers = strings.TrimSpace(m[1])
			continue
		}
		if m := rePathDir.FindStringSubmatch(line); m != nil {
			tag := strings.ToLower(m[1])
			fp := m[2]
			if !filepath.IsAbs(fp) {
				fp = filepath.Join(baseDir, fp)
			}
			switch tag {
			case "ca":
				p.CAPath = fp
			case "cert":
				p.CertPath = fp
			case "key":
				p.KeyPath = fp
			case "tls-crypt":
				p.TLSCrypt = fp
			case "tls-auth", "secret":
				p.StaticKey = fp
				if tag == "secret" {
					p.AuthMode = "static_key"
				}
			}
			continue
		}
		// Keep a few useful long-tail lines
		low := strings.ToLower(trim)
		switch {
		case strings.HasPrefix(low, "remote-cert-tls"),
			strings.HasPrefix(low, "verb "),
			strings.HasPrefix(low, "nobind"),
			strings.HasPrefix(low, "resolv-retry"),
			strings.HasPrefix(low, "persist-"),
			strings.HasPrefix(low, "explicit-exit-notify"),
			strings.HasPrefix(low, "mssfix"),
			strings.HasPrefix(low, "tun-mtu"),
			strings.HasPrefix(low, "sndbuf"),
			strings.HasPrefix(low, "rcvbuf"),
			strings.HasPrefix(low, "comp-lzo"),
			strings.HasPrefix(low, "compress"),
			strings.HasPrefix(low, "pull"),
			strings.HasPrefix(low, "auth-nocache"),
			strings.HasPrefix(low, "redirect-gateway"):
			extra = append(extra, trim)
		}
	}

	if len(remotes) == 0 && len(inline) == 0 && p.CAPath == "" {
		return clientProfile{}, fmt.Errorf("no remotes or cert material found in profile")
	}
	p.Remotes = strings.Join(remotes, ",")

	if len(inline) > 0 {
		if err := os.MkdirAll(destDir, 0o700); err != nil {
			return clientProfile{}, fmt.Errorf("mkdir import dir: %w", err)
		}
		write := func(name, body string) (string, error) {
			path := filepath.Join(destDir, name)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				return "", err
			}
			return path, nil
		}
		var err error
		if body, ok := inline["ca"]; ok {
			if p.CAPath, err = write("ca.crt", body); err != nil {
				return clientProfile{}, err
			}
		}
		if body, ok := inline["cert"]; ok {
			if p.CertPath, err = write("client.crt", body); err != nil {
				return clientProfile{}, err
			}
		}
		if body, ok := inline["key"]; ok {
			if p.KeyPath, err = write("client.key", body); err != nil {
				return clientProfile{}, err
			}
		}
		if body, ok := inline["tls-crypt"]; ok {
			if p.TLSCrypt, err = write("tls-crypt.key", body); err != nil {
				return clientProfile{}, err
			}
		}
		if body, ok := inline["tls-auth"]; ok {
			if p.StaticKey, err = write("tls-auth.key", body); err != nil {
				return clientProfile{}, err
			}
		}
		if body, ok := inline["secret"]; ok {
			if p.StaticKey, err = write("static.key", body); err != nil {
				return clientProfile{}, err
			}
			p.AuthMode = "static_key"
		}
	}

	if len(extra) > 0 {
		p.Extra = strings.Join(extra, "\n") + "\n"
	}
	return p, nil
}

func importDestDir(profilePath string) string {
	base := strings.TrimSuffix(filepath.Base(profilePath), filepath.Ext(profilePath))
	if base == "" {
		base = "profile"
	}
	// Prefer next to the profile (admin often co-locates material).
	return filepath.Join(filepath.Dir(profilePath), ".openvpnd-import", base)
}
