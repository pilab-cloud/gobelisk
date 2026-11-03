package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

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

// FeatureConfig holds call-feature toggles.
type FeatureConfig struct {
	// CallWaiting allows a second call to be delivered to an extension that is
	// already in an established call, instead of answering 486 Busy Here.
	CallWaiting bool `yaml:"call_waiting"`
	// CallWaitingExtensions overrides CallWaiting per extension.
	CallWaitingExtensions map[string]bool `yaml:"call_waiting_extensions"`
	// RingTimeoutSeconds is how long a ring group member rings before the
	// next member is tried (sequential strategies) or the call gives up
	// (simultaneous). Defaults to 20.
	RingTimeoutSeconds int `yaml:"ring_timeout_seconds"`
}

type Config struct {
	Server            ServerConfig     `yaml:"server"`
	Auth              AuthConfig       `yaml:"auth"`
	Features          FeatureConfig    `yaml:"features"`
	VoiceAgent        VoiceAgentConfig `yaml:"voice_agent"`
	DialPlan          string           `yaml:"dialplan"`
	MeetingServiceURL string           `yaml:"meeting_service_url"`
	MeetingBridgeExt  string           `yaml:"meeting_bridge_ext"`
}

// RingTimeout returns the configured ring timeout, falling back to the default.
func (c *Config) RingTimeout() time.Duration {
	secs := c.Features.RingTimeoutSeconds
	if secs <= 0 {
		secs = 20
	}
	return time.Duration(secs) * time.Second
}

// CallWaitingEnabled reports whether call waiting is enabled for an extension.
func (c *Config) CallWaitingEnabled(ext string) bool {
	if c == nil {
		return false
	}
	if v, ok := c.Features.CallWaitingExtensions[ext]; ok {
		return v
	}
	return c.Features.CallWaiting
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
		Features: FeatureConfig{
			CallWaiting:           true,
			CallWaitingExtensions: map[string]bool{},
			RingTimeoutSeconds:    20,
		},
		VoiceAgent: func() VoiceAgentConfig {
			va := VoiceAgentConfig{}
			va.ApplyDefaults()
			return va
		}(),
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		if envPath := os.Getenv("GOBELISK_CONFIG"); envPath != "" {
			path = envPath
		} else {
			homeDir, _ := os.UserHomeDir()
			candidates := []string{
				"gobelisk.yaml",
				"config.yaml",
				filepath.Join(homeDir, ".gobelisk", "config.yaml"),
			}
			for _, cand := range candidates {
				if _, err := os.Stat(cand); err == nil {
					path = cand
					break
				}
			}
		}
	}
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
	cfg.VoiceAgent.ApplyDefaults()
	return cfg, nil
}
