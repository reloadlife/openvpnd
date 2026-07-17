package confgen

import (
	"fmt"
	"os"
	"strings"

	"github.com/reloadlife/openvpnd/internal/db"
)

// ProfileMaterial is PEM content (or empty if unavailable).
type ProfileMaterial struct {
	CA       string // PEM
	Cert     string // PEM
	Key      string // PEM
	TLSCrypt string // optional PEM/key block
}

// ProfileOptions controls .ovpn rendering.
type ProfileOptions struct {
	// Remote host:port advertised to clients (required).
	RemoteHost string
	RemotePort int
	Proto      string
	DevType    string
	// Inline embeds PEM blocks; if false, uses file paths from Instance/Client.
	Inline bool
	// Extra lines appended (advanced).
	Extra string
}

// LoadMaterialFromPaths reads PEM files for profile generation.
func LoadMaterialFromPaths(caPath, certPath, keyPath, tlsCryptPath string) (ProfileMaterial, error) {
	var m ProfileMaterial
	var err error
	if caPath != "" {
		m.CA, err = readFileString(caPath)
		if err != nil {
			return m, fmt.Errorf("ca: %w", err)
		}
	}
	if certPath != "" {
		m.Cert, err = readFileString(certPath)
		if err != nil {
			return m, fmt.Errorf("cert: %w", err)
		}
	}
	if keyPath != "" {
		m.Key, err = readFileString(keyPath)
		if err != nil {
			return m, fmt.Errorf("key: %w", err)
		}
	}
	if tlsCryptPath != "" {
		m.TLSCrypt, err = readFileString(tlsCryptPath)
		if err != nil {
			return m, fmt.Errorf("tls-crypt: %w", err)
		}
	}
	return m, nil
}

