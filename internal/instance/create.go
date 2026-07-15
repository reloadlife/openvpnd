// Package instance holds create/validation helpers for OpenVPN instances.
package instance

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/netutil"
)

// ensure filepath used (plugins abs path)

var nameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)

// CreateInput is normalized create intent (from API/TUI/CLI).
type CreateInput struct {
	Name            string
	Role            string
	Enabled         *bool
	BinaryName      string
	BinaryPath      string
	DevType         string
	Device          string
	Proto           string
	LocalBind       string
	Port            int
	Remotes         []db.Remote
	ServerNetwork   string
	Topology        string
	PoolStart       string
	PoolEnd         string
	AuthMode        string
	Cipher          string
	DataCiphers     string
	AuthDigest      string
	PushRoutes      []string
	PushDNS         []string
	PushDomain      string
	RedirectGateway bool
	PKICaPath       string
	PKICertPath     string
	PKIKeyPath      string
	PKITLSCrypt    string
	PKIDHPath       string
	StaticKeyPath   string
	ExtraDirectives string
	Plugins         []db.Plugin
	EnvVars         []db.EnvVar
	FeatureSets     []string
	PreUp           string
	PostUp          string
	PreDown         string
	PostDown        string
	PublicEndpoint  string

	// Advanced knobs (migrations 00005/00006)
	MaxClients           int
	TLSVersionMin        string
	TunMTU               int
	Sndbuf               int
	Rcvbuf               int
	ServerIPv6           string
	AuthUserPass         bool
	BridgeMode           bool
	BridgeGateway        string
	BridgePoolStart      string
	BridgePoolEnd        string
	BridgeNetmask        string
	TLSCipher            string
	TLSCiphersuites      string
	TLSGroups            string
	TLSCertProfile       string
	AuthUserPassVerify   string
	ScriptSecurity       int
	UsernameAsCommonName bool
	AuthUserPassFile     string
	IfconfigIPv6         string
	// Instance-level rate caps (bits/sec). See db.Instance for role semantics.
	BandwidthRxBps int64
	BandwidthTxBps int64

	// Automation flags
	IssueServerCert  bool   // after create: issue server cert from CA
	GenerateTLSCrypt bool   // with issue: also make tls-crypt
	CAName           string // CA for issue (default first)
	ServerCN         string // CN for server cert
	CreateCAIfEmpty  bool   // if no CA and IssueServerCert, create "default"
}

// Context is existing state for validation/auto.
type Context struct {
	ExistingNames    map[string]struct{}
	UsedPorts        map[int]struct{} // by proto key "udp/1194"
	UsedNetworks     []string         // CIDRs already taken
	DefaultBinary    string
	BinaryNames      map[string]struct{}
	HasCA            bool
	DefaultCA        string
}

// Result is a validated, default-filled instance row + post-create actions.
type Result struct {
	Instance db.Instance
	// Post-create
	IssueServerCert  bool
	GenerateTLSCrypt bool
	CAName           string
	ServerCN         string
	CreateCAIfEmpty  bool
	// What was auto-filled (for API response notes)
	Auto []string
}

