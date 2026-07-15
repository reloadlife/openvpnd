package db

import "time"

// Binary is a named OpenVPN executable.
type Binary struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Version   string    `json:"version,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Remote is a client remote endpoint.
type Remote struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Proto string `json:"proto,omitempty"`
}

// Plugin is an OpenVPN --plugin module (path + args).
// Used for custom features (e.g. UDP stuffing plugin with a forked binary).
type Plugin struct {
	Path string   `json:"path"`
	Args []string `json:"args,omitempty"`
}

// EnvVar is injected into the openvpn process environment.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// FeaturePreset is a reusable extension bundle (plugins + directives + env).
type FeaturePreset struct {
	ID              string    `json:"id"`
	Description     string    `json:"description,omitempty"`
	ExtraDirectives string    `json:"extra_directives,omitempty"`
	Plugins         []Plugin  `json:"plugins,omitempty"`
	EnvVars         []EnvVar  `json:"env_vars,omitempty"`
	Notes           string    `json:"notes,omitempty"`
	Builtin         bool      `json:"builtin"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

// Instance is the desired OpenVPN process configuration.
type Instance struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Role            string   `json:"role"` // server | client
	Enabled         bool     `json:"enabled"`
	BinaryName      string   `json:"binary_name"`
	BinaryPath      string   `json:"binary_path,omitempty"`
	DevType         string   `json:"dev_type"`
	Device          string   `json:"device,omitempty"`
	Proto           string   `json:"proto"`
	LocalBind       string   `json:"local_bind,omitempty"`
	Port            int      `json:"port"`
	Remotes         []Remote `json:"remotes,omitempty"`
	ServerNetwork   string   `json:"server_network,omitempty"`
	Topology        string   `json:"topology,omitempty"`
	PoolStart       string   `json:"pool_start,omitempty"`
	PoolEnd         string   `json:"pool_end,omitempty"`
	AuthMode        string   `json:"auth_mode"`
	Cipher          string   `json:"cipher,omitempty"`
	DataCiphers     string   `json:"data_ciphers,omitempty"`
	AuthDigest      string   `json:"auth_digest,omitempty"`
	PushRoutes      []string `json:"push_routes,omitempty"`
	PushDNS         []string `json:"push_dns,omitempty"`
	PushDomain      string   `json:"push_domain,omitempty"`
	RedirectGateway bool     `json:"redirect_gateway"`
	PKICaPath       string   `json:"pki_ca_path,omitempty"`
	PKICertPath     string   `json:"pki_cert_path,omitempty"`
	PKIKeyPath      string   `json:"pki_key_path,omitempty"`
	PKITLSCryptPath string   `json:"pki_tls_crypt_path,omitempty"`
	PKIDHPath       string   `json:"pki_dh_path,omitempty"`
	PKICRLPath      string   `json:"pki_crl_path,omitempty"`
	StaticKeyPath   string   `json:"static_key_path,omitempty"`
	ExtraDirectives string   `json:"extra_directives,omitempty"`
	// Extensions for custom OpenVPN builds / plugins (UDP stuffing, etc.)
	Plugins     []Plugin `json:"plugins,omitempty"`
	EnvVars     []EnvVar `json:"env_vars,omitempty"`
	FeatureSets []string `json:"feature_sets,omitempty"` // preset IDs
	PreUp       string   `json:"pre_up,omitempty"`
	PostUp      string   `json:"post_up,omitempty"`
	PreDown     string   `json:"pre_down,omitempty"`
	PostDown    string   `json:"post_down,omitempty"`
	// Advanced OpenVPN knobs (migration 00005).
	MaxClients       int     `json:"max_clients,omitempty"`
	TLSVersionMin    string  `json:"tls_version_min,omitempty"`
	TunMTU           int     `json:"tun_mtu,omitempty"`
	Sndbuf           int     `json:"sndbuf,omitempty"`
	Rcvbuf           int     `json:"rcvbuf,omitempty"`
	ServerIPv6       string  `json:"server_ipv6,omitempty"`
	AuthUserPass     bool    `json:"auth_user_pass,omitempty"`
	// Roadmap knobs (migration 00006): bridge, TLS control-channel, server auth scripts, dual-stack.
	BridgeMode            bool   `json:"bridge_mode,omitempty"`
	BridgeGateway         string `json:"bridge_gateway,omitempty"`
	BridgePoolStart       string `json:"bridge_pool_start,omitempty"`
	BridgePoolEnd         string `json:"bridge_pool_end,omitempty"`
	BridgeNetmask         string `json:"bridge_netmask,omitempty"`
	TLSCipher             string `json:"tls_cipher,omitempty"`
	TLSCiphersuites       string `json:"tls_ciphersuites,omitempty"`
	TLSGroups             string `json:"tls_groups,omitempty"`
	TLSCertProfile        string `json:"tls_cert_profile,omitempty"`
	AuthUserPassVerify    string `json:"auth_user_pass_verify,omitempty"`
	ScriptSecurity        int    `json:"script_security,omitempty"`
	UsernameAsCommonName  bool   `json:"username_as_common_name,omitempty"`
	AuthUserPassFile      string `json:"auth_user_pass_file,omitempty"` // client: path after auth-user-pass
	IfconfigIPv6          string `json:"ifconfig_ipv6,omitempty"`
	// BandwidthRxBps / BandwidthTxBps are instance-level rate caps (bits/sec).
	// Role semantics differ:
	//   - client: whole-tunnel limit on Device (download/upload through the VPN)
	//   - server: optional device ceiling on the TUN (peer limits live on Client rows)
	BandwidthRxBps int64 `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps int64 `json:"bandwidth_tx_bps,omitempty"`
	ConfHash              string `json:"conf_hash,omitempty"`
	PID              int     `json:"pid,omitempty"`
	LastError        string  `json:"last_error,omitempty"`
	LastRxBytes      int64   `json:"last_rx_bytes"`
	LastTxBytes      int64   `json:"last_tx_bytes"`
	LastRxBps        float64 `json:"last_rx_bps"`
	LastTxBps        float64 `json:"last_tx_bps"`
	ConnectedClients int     `json:"connected_clients"`
	// PublicEndpoint is host or host:port advertised in client .ovpn profiles.
	PublicEndpoint string    `json:"public_endpoint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Client is a server-side client identity (CN-based).