func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// RenderClientProfile builds a portable .ovpn for OpenVPN Connect / third-party clients.
func RenderClientProfile(inst db.Instance, client db.Client, mat ProfileMaterial, opt ProfileOptions) (string, error) {
	host := strings.TrimSpace(opt.RemoteHost)
	if host == "" {
		host = strings.TrimSpace(inst.PublicEndpoint)
	}
	// PublicEndpoint may be host:port
	port := opt.RemotePort
	if port == 0 {
		port = inst.Port
	}
	if port == 0 {
		port = 1194
	}
	if h, p, ok := splitHostPort(host); ok {
		host = h
		if opt.RemotePort == 0 {
			port = p
		}
	}
	if host == "" {
		return "", fmt.Errorf("public endpoint required (set instance public_endpoint or profile remote)")
	}
	if mat.CA == "" && inst.PKICaPath == "" {
		return "", fmt.Errorf("CA material required for client profile")
	}
	if mat.Cert == "" && client.ClientCertPath == "" {
		return "", fmt.Errorf("client certificate required (set client_cert_path)")
	}
	if mat.Key == "" && client.ClientKeyPath == "" {
		return "", fmt.Errorf("client key required (set client_key_path)")
	}

	proto := opt.Proto
	if proto == "" {
		proto = inst.Proto
	}
	if proto == "" {
		proto = "udp"
	}
	dev := opt.DevType
	if dev == "" {
		dev = inst.DevType
	}
	if dev == "" {
		dev = "tun"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# openvpnd profile · instance=%s cn=%s\n", inst.Name, client.CommonName)
	// Portable profile: OpenVPN Connect (iOS/Android), community 2.5+, MikroTik-friendly defaults.
	fmt.Fprintf(&b, "client\n")
	fmt.Fprintf(&b, "dev %s\n", dev)
	fmt.Fprintf(&b, "proto %s\n", proto)
	fmt.Fprintf(&b, "remote %s %d\n", host, port)
	fmt.Fprintf(&b, "resolv-retry infinite\n")
	fmt.Fprintf(&b, "nobind\n")
	fmt.Fprintf(&b, "persist-key\n")
	fmt.Fprintf(&b, "persist-tun\n")
	fmt.Fprintf(&b, "remote-cert-tls server\n")
	fmt.Fprintf(&b, "auth-nocache\n")
	fmt.Fprintf(&b, "verb 3\n")
	// Ignore options older peers (MikroTik, ancient Connect) do not understand.
	fmt.Fprintf(&b, "ignore-unknown-option data-ciphers\n")
	fmt.Fprintf(&b, "ignore-unknown-option data-ciphers-fallback\n")
	// Friendly UDP disconnect (OpenVPN Connect / community clients)
	if strings.HasPrefix(strings.ToLower(proto), "udp") {
		fmt.Fprintf(&b, "explicit-exit-notify 1\n")
	}
	// Always emit cipher + data-ciphers for wide client coverage:
	//  - OpenVPN ≤2.4 / some iOS builds want `cipher`
	//  - 2.5+ prefer data-ciphers; fallback for NCP negotiation
	cipher := strings.TrimSpace(inst.Cipher)
	dataCiphers := strings.TrimSpace(inst.DataCiphers)
	if dataCiphers == "" {
		dataCiphers = "AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305"
	}
	if cipher == "" {
		// First GCM entry from data-ciphers, else AES-256-GCM
		cipher = "AES-256-GCM"
		for _, part := range strings.Split(dataCiphers, ":") {
			part = strings.TrimSpace(part)
			if part != "" {
				cipher = part
				break
			}
		}
	}
	fmt.Fprintf(&b, "cipher %s\n", cipher)
	fmt.Fprintf(&b, "data-ciphers %s\n", dataCiphers)
	fmt.Fprintf(&b, "data-ciphers-fallback AES-256-CBC\n")
	auth := strings.TrimSpace(inst.AuthDigest)
	if auth == "" {
		auth = "SHA256"
	}
	fmt.Fprintf(&b, "auth %s\n", auth)
	// Mobile clients: float allows rebind after sleep/network change.
	fmt.Fprintf(&b, "float\n")
	fmt.Fprintf(&b, "reneg-sec 0\n")

	inline := opt.Inline
	if inline || (mat.CA != "" && mat.Cert != "" && mat.Key != "") {
		// Prefer inline when PEMs loaded (best for URL/QR import).
		if mat.CA == "" || mat.Cert == "" || mat.Key == "" {
			return "", fmt.Errorf("inline profile needs ca, cert, and key PEMs")
		}
		writeInlinePEM(&b, "ca", mat.CA)
		writeInlinePEM(&b, "cert", mat.Cert)
		writeInlinePEM(&b, "key", mat.Key)
		if mat.TLSCrypt != "" {
			writeInlinePEM(&b, "tls-crypt", mat.TLSCrypt)
		}
	} else {
		fmt.Fprintf(&b, "ca %s\n", inst.PKICaPath)
		fmt.Fprintf(&b, "cert %s\n", client.ClientCertPath)
		fmt.Fprintf(&b, "key %s\n", client.ClientKeyPath)
		if inst.PKITLSCryptPath != "" {
			fmt.Fprintf(&b, "tls-crypt %s\n", inst.PKITLSCryptPath)
		}
	}

	if opt.Extra != "" {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(opt.Extra))
	}
	return b.String(), nil
}

func writeInlinePEM(b *strings.Builder, tag, pem string) {
	pem = strings.TrimSpace(pem) + "\n"
	fmt.Fprintf(b, "<%s>\n%s</%s>\n", tag, pem, tag)
}

func splitHostPort(s string) (host string, port int, ok bool) {
	s = strings.TrimSpace(s)
	// host:port — avoid breaking bare IPv6 without brackets for now
	if strings.Count(s, ":") != 1 {
		return "", 0, false
	}
	i := strings.LastIndex(s, ":")
	if i <= 0 {
		return "", 0, false
	}
	host = s[:i]
	var p int
	if _, err := fmt.Sscanf(s[i+1:], "%d", &p); err != nil || p <= 0 {
		return "", 0, false
	}
	return host, p, true
}
