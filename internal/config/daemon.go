package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// API auth roles for bearer tokens.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleReadonly = "readonly"
)

// AuthToken is a named API credential with a role.
type AuthToken struct {
	Name  string `mapstructure:"name" yaml:"name"`
	Token string `mapstructure:"token" yaml:"token"`
	Role  string `mapstructure:"role" yaml:"role"` // admin | operator | readonly
}

// AuthPrincipal is a resolved bearer credential used by the API middleware.
type AuthPrincipal struct {
	Name  string
	Token string
	Role  string
}

// DaemonConfig holds openvpnd configuration.
type DaemonConfig struct {
	Listen struct {
		HTTP    string `mapstructure:"http"`
		Unix    string `mapstructure:"unix"`
		Metrics string `mapstructure:"metrics"`
	} `mapstructure:"listen"`
	SNMP struct {
		Enabled       bool   `mapstructure:"enabled"`
		Listen        string `mapstructure:"listen"`
		Community     string `mapstructure:"community"`
		EnterpriseOID string `mapstructure:"enterprise_oid"`
	} `mapstructure:"snmp"`
	DB struct {
		Path           string `mapstructure:"path"`
		TimeseriesPath string `mapstructure:"timeseries_path"`
	} `mapstructure:"db"`
	Auth struct {
		// Token is the legacy single bearer credential. When Tokens is empty it is
		// treated as role=admin (backward compatible). When Tokens is non-empty,
		// only Tokens are accepted.
		Token  string      `mapstructure:"token"`
		Tokens []AuthToken `mapstructure:"tokens"`
	} `mapstructure:"auth"`
	OpenVPN struct {
		ConfDir           string            `mapstructure:"conf_dir"`
		RuntimeDir        string            `mapstructure:"runtime_dir"`
		PKIDir            string            `mapstructure:"pki_dir"`
		Persistence       string            `mapstructure:"persistence"`
		DefaultBinary     string            `mapstructure:"default_binary"`
		Binaries          map[string]string `mapstructure:"binaries"`
		SampleInterval    string            `mapstructure:"sample_interval"`
		ReconcileInterval string            `mapstructure:"reconcile_interval"`
		AllowHooks        bool              `mapstructure:"allow_hooks"`
		UseMockBackend    bool              `mapstructure:"use_mock_backend"`
		AdoptOnStart      bool              `mapstructure:"adopt_on_start"`
		// AdoptTakeoverEnabled controls whether POST /instances/adopt with
		// take_over=true may SIGTERM/SIGKILL a verified openvpn PID.
		// When false, take_over only records operator notes (legacy behavior).
		// Default true via setDaemonDefaults.
		AdoptTakeoverEnabled bool `mapstructure:"adopt_takeover_enabled"`
		// BandwidthEnforcement: off | tc | shaper | log
		// off: no shaping (traffic_limit_bytes suspend still works)
		// tc: Linux tc HTB + ingress police per client static_ip (needs device name)
		// shaper: global OpenVPN --shaper from max client limits (confgen)
		// log: plan tc rules and log only (dry-run)
		BandwidthEnforcement string `mapstructure:"bandwidth_enforcement"`
	} `mapstructure:"openvpn"`
	// PublicBaseURL is the externally reachable base (https://vpn.example.com) used
	// when minting client profile download / OpenVPN Connect import links.
	// Empty falls back to http://{listen.http}.
	PublicBaseURL string `mapstructure:"public_base_url"`
	ProfileLinks  struct {
		DefaultTTL     string `mapstructure:"default_ttl"`      // e.g. 24h
		DefaultMaxUses int    `mapstructure:"default_max_uses"` // 1 = single-use; 0 = unlimited until expiry
	} `mapstructure:"profile_links"`
	Log struct {
		Level  string `mapstructure:"level"`
		Format string `mapstructure:"format"`
	} `mapstructure:"log"`
	ReadOnly bool `mapstructure:"read_only"`
	// Production enables strict startup checks (non-default auth token, etc.).
	Production bool `mapstructure:"production"`
	// Webhooks push agent events to an external controller (optional).
	Webhooks WebhooksConfig `mapstructure:"webhooks"`
}

// WebhooksConfig delivers HTTP callbacks for controller integration.
type WebhooksConfig struct {
	Enabled   bool     `mapstructure:"enabled" yaml:"enabled"`
	URL       string   `mapstructure:"url" yaml:"url"`
	Secret    string   `mapstructure:"secret" yaml:"secret"`
	Events    []string `mapstructure:"events" yaml:"events"` // empty/* = all; supports peer.*
	Timeout   string   `mapstructure:"timeout" yaml:"timeout"`
	QueueSize int      `mapstructure:"queue_size" yaml:"queue_size"`
}

