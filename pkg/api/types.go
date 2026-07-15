package api

import "time"

// VersionInfo is returned by /v1/version.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// DaemonConfig is non-secret runtime config.
type DaemonConfig struct {
	HTTPListen         string `json:"http_listen"`
	UnixListen         string `json:"unix_listen,omitempty"`
	MetricsListen      string `json:"metrics_listen,omitempty"`
	SNMPEnabled        bool   `json:"snmp_enabled"`
	SNMPListen         string `json:"snmp_listen,omitempty"`
	Persistence        string `json:"persistence"`
	ConfDir            string `json:"conf_dir"`
	RuntimeDir         string `json:"runtime_dir"`
	PKIDir             string `json:"pki_dir"`
	DefaultBinary      string `json:"default_binary"`
	SampleInterval     string `json:"sample_interval"`
	ReconcileInterval  string `json:"reconcile_interval"`
	AllowHooks         bool   `json:"allow_hooks"`
	DBPath             string `json:"db_path"`
	TimeseriesPath     string `json:"timeseries_path,omitempty"`
	PublicBaseURL      string `json:"public_base_url,omitempty"`
	ProfileLinkTTL     string `json:"profile_link_ttl,omitempty"`
	ProfileLinkMaxUses int    `json:"profile_link_max_uses,omitempty"`
	ReadOnly           bool   `json:"read_only"`
	Production         bool   `json:"production"`
	// Role is the authenticated caller's API role (admin|operator|readonly).
	Role string `json:"role,omitempty"`
}

// SystemInfo is non-secret daemon runtime summary (GET /v1/system/info).
type SystemInfo struct {
	Version        string `json:"version"`
	Commit         string `json:"commit,omitempty"`
	Date           string `json:"date,omitempty"`
	Production     bool   `json:"production"`
	BandwidthMode  string `json:"bandwidth_mode"`
	Backend        string `json:"backend"` // mock | host
	ListenHTTP     string `json:"listen_http,omitempty"`
	ListenUnix     string `json:"listen_unix,omitempty"`
	ListenMetrics  string `json:"listen_metrics,omitempty"`
	ReadOnly       bool   `json:"read_only"`
	InstancesTotal *int   `json:"instances_total,omitempty"`
	InstancesUp    *int   `json:"instances_up,omitempty"`

	// Optional chrome fields (TUI / operators)
	Status   string `json:"status,omitempty"` // ok | ready | degraded
	Hostname string `json:"hostname,omitempty"`
	Uptime   string `json:"uptime,omitempty"`

	// Paths (non-secret layout)
	DBPath         string `json:"db_path,omitempty"`
	TimeseriesPath string `json:"timeseries_path,omitempty"`
	PKIDir         string `json:"pki_dir,omitempty"`
	ConfDir        string `json:"conf_dir,omitempty"`
	RuntimeDir     string `json:"runtime_dir,omitempty"`
	Persistence    string `json:"persistence,omitempty"`
	PublicBaseURL  string `json:"public_base_url,omitempty"`

	// Ready is existence / ping bits only (no secrets).
	Ready SystemReady `json:"ready"`
}

// SystemReady reports non-secret readiness of on-disk layout and DB.
type SystemReady struct {
	DB           bool `json:"db"`
	StateDB      bool `json:"state_db"`
	TimeseriesDB bool `json:"timeseries_db,omitempty"`
	PKIDir       bool `json:"pki_dir"`
	ConfDir      bool `json:"conf_dir"`
}

// SystemBackupRequest is the body for POST /v1/system/backup.
// Prefer path (write on host); empty path streams the archive in the response body.
type SystemBackupRequest struct {
	Path string `json:"path,omitempty"`
}

// SystemBackupResponse is returned when backup is written to a host path.
type SystemBackupResponse struct {
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	Version string `json:"version,omitempty"`
	Host    string `json:"host,omitempty"`
	TS      string `json:"timestamp,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// Binary is a named OpenVPN executable.
type Binary struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Version   string    `json:"version,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BinaryCreateRequest registers a binary.
type BinaryCreateRequest struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Notes string `json:"notes,omitempty"`
}

// Remote is a client remote endpoint.
type Remote struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Proto string `json:"proto,omitempty"`
}

