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

func TestDefaultVoiceAgent(t *testing.T) {
	c := Default()
	va := c.VoiceAgent
	if va.UID != "voice-agent" || va.DisplayName == "" {
		t.Errorf("voice agent identity = %q / %q", va.UID, va.DisplayName)
	}
	if va.ASR.Endpoint == "" || va.LLM.Endpoint == "" || va.TTS.Endpoint == "" {
		t.Errorf("voice agent endpoints not defaulted: %+v", va)
	}
	if va.VAD.SilenceMS != 450 || va.MaxHistory != 20 {
		t.Errorf("voice agent tunables = %+v", va.VAD)
	}
	if va.LLM.KeepAlive != "-1" {
		t.Errorf("keep_alive = %q", va.LLM.KeepAlive)
	}
}

func TestLoadVoiceAgentConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "voice_agent:\n" +
		"  uid: assistant\n" +
		"  language: en\n" +
		"  media_service_url: http://media:8090\n" +
		"  llm:\n" +
		"    model: qwen2.5:1.5b\n" +
		"  vad:\n" +
		"    silence_ms: 600\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	va := c.VoiceAgent
	if va.UID != "assistant" || va.Language != "en" {
		t.Errorf("identity = %q / %q", va.UID, va.Language)
	}
	if va.MediaServiceURL != "http://media:8090" {
		t.Errorf("media url = %q", va.MediaServiceURL)
	}
	if va.LLM.Model != "qwen2.5:1.5b" {
		t.Errorf("llm model = %q", va.LLM.Model)
	}
	// Unset fields still get the local-GPU defaults.
	if va.LLM.Endpoint == "" || va.ASR.Endpoint == "" || va.TTS.Endpoint == "" {
		t.Errorf("defaults not applied: %+v", va)
	}
	if va.VAD.SilenceMS != 600 || va.VAD.EnergyThreshold == 0 {
		t.Errorf("vad = %+v", va.VAD)
	}
}

func TestLoadGobeliskYAML(t *testing.T) {
	// Verify that the root gobelisk.yaml loads cleanly and parses all model configs
	c, err := Load("../../gobelisk.yaml")
	if err != nil {
		t.Fatalf("failed to load root gobelisk.yaml: %v", err)
	}
	va := c.VoiceAgent
	if va.ASR.ModelPath != "/opt/whisper/models/ggml-large-v3-turbo-q5_0.bin" {
		t.Errorf("ASR ModelPath = %q", va.ASR.ModelPath)
	}
	if va.ASR.Device != "1" {
		t.Errorf("ASR Device = %q", va.ASR.Device)
	}
	if va.LLM.Model != "qwen2.5:1.5b-instruct" {
		t.Errorf("LLM Model = %q", va.LLM.Model)
	}
	if va.TTS.ModelPath != "/opt/piper/models/hu_HU-anna-medium.onnx" {
		t.Errorf("TTS ModelPath = %q", va.TTS.ModelPath)
	}
	if va.TTS.Voice != "anna" {
		t.Errorf("TTS Voice = %q", va.TTS.Voice)
	}
	if va.VAD.SilenceMS != 640 || va.VAD.EnergyThreshold != 500.0 {
		t.Errorf("VAD tunables = %+v", va.VAD)
	}
	if !va.WakeWord.Enabled || va.WakeWord.Word != "maci" || va.WakeWord.TimeoutSeconds != 25 {
		t.Errorf("WakeWord config = %+v", va.WakeWord)
	}
}