// Prepare validates and applies defaults. Does not touch disk/DB.
func Prepare(in CreateInput, ctx Context) (Result, error) {
	var auto []string
	role := strings.ToLower(strings.TrimSpace(in.Role))
	if role == "" {
		role = "server"
		auto = append(auto, "role=server")
	}
	if role != "server" && role != "client" {
		return Result{}, fmt.Errorf("role must be server or client")
	}

	name := strings.TrimSpace(in.Name)
	if name == "" || netutil.IsAutoToken(name) {
		name = nextName(ctx.ExistingNames, "ovpn")
		auto = append(auto, "name="+name)
	}
	if err := ValidateName(name); err != nil {
		return Result{}, err
	}
	if _, taken := ctx.ExistingNames[name]; taken {
		return Result{}, fmt.Errorf("instance %q already exists", name)
	}

	proto := strings.ToLower(strings.TrimSpace(in.Proto))
	if proto == "" {
		proto = "udp"
		auto = append(auto, "proto=udp")
	}
	if err := ValidateProto(proto); err != nil {
		return Result{}, err
	}

	devType := strings.ToLower(strings.TrimSpace(in.DevType))
	if devType == "" {
		devType = "tun"
		auto = append(auto, "dev_type=tun")
	}
	if devType != "tun" && devType != "tap" {
		return Result{}, fmt.Errorf("dev_type must be tun or tap")
	}

	authMode := strings.ToLower(strings.TrimSpace(in.AuthMode))
	if authMode == "" {
		authMode = "pki"
		auto = append(auto, "auth_mode=pki")
	}
	if authMode != "pki" && authMode != "static_key" {
		return Result{}, fmt.Errorf("auth_mode must be pki or static_key")
	}

	topology := strings.ToLower(strings.TrimSpace(in.Topology))
	if role == "server" {
		if topology == "" {
			topology = "subnet"
			auto = append(auto, "topology=subnet")
		}
		if topology != "subnet" && topology != "net30" && topology != "p2p" {
			return Result{}, fmt.Errorf("topology must be subnet, net30, or p2p")
		}
	}

	port := in.Port
	if port == 0 {
		if role == "server" {
			port = nextPort(ctx.UsedPorts, proto, 1194)
			auto = append(auto, fmt.Sprintf("port=%d", port))
		}
		// client: port 0 means no lport / use remote ports only
	}
	if port != 0 {
		if port < 1 || port > 65535 {
			return Result{}, fmt.Errorf("port must be 1-65535")
		}
	}

	if in.LocalBind != "" {
		if ip := net.ParseIP(in.LocalBind); ip == nil {
			// allow hostnames? OpenVPN local is typically IP — allow hostname-like
			if strings.ContainsAny(in.LocalBind, " \t") {
				return Result{}, fmt.Errorf("invalid local_bind")
			}
		}
	}

	serverNetwork := strings.TrimSpace(in.ServerNetwork)
	if role == "server" {
		if serverNetwork == "" || netutil.IsAutoToken(serverNetwork) {
			serverNetwork = nextServerNetwork(ctx.UsedNetworks)
			auto = append(auto, "server_network="+serverNetwork)
		}
		if err := netutil.ValidateServerNetwork(serverNetwork); err != nil {
			return Result{}, err
		}
		// overlap check
		if err := checkNetworkOverlap(serverNetwork, ctx.UsedNetworks); err != nil {
			return Result{}, err
		}
	} else if serverNetwork != "" {
		return Result{}, fmt.Errorf("server_network only valid for server role")
	}

	if in.PoolStart != "" || in.PoolEnd != "" {
		if in.PoolStart == "" || in.PoolEnd == "" {
			return Result{}, fmt.Errorf("pool_start and pool_end must both be set")
		}
		if err := netutil.ValidateIP(in.PoolStart); err != nil {
			return Result{}, fmt.Errorf("pool_start: %w", err)
		}
		if err := netutil.ValidateIP(in.PoolEnd); err != nil {
			return Result{}, fmt.Errorf("pool_end: %w", err)
		}
	}

	remotes := in.Remotes
	if role == "client" {
		if len(remotes) == 0 {
			return Result{}, fmt.Errorf("client requires at least one remote (host:port)")
		}
		for i, r := range remotes {
			if strings.TrimSpace(r.Host) == "" {
				return Result{}, fmt.Errorf("remote[%d]: host required", i)
			}
			if r.Port == 0 {
				remotes[i].Port = 1194
			}
			if remotes[i].Port < 1 || remotes[i].Port > 65535 {
				return Result{}, fmt.Errorf("remote[%d]: invalid port", i)
			}
			if r.Proto != "" {
				if err := ValidateProto(r.Proto); err != nil {
					return Result{}, fmt.Errorf("remote[%d]: %w", i, err)
				}
			}
		}
	} else if len(remotes) > 0 {
		return Result{}, fmt.Errorf("remotes only valid for client role")
	}

	publicEP := strings.TrimSpace(in.PublicEndpoint)
	if publicEP != "" {
		if err := netutil.ValidatePublicEndpoint(publicEP); err != nil {
			return Result{}, fmt.Errorf("public_endpoint: %w", err)
		}
	}

	for i, d := range in.PushDNS {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if err := netutil.ValidateIP(d); err != nil {
			return Result{}, fmt.Errorf("push_dns[%d]: %w", i, err)
		}
	}
	for i, rt := range in.PushRoutes {
		rt = strings.TrimSpace(rt)
		if rt == "" {
			continue
		}
		if strings.Contains(rt, "/") {
			if err := netutil.ValidateCIDR(rt); err != nil {
				return Result{}, fmt.Errorf("push_routes[%d]: %w", i, err)
			}
		}
	}

	// Optional path checks (when provided)
	for label, p := range map[string]string{
		"pki_ca_path": pClean(in.PKICaPath), "pki_cert_path": pClean(in.PKICertPath),
		"pki_key_path": pClean(in.PKIKeyPath), "pki_tls_crypt_path": pClean(in.PKITLSCrypt),
		"pki_dh_path": pClean(in.PKIDHPath), "static_key_path": pClean(in.StaticKeyPath),
		"binary_path": pClean(in.BinaryPath),
	} {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			return Result{}, fmt.Errorf("%s must be absolute path", label)
		}
	}

	binary := strings.TrimSpace(in.BinaryName)
	if binary == "" {
		binary = ctx.DefaultBinary
		if binary == "" {
			binary = "default"
		}
		auto = append(auto, "binary_name="+binary)
	}
	if len(ctx.BinaryNames) > 0 {
		if _, ok := ctx.BinaryNames[binary]; !ok && in.BinaryPath == "" {
			return Result{}, fmt.Errorf("unknown binary_name %q (register it or set binary_path)", binary)
		}
	}

	// Sensible crypto defaults for new servers
	dataCiphers := strings.TrimSpace(in.DataCiphers)
	cipher := strings.TrimSpace(in.Cipher)
	authDigest := strings.TrimSpace(in.AuthDigest)
	if role == "server" && authMode == "pki" {
		if dataCiphers == "" && cipher == "" {
			dataCiphers = "AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305"
			auto = append(auto, "data_ciphers=modern")
		}
		if authDigest == "" {
			authDigest = "SHA256"
			auto = append(auto, "auth=SHA256")
		}
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	// Auto PKI for servers when leaf cert paths are not provided.
	issue := in.IssueServerCert
	genTC := in.GenerateTLSCrypt
	caName := strings.TrimSpace(in.CAName)
	serverCN := strings.TrimSpace(in.ServerCN)
	createCA := in.CreateCAIfEmpty
	noLeafPaths := pClean(in.PKICertPath) == "" && pClean(in.PKIKeyPath) == ""
	if role == "server" && authMode == "pki" && noLeafPaths {
		// Default: issue certs when a CA exists (or we will create one).
		if !issue && (ctx.HasCA || createCA) {
			issue = true
			auto = append(auto, "issue_server_cert=true")
		}
		if issue && !genTC && pClean(in.PKITLSCrypt) == "" {
			genTC = true
			auto = append(auto, "generate_tls_crypt=true")
		}
		if issue && caName == "" {
			if ctx.DefaultCA != "" {
				caName = ctx.DefaultCA
			} else if createCA {
				caName = "default"
				auto = append(auto, "create_ca=default")
			}
			if caName != "" && ctx.DefaultCA == caName {
				auto = append(auto, "ca_name="+caName)
			}
		}
		if issue && serverCN == "" {
			if publicEP != "" {
				serverCN = strings.Split(publicEP, ":")[0]
			} else {
				serverCN = name
			}
			auto = append(auto, "server_cn="+serverCN)
		}
	}

	if role == "server" && issue && caName == "" && !ctx.HasCA && !createCA {
		return Result{}, fmt.Errorf("issue_server_cert requires a CA — create one first or set create_ca_if_empty=true")
	}

	inst := db.Instance{
		Name: name, Role: role, Enabled: enabled,
		BinaryName: binary, BinaryPath: strings.TrimSpace(in.BinaryPath),
		DevType: devType, Device: strings.TrimSpace(in.Device),
		Proto: proto, LocalBind: strings.TrimSpace(in.LocalBind), Port: port,
		Remotes: remotes, ServerNetwork: serverNetwork, Topology: topology,
		PoolStart: strings.TrimSpace(in.PoolStart), PoolEnd: strings.TrimSpace(in.PoolEnd),
		AuthMode: authMode, Cipher: cipher, DataCiphers: dataCiphers, AuthDigest: authDigest,
		PushRoutes: cleanList(in.PushRoutes), PushDNS: cleanList(in.PushDNS),
		PushDomain: strings.TrimSpace(in.PushDomain), RedirectGateway: in.RedirectGateway,
		PKICaPath: pClean(in.PKICaPath), PKICertPath: pClean(in.PKICertPath),
		PKIKeyPath: pClean(in.PKIKeyPath), PKITLSCryptPath: pClean(in.PKITLSCrypt),
		PKIDHPath: pClean(in.PKIDHPath), StaticKeyPath: pClean(in.StaticKeyPath),
		ExtraDirectives: in.ExtraDirectives,
		Plugins: in.Plugins, EnvVars: in.EnvVars, FeatureSets: cleanList(in.FeatureSets),
		PreUp: in.PreUp, PostUp: in.PostUp, PreDown: in.PreDown, PostDown: in.PostDown,
		PublicEndpoint: publicEP,
		MaxClients: in.MaxClients, TLSVersionMin: strings.TrimSpace(in.TLSVersionMin),
		TunMTU: in.TunMTU, Sndbuf: in.Sndbuf, Rcvbuf: in.Rcvbuf,
		ServerIPv6: strings.TrimSpace(in.ServerIPv6), AuthUserPass: in.AuthUserPass,
		BridgeMode: in.BridgeMode, BridgeGateway: strings.TrimSpace(in.BridgeGateway),
		BridgePoolStart: strings.TrimSpace(in.BridgePoolStart), BridgePoolEnd: strings.TrimSpace(in.BridgePoolEnd),
		BridgeNetmask: strings.TrimSpace(in.BridgeNetmask),
		TLSCipher: strings.TrimSpace(in.TLSCipher), TLSCiphersuites: strings.TrimSpace(in.TLSCiphersuites),
		TLSGroups: strings.TrimSpace(in.TLSGroups), TLSCertProfile: strings.TrimSpace(in.TLSCertProfile),
		AuthUserPassVerify: strings.TrimSpace(in.AuthUserPassVerify), ScriptSecurity: in.ScriptSecurity,
		UsernameAsCommonName: in.UsernameAsCommonName,
		AuthUserPassFile:     strings.TrimSpace(in.AuthUserPassFile),
		IfconfigIPv6:         strings.TrimSpace(in.IfconfigIPv6),
		BandwidthRxBps:       in.BandwidthRxBps,
		BandwidthTxBps:       in.BandwidthTxBps,
	}
	if inst.BridgeMode {
		if inst.BridgeGateway == "" || inst.BridgeNetmask == "" ||
			inst.BridgePoolStart == "" || inst.BridgePoolEnd == "" {
			return Result{}, fmt.Errorf("bridge_mode requires bridge_gateway, bridge_netmask, bridge_pool_start, bridge_pool_end")
		}
		// TAP is typical for ethernet bridging; leave DevType as requested (tap still ok without bridge).
	}

	// Validate plugins
	for i, pl := range inst.Plugins {
		if pl.Path == "" {
			return Result{}, fmt.Errorf("plugins[%d]: path required", i)
		}
		if !filepath.IsAbs(pl.Path) {
			return Result{}, fmt.Errorf("plugins[%d]: path must be absolute", i)
		}
	}
	for i, e := range inst.EnvVars {
		if e.Name == "" || strings.ContainsAny(e.Name, "=\x00") {
			return Result{}, fmt.Errorf("env_vars[%d]: invalid name", i)
		}
	}

	return Result{
		Instance:         inst,
		IssueServerCert:  issue,
		GenerateTLSCrypt: genTC,
		CAName:           caName,
		ServerCN:         serverCN,
		CreateCAIfEmpty:  createCA,
		Auto:             auto,
	}, nil
}

