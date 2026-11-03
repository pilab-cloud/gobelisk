package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Host    string `yaml:"host"`
	UDPPort int    `yaml:"udp_port"`
	TCPPort int    `yaml:"tcp_port"`
	TLSPort int    `yaml:"tls_port"`

	GRPCPort        int    `yaml:"grpc_port"`
	GRPCTLSCert     string `yaml:"grpc_tls_cert"`
	GRPCTLSKey      string `yaml:"grpc_tls_key"`
	GRPCCACert      string `yaml:"grpc_ca_cert"`
	GRPCBearerToken string `yaml:"grpc_bearer_token"`
}

type AuthConfig struct {
	Type  string            `yaml:"type"`
	Realm string            `yaml:"realm"`
	Users map[string]string `yaml:"users"`
}

type Config struct {
	Server            ServerConfig `yaml:"server"`
	Auth              AuthConfig   `yaml:"auth"`
	DialPlan          string       `yaml:"dialplan"`
	MeetingServiceURL string       `yaml:"meeting_service_url"`
	MeetingBridgeExt  string       `yaml:"meeting_bridge_ext"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host:    "0.0.0.0",
			UDPPort: 15060,
			TCPPort: 15060,
			TLSPort: 15061,

			GRPCPort: 9090,
		},
		Auth: AuthConfig{
			Type:  "none",
			Realm: "gobelisk",
			Users: map[string]string{},
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Server.UDPPort == 0 {
		cfg.Server.UDPPort = 15060
	}
	if cfg.Server.TCPPort == 0 {
		cfg.Server.TCPPort = cfg.Server.UDPPort
	}
	if cfg.Server.TLSPort == 0 {
		cfg.Server.TLSPort = 15061
	}
	return cfg, nil
}
