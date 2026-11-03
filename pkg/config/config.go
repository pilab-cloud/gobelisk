package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	// Hold is the hold/resume policy (GOBELISK-10).
	Hold HoldConfig `yaml:"hold"`
	// DNDExtensions lists extensions with Do Not Disturb enabled; incoming
	// calls to them are rejected with 603 Declined (GOBELISK-14).
	DNDExtensions []string `yaml:"dnd_extensions"`
	// CallForward holds per-extension forwarding rules (GOBELISK-15).
	CallForward map[string]ForwardConfig `yaml:"call_forward"`
	// FeatureCodes are the dialable feature codes (GOBELISK-18).
	FeatureCodes FeatureCodeConfig `yaml:"feature_codes"`
	// Park is the call-park orbit (GOBELISK-12).
	Park ParkConfig `yaml:"park"`
}

// FeatureCodeConfig lists the codes dialed as a number (GOBELISK-18).
type FeatureCodeConfig struct {
	// Enabled toggles feature-code interception; nil means on.
	Enabled *bool `yaml:"enabled"`
	// DNDOn / DNDOff toggle Do Not Disturb for the calling extension.
	DNDOn  string `yaml:"dnd_on"`
	DNDOff string `yaml:"dnd_off"`
	// Park parks the calling party in the next free orbit slot.
	Park string `yaml:"park"`
}

// enabled returns the effective master switch (default: on).
func (f FeatureCodeConfig) enabled() bool { return f.Enabled == nil || *f.Enabled }

// ParkConfig is the call-park orbit (GOBELISK-12): slots are plain numbers
// OrbitStart .. OrbitStart+OrbitSize-1. OrbitSize 0 disables parking.
type ParkConfig struct {
	OrbitStart     int `yaml:"orbit_start"`
	OrbitSize      int `yaml:"orbit_size"`
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// ForwardConfig is call forwarding for one extension: all three targets are
// optional dialable numbers (extensions or dialplan destinations).
type ForwardConfig struct {
	// Unconditional (CFU): every call goes straight to the target; the
	// extension is never rung, registered or not.
	Unconditional string `yaml:"unconditional"`
	// Busy (CFB): used instead of 486 when the extension is in a call and
	// call waiting is off.
	Busy string `yaml:"busy"`
	// NoAnswer (CFNA): used when the extension does not pick up within
	// NoAnswerSeconds; the ringing leg is cancelled first.
	NoAnswer string `yaml:"no_answer"`
	// NoAnswerSeconds is the ring time before the no-answer forward.
	// 0 (default) disables the no-answer target.
	NoAnswerSeconds int `yaml:"no_answer_seconds"`
}

// NoAnswerTimer returns how long the extension may ring before the
// no-answer forward kicks in, or 0 when it is not configured.
func (f ForwardConfig) NoAnswerTimer() time.Duration {
	if f.NoAnswer == "" || f.NoAnswerSeconds <= 0 {
		return 0
	}
	return time.Duration(f.NoAnswerSeconds) * time.Second
}

// HoldConfig is the hold/resume policy. A nil Enabled means "default on", so
// a zero-value config (or a YAML file without a hold section) keeps hold
// working.
type HoldConfig struct {
	// Enabled is the master switch. When false, a re-INVITE or UPDATE that
	// offers a held media stream (a=sendonly / a=recvonly / a=inactive) is
	// rejected with 403 Forbidden. Session refreshes and codec renegotiation
	// are unaffected.
	Enabled *bool `yaml:"enabled"`
	// PerExtension overrides Enabled for single extensions, keyed by
	// extension number. The extension placing the call on hold decides.
	PerExtension map[string]bool `yaml:"per_extension"`
}

// Bool returns a pointer to v, for building config values in Go.
func Bool(v bool) *bool { return &v }

// enabled returns the effective master switch (default: on).
func (h HoldConfig) enabled() bool { return h.Enabled == nil || *h.Enabled }

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

// HoldEnabled reports whether an extension may place a call on hold
// (GOBELISK-10). Defaults to true.
func (c *Config) HoldEnabled(ext string) bool {
	if c == nil {
		return true
	}
	if v, ok := c.Features.Hold.PerExtension[ext]; ok {
		return v
	}
	return c.Features.Hold.enabled()
}

// DNDEnabled reports whether an extension has Do Not Disturb on (GOBELISK-14).
func (c *Config) DNDEnabled(ext string) bool {
	if c == nil {
		return false
	}
	for _, e := range c.Features.DNDExtensions {
		if e == ext {
			return true
		}
	}
	return false
}

// Forward returns the call-forwarding rules of an extension (GOBELISK-15).
func (c *Config) Forward(ext string) ForwardConfig {
	if c == nil {
		return ForwardConfig{}
	}
	return c.Features.CallForward[ext]
}

// FeatureCodesEnabled reports whether feature codes are intercepted (GOBELISK-18).
func (c *Config) FeatureCodesEnabled() bool {
	return c == nil || c.Features.FeatureCodes.enabled()
}

// ParkSlot maps a dialed number to a zero-based orbit slot, if it is inside
// the park orbit (GOBELISK-12).
func (c *Config) ParkSlot(ext string) (int, bool) {
	if c == nil || c.Features.Park.OrbitSize <= 0 {
		return 0, false
	}
	n, err := strconv.Atoi(ext)
	if err != nil {
		return 0, false
	}
	slot := n - c.Features.Park.OrbitStart
	if slot < 0 || slot >= c.Features.Park.OrbitSize {
		return 0, false
	}
	return slot, true
}

// ParkNumber returns the dialable number of an orbit slot.
func (c *Config) ParkNumber(slot int) string {
	if c == nil {
		return strconv.Itoa(slot)
	}
	return strconv.Itoa(c.Features.Park.OrbitStart + slot)
}

// ParkTimeout is how long a call may wait parked before it is hung up.
func (c *Config) ParkTimeout() time.Duration {
	if c == nil || c.Features.Park.TimeoutSeconds <= 0 {
		return 300 * time.Second
	}
	return time.Duration(c.Features.Park.TimeoutSeconds) * time.Second
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
			Hold: HoldConfig{
				Enabled:      Bool(true),
				PerExtension: map[string]bool{},
			},
			DNDExtensions: []string{},
			CallForward:   map[string]ForwardConfig{},
			FeatureCodes: FeatureCodeConfig{
				Enabled: Bool(true),
				DNDOn:   "*78",
				DNDOff:  "*79",
				Park:    "*70",
			},
			Park: ParkConfig{
				OrbitStart:     701,
				OrbitSize:      10,
				TimeoutSeconds: 300,
			},
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