// ValidateName checks instance name rules.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name required")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("name must match %s (letter first, alnum/_/-, max 32)", nameRe.String())
	}
	return nil
}

// ValidateProto checks OpenVPN proto values we support.
func ValidateProto(proto string) error {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "udp", "tcp", "udp4", "tcp4", "udp6", "tcp6":
		return nil
	default:
		return fmt.Errorf("proto %q invalid (udp|tcp|udp4|tcp4|udp6|tcp6)", proto)
	}
}

func nextName(existing map[string]struct{}, prefix string) string {
	if existing == nil {
		existing = map[string]struct{}{}
	}
	for i := 0; i < 1000; i++ {
		n := fmt.Sprintf("%s%d", prefix, i)
		if _, ok := existing[n]; !ok {
			return n
		}
	}
	return fmt.Sprintf("%s-%d", prefix, len(existing))
}

func nextPort(used map[int]struct{}, proto string, start int) int {
	_ = proto
	if used == nil {
		return start
	}
	for p := start; p < start+200; p++ {
		if _, ok := used[p]; !ok {
			return p
		}
	}
	return start
}

func nextServerNetwork(used []string) string {
	// Prefer 10.8.0.0/24, then 10.9.0.0/24 ... then 10.N.0.0/24
	candidates := []string{"10.8.0.0/24", "10.9.0.0/24", "10.10.0.0/24", "10.11.0.0/24"}
	for _, c := range candidates {
		if checkNetworkOverlap(c, used) == nil {
			return c
		}
	}
	for n := 20; n < 250; n++ {
		c := fmt.Sprintf("10.%d.0.0/24", n)
		if checkNetworkOverlap(c, used) == nil {
			return c
		}
	}
	return "10.200.0.0/24"
}