// InstanceCreateRequest creates an OpenVPN instance.
//
// Smart defaults when fields are empty / "auto":
//   - name → ovpn0, ovpn1, …
//   - server_network → free 10.x.0.0/24
//   - port → next free from 1194
//   - proto/topology/dev_type/auth_mode → udp / subnet / tun / pki
//   - modern data-ciphers + auth SHA256 for servers
//   - issue_server_cert + tls-crypt when PKI paths empty and a CA exists
//
// Set create_ca_if_empty=true to mint CA "default" when none exists.
type InstanceCreateRequest struct {
	Name            string   `json:"name"`
	Role            string   `json:"role"` // server | client (default server)
	Enabled         *bool    `json:"enabled,omitempty"`
	BinaryName      string   `json:"binary_name,omitempty"`
	BinaryPath      string   `json:"binary_path,omitempty"`
	DevType         string   `json:"dev_type,omitempty"`
	Device          string   `json:"device,omitempty"`
	Proto           string   `json:"proto,omitempty"`
	LocalBind       string   `json:"local_bind,omitempty"`
	Port            int      `json:"port,omitempty"`
	Remotes         []Remote `json:"remotes,omitempty"`
	Remote          string   `json:"remote,omitempty"` // shorthand host or host:port for client
	ServerNetwork   string   `json:"server_network,omitempty"`
	Topology        string   `json:"topology,omitempty"`
	PoolStart       string   `json:"pool_start,omitempty"`
	PoolEnd         string   `json:"pool_end,omitempty"`
	AuthMode        string   `json:"auth_mode,omitempty"`
	Cipher          string   `json:"cipher,omitempty"`
	DataCiphers     string   `json:"data_ciphers,omitempty"`
	AuthDigest      string   `json:"auth_digest,omitempty"`
	PushRoutes      []string `json:"push_routes,omitempty"`
	PushDNS         []string `json:"push_dns,omitempty"`
	PushDomain      string   `json:"push_domain,omitempty"`
	RedirectGateway bool     `json:"redirect_gateway,omitempty"`
	PKICaPath       string   `json:"pki_ca_path,omitempty"`
	PKICertPath     string   `json:"pki_cert_path,omitempty"`
	PKIKeyPath      string   `json:"pki_key_path,omitempty"`
	PKITLSCryptPath string   `json:"pki_tls_crypt_path,omitempty"`
	PKIDHPath       string   `json:"pki_dh_path,omitempty"`
	StaticKeyPath   string   `json:"static_key_path,omitempty"`
	ExtraDirectives string   `json:"extra_directives,omitempty"`
	// Extensions: custom OpenVPN builds / plugins (UDP stuffing, etc.)
	Plugins        []Plugin `json:"plugins,omitempty"`
	EnvVars        []EnvVar `json:"env_vars,omitempty"`
	FeatureSets    []string `json:"feature_sets,omitempty"`
	PreUp          string   `json:"pre_up,omitempty"`
	PostUp         string   `json:"post_up,omitempty"`
	PreDown        string   `json:"pre_down,omitempty"`
	PostDown       string   `json:"post_down,omitempty"`
	PublicEndpoint string   `json:"public_endpoint,omitempty"` // host or host:port for client profiles

	// Advanced knobs (migrations 00005/00006)
	MaxClients           int    `json:"max_clients,omitempty"`
	TLSVersionMin        string `json:"tls_version_min,omitempty"`
	TunMTU               int    `json:"tun_mtu,omitempty"`
	Sndbuf               int    `json:"sndbuf,omitempty"`
	Rcvbuf               int    `json:"rcvbuf,omitempty"`
	ServerIPv6           string `json:"server_ipv6,omitempty"`
	AuthUserPass         bool   `json:"auth_user_pass,omitempty"`
	BridgeMode           bool   `json:"bridge_mode,omitempty"`
	BridgeGateway        string `json:"bridge_gateway,omitempty"`
	BridgePoolStart      string `json:"bridge_pool_start,omitempty"`
	BridgePoolEnd        string `json:"bridge_pool_end,omitempty"`
	BridgeNetmask        string `json:"bridge_netmask,omitempty"`
	TLSCipher            string `json:"tls_cipher,omitempty"`
	TLSCiphersuites      string `json:"tls_ciphersuites,omitempty"`
	TLSGroups            string `json:"tls_groups,omitempty"`
	TLSCertProfile       string `json:"tls_cert_profile,omitempty"`
	AuthUserPassVerify   string `json:"auth_user_pass_verify,omitempty"`
	ScriptSecurity       int    `json:"script_security,omitempty"`
	UsernameAsCommonName bool   `json:"username_as_common_name,omitempty"`
	AuthUserPassFile     string `json:"auth_user_pass_file,omitempty"`
	IfconfigIPv6         string `json:"ifconfig_ipv6,omitempty"`
	// Instance-level rate caps (bits/sec). Semantics by role:
	//   client → whole-tunnel limit on Device; server → optional TUN ceiling
	//   (per-peer limits are on ClientCreate/Update, not here).
	BandwidthRxBps int64 `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps int64 `json:"bandwidth_tx_bps,omitempty"`

	// Automation (server mTLS)
	IssueServerCert  *bool  `json:"issue_server_cert,omitempty"`  // default true when cert paths empty + CA available
	GenerateTLSCrypt *bool  `json:"generate_tls_crypt,omitempty"` // default true when issuing
	CreateCAIfEmpty  bool   `json:"create_ca_if_empty,omitempty"` // create CA "default" if none
	CAName           string `json:"ca_name,omitempty"`
	ServerCN         string `json:"server_cn,omitempty"`
}

// Plugin is --plugin path + args.
type Plugin struct {
	Path string   `json:"path"`
	Args []string `json:"args,omitempty"`
}

// EnvVar is process environment for openvpn.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// FeaturePreset is a reusable extension bundle.
type FeaturePreset struct {
	ID              string   `json:"id"`
	Description     string   `json:"description,omitempty"`
	ExtraDirectives string   `json:"extra_directives,omitempty"`
	Plugins         []Plugin `json:"plugins,omitempty"`
	EnvVars         []EnvVar `json:"env_vars,omitempty"`
	Notes           string   `json:"notes,omitempty"`
	Builtin         bool     `json:"builtin"`
}

// InstanceCreateResponse is the created instance plus what was auto-filled.
type InstanceCreateResponse struct {
	Instance
	AutoFilled []string `json:"auto_filled,omitempty"`
}

// ImportInstanceRequest adopts an existing OpenVPN .conf / .ovpn.
//
// Create: nil/true → create instance; false → parse preview only.
type ImportInstanceRequest struct {
	Name           string `json:"name,omitempty"`
	Content        string `json:"content"`
	Enabled        *bool  `json:"enabled,omitempty"`
	Create         *bool  `json:"create,omitempty"`
	BinaryName     string `json:"binary_name,omitempty"`
	PublicEndpoint string `json:"public_endpoint,omitempty"`
	// Role hint when conf is ambiguous: server | client
	Role string `json:"role,omitempty"`
	// SourcePath optional original path for operator notes.
	SourcePath string `json:"source_path,omitempty"`
}

// ImportInstanceResponse is the parse result, optional created instance, and warnings.
type ImportInstanceResponse struct {
	Instance   *Instance             `json:"instance,omitempty"`
	Parsed     InstanceCreateRequest `json:"parsed"`
	Warnings   []string              `json:"warnings,omitempty"`
	AutoFilled []string              `json:"auto_filled,omitempty"`
	Created    bool                  `json:"created"`
}

// AdoptInstanceRequest registers an on-disk OpenVPN conf as a managed instance.
//
// Unlike import (which takes conf content in the body), adopt reads conf_path
// from the daemon host filesystem. When take_over is true and
// openvpn.adopt_takeover_enabled is set (default), a live openvpn PID is
// stopped (SIGTERM, then SIGKILL after 5s) after create, the instance is
// enabled, and reconcile is forced. openvpnd/openvpnctl PIDs are never killed.
// Failures to stop are soft-failed into response notes; create still succeeds.
type AdoptInstanceRequest struct {
	ConfPath       string `json:"conf_path"`
	Name           string `json:"name,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	BinaryName     string `json:"binary_name,omitempty"`
	// BinaryPath is absolute path to the openvpn binary discovered on the host
	// (e.g. /usr/bin/openvpn-linux). Registered automatically when set.
	BinaryPath     string `json:"binary_path,omitempty"`
	TakeOver       bool   `json:"take_over,omitempty"`
	PublicEndpoint string `json:"public_endpoint,omitempty"`
	// PID is optional (e.g. from discover). With take_over=true it is the process
	// to stop after double-checking /proc/<pid>/cmdline is openvpn.
	PID int `json:"pid,omitempty"`
}

