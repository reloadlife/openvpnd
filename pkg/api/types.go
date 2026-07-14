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
type InstanceCreateRequest struct {
	Name             string   `json:"name"`
	Role             string   `json:"role"` // server | client
	Enabled          *bool    `json:"enabled,omitempty"`
	BinaryName       string   `json:"binary_name,omitempty"`
	BinaryPath       string   `json:"binary_path,omitempty"`
	DevType          string   `json:"dev_type,omitempty"`
	Device           string   `json:"device,omitempty"`
	Proto            string   `json:"proto,omitempty"`
	LocalBind        string   `json:"local_bind,omitempty"`
	Port             int      `json:"port,omitempty"`
	Remotes          []Remote `json:"remotes,omitempty"`
	ServerNetwork    string   `json:"server_network,omitempty"`
	Topology         string   `json:"topology,omitempty"`
	PoolStart        string   `json:"pool_start,omitempty"`
	PoolEnd          string   `json:"pool_end,omitempty"`
	AuthMode         string   `json:"auth_mode,omitempty"`
	Cipher           string   `json:"cipher,omitempty"`
	DataCiphers      string   `json:"data_ciphers,omitempty"`
	AuthDigest       string   `json:"auth_digest,omitempty"`
	PushRoutes       []string `json:"push_routes,omitempty"`
	PushDNS          []string `json:"push_dns,omitempty"`
	PushDomain       string   `json:"push_domain,omitempty"`
	RedirectGateway  bool     `json:"redirect_gateway,omitempty"`
	PKICaPath        string   `json:"pki_ca_path,omitempty"`
	PKICertPath      string   `json:"pki_cert_path,omitempty"`
	PKIKeyPath       string   `json:"pki_key_path,omitempty"`
	PKITLSCryptPath string   `json:"pki_tls_crypt_path,omitempty"`
	PKIDHPath        string   `json:"pki_dh_path,omitempty"`
	StaticKeyPath    string   `json:"static_key_path,omitempty"`
	ExtraDirectives  string   `json:"extra_directives,omitempty"`
	PreUp            string   `json:"pre_up,omitempty"`
	PostUp           string   `json:"post_up,omitempty"`
	PreDown          string   `json:"pre_down,omitempty"`
	PostDown         string   `json:"post_down,omitempty"`
	PublicEndpoint   string   `json:"public_endpoint,omitempty"` // host or host:port for client profiles
}

// InstanceUpdateRequest patches an instance.
type InstanceUpdateRequest struct {
	Enabled          *bool    `json:"enabled,omitempty"`
	BinaryName       *string  `json:"binary_name,omitempty"`
	BinaryPath       *string  `json:"binary_path,omitempty"`
	DevType          *string  `json:"dev_type,omitempty"`
	Device           *string  `json:"device,omitempty"`
	Proto            *string  `json:"proto,omitempty"`
	LocalBind        *string  `json:"local_bind,omitempty"`
	Port             *int     `json:"port,omitempty"`
	Remotes          []Remote `json:"remotes,omitempty"`
	ServerNetwork    *string  `json:"server_network,omitempty"`
	Topology         *string  `json:"topology,omitempty"`
	PoolStart        *string  `json:"pool_start,omitempty"`
	PoolEnd          *string  `json:"pool_end,omitempty"`
	AuthMode         *string  `json:"auth_mode,omitempty"`
	Cipher           *string  `json:"cipher,omitempty"`
	DataCiphers      *string  `json:"data_ciphers,omitempty"`
	AuthDigest       *string  `json:"auth_digest,omitempty"`
	PushRoutes       []string `json:"push_routes,omitempty"`
	PushDNS          []string `json:"push_dns,omitempty"`
	PushDomain       *string  `json:"push_domain,omitempty"`
	RedirectGateway  *bool    `json:"redirect_gateway,omitempty"`
	PKICaPath        *string  `json:"pki_ca_path,omitempty"`
	PKICertPath      *string  `json:"pki_cert_path,omitempty"`
	PKIKeyPath       *string  `json:"pki_key_path,omitempty"`
	PKITLSCryptPath *string  `json:"pki_tls_crypt_path,omitempty"`
	PKIDHPath        *string  `json:"pki_dh_path,omitempty"`
	StaticKeyPath    *string  `json:"static_key_path,omitempty"`
	ExtraDirectives  *string  `json:"extra_directives,omitempty"`
	PreUp            *string  `json:"pre_up,omitempty"`
	PostUp           *string  `json:"post_up,omitempty"`
	PreDown          *string  `json:"pre_down,omitempty"`
	PostDown         *string  `json:"post_down,omitempty"`
	PublicEndpoint   *string  `json:"public_endpoint,omitempty"`
}