type Client struct {
	ID                int64     `json:"id"`
	InstanceID        int64     `json:"instance_id"`
	InstanceName      string    `json:"instance_name,omitempty"`
	CommonName        string    `json:"common_name"`
	Name              string    `json:"name"`
	Notes             string    `json:"notes"`
	StaticIP          string    `json:"static_ip,omitempty"`
	PushRoutes        []string  `json:"push_routes,omitempty"`
	IRoutes           []string  `json:"iroutes,omitempty"` // client-side subnets (CCD iroute)
	// Per-client push overrides (migration 00006).
	PushDNS         []string `json:"push_dns,omitempty"`
	PushDomain      string   `json:"push_domain,omitempty"`
	RedirectGateway bool     `json:"redirect_gateway,omitempty"`
	DisablePush     []string `json:"disable_push,omitempty"` // OpenVPN 2.5+ push-remove names
	Suspended         bool      `json:"suspended"`
	TrafficLimitBytes int64     `json:"traffic_limit_bytes"`
	// Bandwidth*Bps are per-peer rate caps (bits/sec). When BandwidthTotalBps > 0,
	// both directions are shaped at Total (see policy.EffectiveBandwidth).
	BandwidthRxBps    int64     `json:"bandwidth_rx_bps"`
	BandwidthTxBps    int64     `json:"bandwidth_tx_bps"`
	BandwidthTotalBps int64     `json:"bandwidth_total_bps"`
	// ExpiresAt: zero = never; when past, reconciler auto-suspends the peer.
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	CertRef           string    `json:"cert_ref,omitempty"`
	ClientCertPath    string    `json:"client_cert_path,omitempty"`
	ClientKeyPath     string    `json:"client_key_path,omitempty"`
	RxBytesOffset     int64     `json:"-"`
	TxBytesOffset     int64     `json:"-"`
	RealAddress       string    `json:"real_address,omitempty"`
	VirtualAddress    string    `json:"virtual_address,omitempty"`
	ConnectedSince    string    `json:"connected_since,omitempty"`
	LastRxBytes       int64     `json:"last_rx_bytes"`
	LastTxBytes       int64     `json:"last_tx_bytes"`
	LastRxBps         float64   `json:"last_rx_bps"`
	LastTxBps         float64   `json:"last_tx_bps"`
	Tags              []string  `json:"tags,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// EffectiveRx returns user-visible received bytes after soft reset.
func (c Client) EffectiveRx() int64 {
	v := c.LastRxBytes - c.RxBytesOffset
	if v < 0 {
		return 0
	}
	return v
}

// EffectiveTx returns user-visible transmitted bytes after soft reset.
func (c Client) EffectiveTx() int64 {
	v := c.LastTxBytes - c.TxBytesOffset
	if v < 0 {
		return 0
	}
	return v
}

// Event is an audit or enforcement record.
type Event struct {
	ID       int64     `json:"id"`
	TS       time.Time `json:"ts"`
	Level    string    `json:"level"`
	Kind     string    `json:"kind"`
	Instance string    `json:"instance,omitempty"`
	ClientCN string    `json:"client_cn,omitempty"`
	Message  string    `json:"message"`
	Meta     string    `json:"meta,omitempty"`
}

// TrafficSample is a rate/counter sample for a client.
type TrafficSample struct {
	ID        int64
	ClientID  int64
	SampledAt time.Time
	RxBytes   int64
	TxBytes   int64
	RxBps     float64
	TxBps     float64
}