func checkNetworkOverlap(cidr string, used []string) error {
	_, a, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	for _, u := range used {
		_, b, err := net.ParseCIDR(u)
		if err != nil {
			continue
		}
		if networksOverlap(a, b) {
			return fmt.Errorf("server_network %s overlaps existing %s", cidr, u)
		}
	}
	return nil
}

func networksOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

func pClean(s string) string { return strings.TrimSpace(s) }

func cleanList(in []string) []string {
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ParseRemoteCSV parses "host:port" or "host:port:proto" CSV.
func ParseRemoteCSV(s string) ([]db.Remote, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []db.Remote
	for i, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// host:port or host:port:proto — last two numeric-ish
		host, port, proto, err := splitRemote(part)
		if err != nil {
			return nil, fmt.Errorf("remote[%d] %q: %w", i, part, err)
		}
		out = append(out, db.Remote{Host: host, Port: port, Proto: proto})
	}
	return out, nil
}

func splitRemote(s string) (host string, port int, proto string, err error) {
	// bracket IPv6 not fully handled — common case host:port
	if strings.Count(s, ":") == 0 {
		return s, 1194, "", nil
	}
	// try host:port
	h, p, e := net.SplitHostPort(s)
	if e == nil {
		port, err = strconv.Atoi(p)
		return h, port, "", err
	}
	// host:port:proto
	parts := strings.Split(s, ":")
	if len(parts) >= 2 {
		port, err = strconv.Atoi(parts[len(parts)-2])
		if err != nil {
			// last is port
			port, err = strconv.Atoi(parts[len(parts)-1])
			if err != nil {
				return "", 0, "", fmt.Errorf("need host:port")
			}
			return strings.Join(parts[:len(parts)-1], ":"), port, "", nil
		}
		proto = parts[len(parts)-1]
		host = strings.Join(parts[:len(parts)-2], ":")
		return host, port, proto, nil
	}
	return "", 0, "", fmt.Errorf("need host:port")
}