// Instance is the API representation.
type Instance struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Role             string    `json:"role"`
	Enabled          bool      `json:"enabled"`
	Up               bool      `json:"up"`
	BinaryName       string    `json:"binary_name"`
	BinaryPath       string    `json:"binary_path,omitempty"`
	DevType          string    `json:"dev_type"`
	Device           string    `json:"device,omitempty"`
	Proto            string    `json:"proto"`
	LocalBind        string    `json:"local_bind,omitempty"`
	Port             int       `json:"port"`
	Remotes          []Remote  `json:"remotes,omitempty"`
	ServerNetwork    string    `json:"server_network,omitempty"`
	Topology         string    `json:"topology,omitempty"`
	PoolStart        string    `json:"pool_start,omitempty"`
	PoolEnd          string    `json:"pool_end,omitempty"`
	AuthMode         string    `json:"auth_mode"`
	Cipher           string    `json:"cipher,omitempty"`
	DataCiphers      string    `json:"data_ciphers,omitempty"`
	AuthDigest       string    `json:"auth_digest,omitempty"`
	PushRoutes       []string  `json:"push_routes,omitempty"`
	PushDNS          []string  `json:"push_dns,omitempty"`
	PushDomain       string    `json:"push_domain,omitempty"`
	RedirectGateway  bool      `json:"redirect_gateway"`
	PKICaPath        string    `json:"pki_ca_path,omitempty"`
	PKICertPath      string    `json:"pki_cert_path,omitempty"`
	PKIKeyPath       string    `json:"pki_key_path,omitempty"`
	PKITLSCryptPath string    `json:"pki_tls_crypt_path,omitempty"`
	PKIDHPath        string    `json:"pki_dh_path,omitempty"`
	StaticKeyPath    string    `json:"static_key_path,omitempty"`
	ExtraDirectives  string    `json:"extra_directives,omitempty"`
	PreUp            string    `json:"pre_up,omitempty"`
	PostUp           string    `json:"post_up,omitempty"`
	PreDown          string    `json:"pre_down,omitempty"`
	PostDown         string    `json:"post_down,omitempty"`
	PublicEndpoint   string    `json:"public_endpoint,omitempty"`
	PID              int       `json:"pid,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	ConnectedClients int       `json:"connected_clients"`
	RxBytes          int64     `json:"rx_bytes"`
	TxBytes          int64     `json:"tx_bytes"`
	RxBps            float64   `json:"rx_bps"`
	TxBps            float64   `json:"tx_bps"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ClientCreateRequest creates a server client.
type ClientCreateRequest struct {
	CommonName        string   `json:"common_name"`
	Name              string   `json:"name,omitempty"`
	Notes             string   `json:"notes,omitempty"`
	StaticIP          string   `json:"static_ip,omitempty"` // empty/auto → allocate
	PushRoutes        []string `json:"push_routes,omitempty"`
	Suspended         bool     `json:"suspended,omitempty"`
	TrafficLimitBytes int64    `json:"traffic_limit_bytes,omitempty"`
	BandwidthRxBps    int64    `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps    int64    `json:"bandwidth_tx_bps,omitempty"`
	CertRef           string   `json:"cert_ref,omitempty"`
	ClientCertPath    string   `json:"client_cert_path,omitempty"`
	ClientKeyPath     string   `json:"client_key_path,omitempty"`
	// IssueCert issues a client cert under CAName (or default CA) and sets paths.
	IssueCert bool   `json:"issue_cert,omitempty"`
	CAName    string `json:"ca_name,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

// ClientUpdateRequest patches a client.
type ClientUpdateRequest struct {
	Name              *string  `json:"name,omitempty"`
	Notes             *string  `json:"notes,omitempty"`
	StaticIP          *string  `json:"static_ip,omitempty"`
	PushRoutes        []string `json:"push_routes,omitempty"`
	Suspended         *bool    `json:"suspended,omitempty"`
	TrafficLimitBytes *int64   `json:"traffic_limit_bytes,omitempty"`
	BandwidthRxBps    *int64   `json:"bandwidth_rx_bps,omitempty"`
	BandwidthTxBps    *int64   `json:"bandwidth_tx_bps,omitempty"`
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
	Suspended         bool      `json:"suspended"`
	Connected         bool      `json:"connected"`
	TrafficLimitBytes int64     `json:"traffic_limit_bytes"`
	BandwidthRxBps    int64     `json:"bandwidth_rx_bps"`
	BandwidthTxBps    int64     `json:"bandwidth_tx_bps"`
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

// CA is a managed certificate authority.
type CA struct {
	Name       string    `json:"name"`
	CommonName string    `json:"common_name"`
	Org        string    `json:"org,omitempty"`
	CertPath   string    `json:"cert_path"`
	KeyPath    string    `json:"key_path"`
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
	InstanceName string    `json:"instance_name,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
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