// LoadDaemon loads daemon config from file/env/defaults.
func LoadDaemon(path string) (*DaemonConfig, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("OPENVPND")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDaemonDefaults(v)

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	_ = v.BindEnv("auth.token", "OPENVPND_AUTH_TOKEN", "OPENVPND_API_TOKEN")
	_ = v.BindEnv("db.path", "OPENVPND_DB_PATH")
	_ = v.BindEnv("db.timeseries_path", "OPENVPND_DB_TIMESERIES_PATH")
	_ = v.BindEnv("listen.http", "OPENVPND_LISTEN_HTTP")
	_ = v.BindEnv("public_base_url", "OPENVPND_PUBLIC_BASE_URL")

	var cfg DaemonConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if cfg.Auth.Token == "" {
		cfg.Auth.Token = v.GetString("auth.token")
	}
	if err := normalizeAuth(&cfg); err != nil {
		return nil, err
	}
	if cfg.OpenVPN.Binaries == nil {
		cfg.OpenVPN.Binaries = map[string]string{}
	}
	if cfg.OpenVPN.DefaultBinary == "" {
		cfg.OpenVPN.DefaultBinary = "default"
	}
	if _, ok := cfg.OpenVPN.Binaries[cfg.OpenVPN.DefaultBinary]; !ok {
		if p := v.GetString("openvpn.binaries.default"); p != "" {
			cfg.OpenVPN.Binaries[cfg.OpenVPN.DefaultBinary] = p
		} else if len(cfg.OpenVPN.Binaries) == 0 {
			cfg.OpenVPN.Binaries["default"] = "/usr/sbin/openvpn"
		}
	}
	cfg.OpenVPN.Persistence = strings.ToLower(strings.TrimSpace(cfg.OpenVPN.Persistence))
	switch cfg.OpenVPN.Persistence {
	case "", "database", "conf", "hybrid":
		if cfg.OpenVPN.Persistence == "" {
			cfg.OpenVPN.Persistence = "hybrid"
		}
	default:
		return nil, fmt.Errorf("openvpn.persistence %q invalid (want database|conf|hybrid)", cfg.OpenVPN.Persistence)
	}
	cfg.OpenVPN.BandwidthEnforcement = strings.ToLower(strings.TrimSpace(cfg.OpenVPN.BandwidthEnforcement))
	switch cfg.OpenVPN.BandwidthEnforcement {
	case "", "off", "none", "false":
		cfg.OpenVPN.BandwidthEnforcement = "off"
	case "tc", "htb":
		cfg.OpenVPN.BandwidthEnforcement = "tc"
	case "shaper", "log":
		// ok
	default:
		return nil, fmt.Errorf("openvpn.bandwidth_enforcement %q invalid (want off|tc|shaper|log)", cfg.OpenVPN.BandwidthEnforcement)
	}
	return &cfg, nil
}

func setDaemonDefaults(v *viper.Viper) {
	v.SetDefault("listen.http", "127.0.0.1:51980")
	v.SetDefault("listen.unix", "")
	v.SetDefault("listen.metrics", "127.0.0.1:9092")
	// SNMP on 1162 by default so it does not clash with wireguardd's 1161.
	v.SetDefault("snmp.enabled", false)
	v.SetDefault("snmp.listen", "127.0.0.1:1162")
	v.SetDefault("snmp.community", "change-me-snmp")
	v.SetDefault("snmp.enterprise_oid", "1.3.6.1.4.1.66666.2")
	v.SetDefault("db.path", "openvpnd.db")
	v.SetDefault("auth.token", "change-me")
	v.SetDefault("openvpn.conf_dir", "/etc/openvpnd/instances")
	v.SetDefault("openvpn.runtime_dir", "/run/openvpnd")
	v.SetDefault("openvpn.pki_dir", "/var/lib/openvpnd/pki")
	v.SetDefault("openvpn.persistence", "hybrid")
	v.SetDefault("openvpn.default_binary", "default")
	v.SetDefault("openvpn.binaries.default", "/usr/sbin/openvpn")
	v.SetDefault("openvpn.sample_interval", "5s")
	v.SetDefault("openvpn.reconcile_interval", "5s")
	v.SetDefault("openvpn.allow_hooks", false)
	v.SetDefault("openvpn.use_mock_backend", false)
	v.SetDefault("openvpn.adopt_on_start", false)
	v.SetDefault("openvpn.adopt_takeover_enabled", true)
	v.SetDefault("openvpn.bandwidth_enforcement", "off")
	v.SetDefault("public_base_url", "")
	v.SetDefault("profile_links.default_ttl", "24h")
	v.SetDefault("profile_links.default_max_uses", 1)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("read_only", false)
	v.SetDefault("production", false)
	v.SetDefault("webhooks.enabled", false)
	v.SetDefault("webhooks.url", "")
	v.SetDefault("webhooks.secret", "")
	v.SetDefault("webhooks.timeout", "5s")
	v.SetDefault("webhooks.queue_size", 256)
}