// AdoptInstanceResponse is the create result from adopt.
type AdoptInstanceResponse struct {
	Instance   *Instance             `json:"instance,omitempty"`
	Parsed     InstanceCreateRequest `json:"parsed"`
	Warnings   []string              `json:"warnings,omitempty"`
	AutoFilled []string              `json:"auto_filled,omitempty"`
	Notes      []string              `json:"notes,omitempty"`
	ConfPath   string                `json:"conf_path"`
	PID        int                   `json:"pid,omitempty"`
}

// OpenVPNCandidate is a running openvpn process discovered on the daemon host.
type OpenVPNCandidate struct {
	PID      int    `json:"pid"`
	ConfPath string `json:"conf_path,omitempty"`
	Cmdline  string `json:"cmdline"`
	Binary   string `json:"binary"`
}

// InstanceUpdateRequest patches an instance.
type InstanceUpdateRequest struct {
	Enabled         *bool    `json:"enabled,omitempty"`
	BinaryName      *string  `json:"binary_name,omitempty"`
	BinaryPath      *string  `json:"binary_path,omitempty"`
	DevType         *string  `json:"dev_type,omitempty"`
	Device          *string  `json:"device,omitempty"`
	Proto           *string  `json:"proto,omitempty"`
	LocalBind       *string  `json:"local_bind,omitempty"`
	Port            *int     `json:"port,omitempty"`
	Remotes         []Remote `json:"remotes,omitempty"`
	ServerNetwork   *string  `json:"server_network,omitempty"`
	Topology        *string  `json:"topology,omitempty"`
	PoolStart       *string  `json:"pool_start,omitempty"`
	PoolEnd         *string  `json:"pool_end,omitempty"`
	AuthMode        *string  `json:"auth_mode,omitempty"`
	Cipher          *string  `json:"cipher,omitempty"`
	DataCiphers     *string  `json:"data_ciphers,omitempty"`
	AuthDigest      *string  `json:"auth_digest,omitempty"`
	PushRoutes      []string `json:"push_routes,omitempty"`
	PushDNS         []string `json:"push_dns,omitempty"`
	PushDomain      *string  `json:"push_domain,omitempty"`
	RedirectGateway *bool    `json:"redirect_gateway,omitempty"`
	PKICaPath       *string  `json:"pki_ca_path,omitempty"`
	PKICertPath     *string  `json:"pki_cert_path,omitempty"`
	PKIKeyPath      *string  `json:"pki_key_path,omitempty"`
	PKITLSCryptPath *string  `json:"pki_tls_crypt_path,omitempty"`
	PKIDHPath       *string  `json:"pki_dh_path,omitempty"`
	StaticKeyPath   *string  `json:"static_key_path,omitempty"`
	ExtraDirectives *string  `json:"extra_directives,omitempty"`
	Plugins         []Plugin `json:"plugins,omitempty"`
	EnvVars         []EnvVar `json:"env_vars,omitempty"`
	FeatureSets     []string `json:"feature_sets,omitempty"`
	PreUp           *string  `json:"pre_up,omitempty"`
	PostUp          *string  `json:"post_up,omitempty"`
	PreDown         *string  `json:"pre_down,omitempty"`
	PostDown        *string  `json:"post_down,omitempty"`
	PublicEndpoint  *string  `json:"public_endpoint,omitempty"`

	MaxClients           *int    `json:"max_clients,omitempty"`
	TLSVersionMin        *string `json:"tls_version_min,omitempty"`
	TunMTU               *int    `json:"tun_mtu,omitempty"`
	Sndbuf               *int    `json:"sndbuf,omitempty"`
	Rcvbuf               *int    `json:"rcvbuf,omitempty"`
	ServerIPv6           *string `json:"server_ipv6,omitempty"`
	AuthUserPass         *bool   `json:"auth_user_pass,omitempty"`
	BridgeMode           *bool   `json:"bridge_mode,omitempty"`
	BridgeGateway        *string `json:"bridge_gateway,omitempty"`
	BridgePoolStart      *string `json:"bridge_pool_start,omitempty"`
	BridgePoolEnd        *string `json:"bridge_pool_end,omitempty"`
	BridgeNetmask        *string `json:"bridge_netmask,omitempty"`
	TLSCipher            *string `json:"tls_cipher,omitempty"`
	TLSCiphersuites      *string `json:"tls_ciphersuites,omitempty"`
	TLSGroups            *string `json:"tls_groups,omitempty"`
	TLSCertProfile       *string `json:"tls_cert_profile,omitempty"`
	AuthUserPassVerify   *string `json:"auth_user_pass_verify,omitempty"`
	ScriptSecurity       *int    `json:"script_security,omitempty"`
	UsernameAsCommonName *bool   `json:"username_as_common_name,omitempty"`
	AuthUserPassFile     *string `json:"auth_user_pass_file,omitempty"`
	IfconfigIPv6         *string `json:"ifconfig_ipv6,omitempty"`
	// Instance-level rate caps (bits/sec). See InstanceCreateRequest for role semantics.
	BandwidthRxBps *int64 `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps *int64 `json:"bandwidth_tx_bps,omitempty"`
}

// MgmtCommandRequest is a whitelisted OpenVPN management interface command.
//
// Allowed commands: status, kill, signal, hold, log, state, bytecount, pid, version.
// kill requires args[0] (CN or IP:port). signal requires args[0] (e.g. SIGUSR1, SIGHUP, SIGTERM).
type MgmtCommandRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// MgmtCommandResponse is the raw management interface reply body.
type MgmtCommandResponse struct {
	Output string `json:"output"`
}

// InstanceStatus is live process/client status from the management interface (when up).
type InstanceStatus struct {
	Name             string                 `json:"name"`
	Up               bool                   `json:"up"`
	PID              int                    `json:"pid,omitempty"`
	RxBytes          int64                  `json:"rx_bytes"`
	TxBytes          int64                  `json:"tx_bytes"`
	ConnectedClients int                    `json:"connected_clients"`
	Clients          []InstanceStatusClient `json:"clients,omitempty"`
	UpdatedAt        time.Time              `json:"updated_at,omitempty"`
	// Error is set when the instance exists but management is unreachable / not running.
	Error string `json:"error,omitempty"`
}

// InstanceStatusClient is one connected peer from management status.
type InstanceStatusClient struct {
	CommonName     string    `json:"common_name"`
	RealAddress    string    `json:"real_address,omitempty"`
	VirtualAddress string    `json:"virtual_address,omitempty"`
	ConnectedSince time.Time `json:"connected_since,omitempty"`
	RxBytes        int64     `json:"rx_bytes"`
	TxBytes        int64     `json:"tx_bytes"`
}

// Instance is the API representation.
type Instance struct {
	ID                   int64     `json:"id"`
	Name                 string    `json:"name"`
	Role                 string    `json:"role"`
	Enabled              bool      `json:"enabled"`
	Up                   bool      `json:"up"`
	BinaryName           string    `json:"binary_name"`
	BinaryPath           string    `json:"binary_path,omitempty"`
	DevType              string    `json:"dev_type"`
	Device               string    `json:"device,omitempty"`
	Proto                string    `json:"proto"`
	LocalBind            string    `json:"local_bind,omitempty"`
	Port                 int       `json:"port"`
	Remotes              []Remote  `json:"remotes,omitempty"`
	ServerNetwork        string    `json:"server_network,omitempty"`
	Topology             string    `json:"topology,omitempty"`
	PoolStart            string    `json:"pool_start,omitempty"`
	PoolEnd              string    `json:"pool_end,omitempty"`
	AuthMode             string    `json:"auth_mode"`
	Cipher               string    `json:"cipher,omitempty"`
	DataCiphers          string    `json:"data_ciphers,omitempty"`
	AuthDigest           string    `json:"auth_digest,omitempty"`
	PushRoutes           []string  `json:"push_routes,omitempty"`
	PushDNS              []string  `json:"push_dns,omitempty"`
	PushDomain           string    `json:"push_domain,omitempty"`
	RedirectGateway      bool      `json:"redirect_gateway"`
	PKICaPath            string    `json:"pki_ca_path,omitempty"`
	PKICertPath          string    `json:"pki_cert_path,omitempty"`
	PKIKeyPath           string    `json:"pki_key_path,omitempty"`
	PKITLSCryptPath      string    `json:"pki_tls_crypt_path,omitempty"`
	PKIDHPath            string    `json:"pki_dh_path,omitempty"`
	PKICRLPath           string    `json:"pki_crl_path,omitempty"`
	StaticKeyPath        string    `json:"static_key_path,omitempty"`
	ExtraDirectives      string    `json:"extra_directives,omitempty"`
	Plugins              []Plugin  `json:"plugins,omitempty"`
	EnvVars              []EnvVar  `json:"env_vars,omitempty"`
	FeatureSets          []string  `json:"feature_sets,omitempty"`
	PreUp                string    `json:"pre_up,omitempty"`
	PostUp               string    `json:"post_up,omitempty"`
	PreDown              string    `json:"pre_down,omitempty"`
	PostDown             string    `json:"post_down,omitempty"`
	MaxClients           int       `json:"max_clients,omitempty"`
	TLSVersionMin        string    `json:"tls_version_min,omitempty"`
	TunMTU               int       `json:"tun_mtu,omitempty"`
	Sndbuf               int       `json:"sndbuf,omitempty"`
	Rcvbuf               int       `json:"rcvbuf,omitempty"`
	ServerIPv6           string    `json:"server_ipv6,omitempty"`
	AuthUserPass         bool      `json:"auth_user_pass,omitempty"`
	BridgeMode           bool      `json:"bridge_mode,omitempty"`
	BridgeGateway        string    `json:"bridge_gateway,omitempty"`
	BridgePoolStart      string    `json:"bridge_pool_start,omitempty"`
	BridgePoolEnd        string    `json:"bridge_pool_end,omitempty"`
	BridgeNetmask        string    `json:"bridge_netmask,omitempty"`
	TLSCipher            string    `json:"tls_cipher,omitempty"`
	TLSCiphersuites      string    `json:"tls_ciphersuites,omitempty"`
	TLSGroups            string    `json:"tls_groups,omitempty"`
	TLSCertProfile       string    `json:"tls_cert_profile,omitempty"`
	AuthUserPassVerify   string    `json:"auth_user_pass_verify,omitempty"`
	ScriptSecurity       int       `json:"script_security,omitempty"`
	UsernameAsCommonName bool      `json:"username_as_common_name,omitempty"`
	AuthUserPassFile     string    `json:"auth_user_pass_file,omitempty"`
	IfconfigIPv6         string    `json:"ifconfig_ipv6,omitempty"`
	// Instance-level rate caps (bits/sec). Role semantics: client = whole tunnel;
	// server = optional device ceiling (peers use Client.bandwidth_*).
	BandwidthRxBps       int64     `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps       int64     `json:"bandwidth_tx_bps,omitempty"`
	PublicEndpoint       string    `json:"public_endpoint,omitempty"`
	PID                  int       `json:"pid,omitempty"`
	LastError            string    `json:"last_error,omitempty"`
	ConnectedClients     int       `json:"connected_clients"`
	RxBytes              int64     `json:"rx_bytes"`
	TxBytes              int64     `json:"tx_bytes"`
	RxBps                float64   `json:"rx_bps"`
	TxBps                float64   `json:"tx_bps"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ClientCreateRequest creates a server client (VPN user).
//
// Happy path: only common_name is required. Empty static_ip → pool allocate.
// issue_cert defaults to true when cert paths are empty and a CA exists.
// mint_profile_link=true returns a one-click download / OpenVPN Connect URL.
type ClientCreateRequest struct {
	CommonName        string   `json:"common_name"`
	Name              string   `json:"name,omitempty"`
	Notes             string   `json:"notes,omitempty"`
	StaticIP          string   `json:"static_ip,omitempty"` // empty/auto → allocate
	PushRoutes        []string `json:"push_routes,omitempty"`
	IRoutes           []string `json:"iroutes,omitempty"` // subnets behind client (CCD iroute)
	PushDNS           []string `json:"push_dns,omitempty"`
	PushDomain        string   `json:"push_domain,omitempty"`
	RedirectGateway   bool     `json:"redirect_gateway,omitempty"`
	DisablePush       []string `json:"disable_push,omitempty"`
	Suspended         bool   `json:"suspended,omitempty"`
	TrafficLimitBytes int64  `json:"traffic_limit_bytes,omitempty"`
	BandwidthRxBps    int64  `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps    int64  `json:"bandwidth_tx_bps,omitempty"`
	BandwidthTotalBps int64  `json:"bandwidth_total_bps,omitempty"` // when >0, both directions = total
	ExpiresAt         string `json:"expires_at,omitempty"`         // RFC3339 UTC; empty = never
	CertRef           string `json:"cert_ref,omitempty"`
	ClientCertPath    string `json:"client_cert_path,omitempty"`
	ClientKeyPath     string `json:"client_key_path,omitempty"`
	// IssueCert: nil = auto (true when no cert paths + CA available).
	IssueCert *bool  `json:"issue_cert,omitempty"`
	CAName    string `json:"ca_name,omitempty"`
	// MintProfileLink mints a presigned .ovpn / openvpn://import-profile/ link.
	MintProfileLink    bool     `json:"mint_profile_link,omitempty"`
	ProfileLinkTTL     string   `json:"profile_link_ttl,omitempty"` // e.g. "24h"
	ProfileLinkMaxUses *int     `json:"profile_link_max_uses,omitempty"`
	ProfileLinkNote    string   `json:"profile_link_note,omitempty"`
	Tags               []string `json:"tags,omitempty"`
}

