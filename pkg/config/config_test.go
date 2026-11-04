package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultFeatures(t *testing.T) {
	c := Default()
	if !c.CallWaitingEnabled("100") {
		t.Error("call waiting should be enabled by default")
	}
	if got, want := c.RingTimeout(), 20*time.Second; got != want {
		t.Errorf("RingTimeout() = %v, want %v", got, want)
	}
}

func TestCallWaitingPerExtensionOverride(t *testing.T) {
	c := Default()
	c.Features.CallWaiting = false
	c.Features.CallWaitingExtensions = map[string]bool{"100": true, "101": false}

	if !c.CallWaitingEnabled("100") {
		t.Error("extension 100 should have call waiting enabled")
	}
	if c.CallWaitingEnabled("101") {
		t.Error("extension 101 should have call waiting disabled")
	}
	if c.CallWaitingEnabled("102") {
		t.Error("extension 102 should fall back to the disabled global default")
	}
}

func TestRingTimeoutOverride(t *testing.T) {
	c := Default()
	c.Features.RingTimeoutSeconds = 5
	if got, want := c.RingTimeout(), 5*time.Second; got != want {
		t.Errorf("RingTimeout() = %v, want %v", got, want)
	}

	c.Features.RingTimeoutSeconds = 0
	if got, want := c.RingTimeout(), 20*time.Second; got != want {
		t.Errorf("zero RingTimeout() = %v, want default %v", got, want)
	}
}

func TestLoadFeatureConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "features:\n" +
		"  call_waiting: false\n" +
		"  call_waiting_extensions:\n" +
		"    \"100\": true\n" +
		"  ring_timeout_seconds: 8\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.CallWaitingEnabled("200") {
		t.Error("global call waiting should be disabled")
	}
	if !c.CallWaitingEnabled("100") {
		t.Error("extension 100 override should enable call waiting")
	}
	if got, want := c.RingTimeout(), 8*time.Second; got != want {
		t.Errorf("RingTimeout() = %v, want %v", got, want)
	}
}

func TestLoadGobeliskYAML(t *testing.T) {
	// Verify that the root gobelisk.yaml loads cleanly.
	if _, err := Load("../../gobelisk.yaml"); err != nil {
		t.Fatalf("failed to load root gobelisk.yaml: %v", err)
	}
}

func TestLoadSpeechConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := "speech:\n" +
		"  engine: whisper-piper\n" +
		"  options:\n" +
		"    asr:\n" +
		"      language: hu\n" +
		"      timeout_seconds: 12\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Speech.Engine != "whisper-piper" {
		t.Errorf("Speech.Engine = %q, want whisper-piper", c.Speech.Engine)
	}
	asr, ok := c.Speech.Options["asr"].(map[string]any)
	if !ok {
		t.Fatalf("Speech.Options[asr] = %T, want map[string]any", c.Speech.Options["asr"])
	}
	if asr["language"] != "hu" {
		t.Errorf("asr.language = %v, want hu", asr["language"])
	}
	if asr["timeout_seconds"] != 12 {
		t.Errorf("asr.timeout_seconds = %v, want 12", asr["timeout_seconds"])
	}
}
