package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// CtlConfig holds openvpnctl configuration.
type CtlConfig struct {
	Server struct {
		URL   string `mapstructure:"url"`
		Unix  string `mapstructure:"unix"`
		Token string `mapstructure:"token"`
	} `mapstructure:"server"`
	RefreshInterval string `mapstructure:"refresh_interval"`
}

// LoadCtl loads ctl config.
func LoadCtl(path string) (*CtlConfig, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("OPENVPNCTL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("server.url", "http://127.0.0.1:51980")
	v.SetDefault("server.unix", "")
	v.SetDefault("server.token", "change-me")
	v.SetDefault("refresh_interval", "2s")

	if path == "" {
		cands := []string{}
		if home, err := os.UserHomeDir(); err == nil {
			cands = append(cands, filepath.Join(home, ".config", "openvpnctl", "config.yaml"))
		}
		cands = append(cands,
			"/etc/openvpnctl/config.yaml",
			"/etc/openvpnd/openvpnctl.yaml",
		)
		for _, cand := range cands {
			if _, err := os.Stat(cand); err == nil {
				path = cand
				break
			}
		}
	}
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}
	_ = v.BindEnv("server.token", "OPENVPNCTL_TOKEN", "OPENVPND_AUTH_TOKEN")
	_ = v.BindEnv("server.url", "OPENVPNCTL_URL")
	_ = v.BindEnv("server.unix", "OPENVPNCTL_UNIX")

	var cfg CtlConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Refresh returns refresh duration.
func (c *CtlConfig) Refresh() time.Duration {
	d, err := time.ParseDuration(c.RefreshInterval)
	if err != nil {
		return 2 * time.Second
	}
	return d
}

// Endpoint returns connection string for the client.
func (c *CtlConfig) Endpoint() string {
	if c.Server.Unix != "" {
		return "unix://" + c.Server.Unix
	}
	return c.Server.URL
}