// ClientCreateResponse is the created client plus optional one-click profile.
type ClientCreateResponse struct {
	ServerClient
	AutoFilled  []string     `json:"auto_filled,omitempty"`
	ProfileLink *ProfileLink `json:"profile_link,omitempty"`
	Warnings    []string     `json:"warnings,omitempty"`
}

// ClientUpdateRequest patches a client.
type ClientUpdateRequest struct {
	Name              *string  `json:"name,omitempty"`
	Notes             *string  `json:"notes,omitempty"`
	StaticIP          *string  `json:"static_ip,omitempty"`
	PushRoutes        []string `json:"push_routes,omitempty"`
	IRoutes           []string `json:"iroutes,omitempty"`
	PushDNS           []string `json:"push_dns,omitempty"`
	PushDomain        *string  `json:"push_domain,omitempty"`
	RedirectGateway   *bool    `json:"redirect_gateway,omitempty"`
	DisablePush       []string `json:"disable_push,omitempty"`
	Suspended         *bool    `json:"suspended,omitempty"`
	TrafficLimitBytes *int64   `json:"traffic_limit_bytes,omitempty"`
	BandwidthRxBps    *int64   `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps    *int64   `json:"bandwidth_tx_bps,omitempty"`
	BandwidthTotalBps *int64   `json:"bandwidth_total_bps,omitempty"`
	ExpiresAt         *string  `json:"expires_at,omitempty"` // RFC3339 UTC; "" clears
	CertRef           *string  `json:"cert_ref,omitempty"`
	ClientCertPath    *string  `json:"client_cert_path,omitempty"`
	ClientKeyPath     *string  `json:"client_key_path,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

// ServerClient is the API representation of a server-side VPN client (CN).
type ServerClient struct {
	ID                int64     `json:"id"`
	InstanceID        int64     `json:"instance_id"`
	InstanceName      string    `json:"instance_name,omitempty"`
	CommonName        string    `json:"common_name"`
	Name              string    `json:"name"`
	Notes             string    `json:"notes"`
	StaticIP          string    `json:"static_ip,omitempty"`
	PushRoutes        []string  `json:"push_routes,omitempty"`
	IRoutes           []string  `json:"iroutes,omitempty"`
	PushDNS           []string  `json:"push_dns,omitempty"`
	PushDomain        string    `json:"push_domain,omitempty"`
	RedirectGateway   bool      `json:"redirect_gateway,omitempty"`
	DisablePush       []string  `json:"disable_push,omitempty"`
	Suspended         bool      `json:"suspended"`
	Connected         bool      `json:"connected"`
	TrafficLimitBytes int64     `json:"traffic_limit_bytes"`
	BandwidthRxBps    int64     `json:"bandwidth_rx_bps"`
	BandwidthTxBps    int64     `json:"bandwidth_tx_bps"`
	BandwidthTotalBps int64     `json:"bandwidth_total_bps"`
	ExpiresAt         string    `json:"expires_at,omitempty"` // RFC3339 UTC; empty = never
	CertRef           string    `json:"cert_ref,omitempty"`
	ClientCertPath    string    `json:"client_cert_path,omitempty"`
	ClientKeyPath     string    `json:"client_key_path,omitempty"`
	RealAddress       string    `json:"real_address,omitempty"`
	VirtualAddress    string    `json:"virtual_address,omitempty"`
	ConnectedSince    string    `json:"connected_since,omitempty"`
	RxBytes           int64     `json:"rx_bytes"`
	TxBytes           int64     `json:"tx_bytes"`
	RxBps             float64   `json:"rx_bps"`
	TxBps             float64   `json:"tx_bps"`
	Tags              []string  `json:"tags,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ProfileLinkRequest mints a presigned profile download link.
type ProfileLinkRequest struct {
	TTL     string `json:"ttl,omitempty"`      // Go duration, e.g. "24h", "15m"
	MaxUses *int   `json:"max_uses,omitempty"` // default from daemon; 0=unlimited until expiry
	Note    string `json:"note,omitempty"`
}

// ProfileLink is a one-click install payload for users / OpenVPN Connect.
type ProfileLink struct {
	Token       string    `json:"token"`
	DownloadURL string    `json:"download_url"` // raw HTTPS URL → .ovpn
	ImportURL   string    `json:"import_url"`   // openvpn://import-profile/<download_url>
	ExpiresAt   time.Time `json:"expires_at"`
	MaxUses     int       `json:"max_uses"`
	UseCount    int       `json:"use_count"`
	Note        string    `json:"note,omitempty"`
	Instance    string    `json:"instance"`
	CommonName  string    `json:"common_name"`
}

// Event is an audit record.
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

// Stats is a global rollup.
type Stats struct {
	InstancesTotal int     `json:"instances_total"`
	InstancesUp    int     `json:"instances_up"`
	RxBytes        int64   `json:"rx_bytes"`
	TxBytes        int64   `json:"tx_bytes"`
	RxBps          float64 `json:"rx_bps"`
	TxBps          float64 `json:"tx_bps"`
}

// ReadyStatus is returned by GET /readyz (no auth).
// Status is ok | degraded | fail. Checks holds individual probe results.
type ReadyStatus struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// HealthStatus is returned by GET /healthz (no auth).
type HealthStatus struct {
	Status string `json:"status"`
}

// CA is a managed certificate authority.
type CA struct {
	Name       string    `json:"name"`
	CommonName string    `json:"common_name"`
	Org        string    `json:"org,omitempty"`
	CertPath   string    `json:"cert_path"`
	KeyPath    string    `json:"key_path"`
	CRLPath    string    `json:"crl_path,omitempty"`
	CRLNumber  int64     `json:"crl_number,omitempty"`
	NotAfter   string    `json:"not_after,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateCARequest creates a new CA.
type CreateCARequest struct {
	Name       string `json:"name,omitempty"`
	CommonName string `json:"common_name"`
	Org        string `json:"org,omitempty"`
	ValidDays  int    `json:"valid_days,omitempty"`
	KeyType    string `json:"key_type,omitempty"` // ec | rsa
	RSABits    int    `json:"rsa_bits,omitempty"`
}

// Certificate is a managed leaf cert.
type Certificate struct {
	ID           int64     `json:"id"`
	CAName       string    `json:"ca_name"`
	Kind         string    `json:"kind"`
	CommonName   string    `json:"common_name"`
	CertPath     string    `json:"cert_path"`
	KeyPath      string    `json:"key_path"`
	NotBefore    string    `json:"not_before,omitempty"`
	NotAfter     string    `json:"not_after,omitempty"`
	Serial       int64     `json:"serial"`
	Fingerprint  string    `json:"fingerprint,omitempty"`
	Revoked      bool      `json:"revoked"`
	RevokedAt    string    `json:"revoked_at,omitempty"`
	RevokeReason string    `json:"revoke_reason,omitempty"`
	InstanceName string    `json:"instance_name,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// RevokeCertRequest optionally sets a revocation reason.
type RevokeCertRequest struct {
	Reason string `json:"reason,omitempty"`
}

// RenewCertRequest optionally overrides validity / key type on renew.
type RenewCertRequest struct {
	ValidDays int      `json:"valid_days,omitempty"`
	DNSNames  []string `json:"dns_names,omitempty"`
	IPs       []string `json:"ips,omitempty"`
	KeyType   string   `json:"key_type,omitempty"`
}

// IssueCertRequest issues a leaf cert under a CA.
type IssueCertRequest struct {
	CAName       string   `json:"ca_name"`
	Kind         string   `json:"kind"` // server | client
	CommonName   string   `json:"common_name"`
	ValidDays    int      `json:"valid_days,omitempty"`
	DNSNames     []string `json:"dns_names,omitempty"`
	IPs          []string `json:"ips,omitempty"`
	KeyType      string   `json:"key_type,omitempty"`
	InstanceName string   `json:"instance_name,omitempty"`
}

// IssueServerCertRequest wires a server cert onto an instance.
type IssueServerCertRequest struct {
	CAName           string   `json:"ca_name,omitempty"`
	CommonName       string   `json:"common_name,omitempty"`
	ValidDays        int      `json:"valid_days,omitempty"`
	DNSNames         []string `json:"dns_names,omitempty"`
	IPs              []string `json:"ips,omitempty"`
	KeyType          string   `json:"key_type,omitempty"`
	TLSCrypt         string   `json:"tls_crypt,omitempty"`          // existing named key
	GenerateTLSCrypt bool     `json:"generate_tls_crypt,omitempty"` // create named after instance
}

// IssueClientCertRequest issues a client cert and attaches paths.
type IssueClientCertRequest struct {
	CAName    string `json:"ca_name,omitempty"`
	ValidDays int    `json:"valid_days,omitempty"`
	KeyType   string `json:"key_type,omitempty"`
}

// TLSCryptRequest names a tls-crypt key to generate.
type TLSCryptRequest struct {
	Name string `json:"name,omitempty"`
}

// TLSCryptKey is a named OpenVPN static key.
type TLSCryptKey struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}