// WeakAuthToken reports whether no strong API credential is configured.
// Multi-token mode is strong when at least one non-default token is present.
// Legacy mode treats empty or "change-me" auth.token as weak.
func (c *DaemonConfig) WeakAuthToken() bool {
	if c == nil {
		return true
	}
	if len(c.Auth.Tokens) > 0 {
		for _, t := range c.Auth.Tokens {
			tok := strings.TrimSpace(t.Token)
			if tok != "" && tok != "change-me" {
				return false
			}
		}
		return true
	}
	t := strings.TrimSpace(c.Auth.Token)
	return t == "" || t == "change-me"
}

// AllowInsecure reports OPENVPND_ALLOW_INSECURE=1 (dev/CI escape hatch).
func AllowInsecure() bool {
	return os.Getenv("OPENVPND_ALLOW_INSECURE") == "1"
}

// Validate checks security-sensitive settings before the daemon starts.
//
// Weak auth tokens (empty or "change-me") are rejected unless
// OPENVPND_ALLOW_INSECURE=1. When production is true, weak tokens are always
// rejected even if ALLOW_INSECURE is set.
func (c *DaemonConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.WeakAuthToken() {
		if c.Production {
			return fmt.Errorf("production mode refuses weak API auth (empty or \"change-me\"); set auth.token or auth.tokens")
		}
		if !AllowInsecure() {
			return fmt.Errorf("API auth is empty or default \"change-me\"; set auth.token / auth.tokens, or set OPENVPND_ALLOW_INSECURE=1 for development")
		}
	}
	return nil
}

// DefaultBinaryPath returns the filesystem path for the configured default binary name.
func (c *DaemonConfig) DefaultBinaryPath() string {
	if c == nil {
		return ""
	}
	name := c.OpenVPN.DefaultBinary
	if name == "" {
		name = "default"
	}
	if c.OpenVPN.Binaries != nil {
		if p := c.OpenVPN.Binaries[name]; p != "" {
			return p
		}
	}
	return ""
}

// NormalizeRole maps role aliases to canonical Role* constants.
// Returns empty string if the role is unknown.
func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleAdmin, "administrator":
		return RoleAdmin
	case RoleOperator, "ops":
		return RoleOperator
	case RoleReadonly, "read_only", "read-only", "ro", "viewer":
		return RoleReadonly
	default:
		return ""
	}
}

func normalizeAuth(cfg *DaemonConfig) error {
	for i := range cfg.Auth.Tokens {
		t := &cfg.Auth.Tokens[i]
		t.Name = strings.TrimSpace(t.Name)
		t.Token = strings.TrimSpace(t.Token)
		role := NormalizeRole(t.Role)
		if t.Token == "" {
			return fmt.Errorf("auth.tokens[%d]: token is required", i)
		}
		if role == "" {
			return fmt.Errorf("auth.tokens[%d]: role %q invalid (want admin|operator|readonly)", i, t.Role)
		}
		t.Role = role
		if t.Name == "" {
			t.Name = role
		}
	}
	return nil
}

// AuthPrincipals returns accepted bearer credentials for the API.
// When auth.tokens is empty, auth.token is admin (legacy single-token mode).
// When auth.tokens is set, only those entries are accepted.
func (c *DaemonConfig) AuthPrincipals() []AuthPrincipal {
	if c == nil {
		return nil
	}
	if len(c.Auth.Tokens) > 0 {
		out := make([]AuthPrincipal, 0, len(c.Auth.Tokens))
		for _, t := range c.Auth.Tokens {
			if t.Token == "" {
				continue
			}
			role := NormalizeRole(t.Role)
			if role == "" {
				continue
			}
			name := t.Name
			if name == "" {
				name = role
			}
			out = append(out, AuthPrincipal{Name: name, Token: t.Token, Role: role})
		}
		return out
	}
	if c.Auth.Token == "" {
		return nil
	}
	return []AuthPrincipal{{Name: "admin", Token: c.Auth.Token, Role: RoleAdmin}}
}

// ProfileLinkTTL returns default profile link lifetime.
func (c *DaemonConfig) ProfileLinkTTL() time.Duration {
	d, err := time.ParseDuration(c.ProfileLinks.DefaultTTL)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// PublicBase returns absolute base URL without trailing slash.
func (c *DaemonConfig) PublicBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.PublicBaseURL), "/")
	if base != "" {
		return base
	}
	if c.Listen.HTTP != "" {
		return "http://" + c.Listen.HTTP
	}
	return "http://127.0.0.1:51980"
}

// ReconcileInterval parses duration.
func (c *DaemonConfig) ReconcileInterval() time.Duration {
	d, err := time.ParseDuration(c.OpenVPN.ReconcileInterval)
	if err != nil || d <= 0 {
		return 5 * time.Second
	}
	return d
}

// SampleInterval parses sample duration.
func (c *DaemonConfig) SampleInterval() time.Duration {
	d, err := time.ParseDuration(c.OpenVPN.SampleInterval)
	if err != nil || d <= 0 {
		return 5 * time.Second
	}
	return d
}
