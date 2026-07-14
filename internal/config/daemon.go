package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

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
		Token string `mapstructure:"token"`
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
	v.SetDefault("public_base_url", "")
	v.SetDefault("profile_links.default_ttl", "24h")
	v.SetDefault("profile_links.default_max_uses", 1)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("read_only", false)
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
