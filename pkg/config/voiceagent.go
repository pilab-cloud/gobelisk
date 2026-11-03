package config

import "time"

// VoiceAgentConfig configures the GPU-backed WebRTC voice agent. The agent
// joins a Pir² meeting through the media control service and the SFU, listens
// to the meeting audio, and answers with synthesized speech.
type VoiceAgentConfig struct {
	// Enabled is a convenience flag for deployments that start the agent
	// alongside the PBX. The `gobelisk agent run` command works regardless.
	Enabled bool `yaml:"enabled"`

	// UID is the SFU peer id and grant subject. DisplayName is what meeting
	// clients show and Role is carried in the join grant (default "guest").
	UID         string `yaml:"uid"`
	DisplayName string `yaml:"display_name"`
	Role        string `yaml:"role"`

	// Pir² media control service. The agent mints a join grant through the
	// internal API, exactly like the Pir² recorder does.
	MediaServiceURL    string `yaml:"media_service_url"`
	MediaInternalToken string `yaml:"media_internal_token"`
	// SFUSignalingURL is used when the media service does not return one.
	SFUSignalingURL string `yaml:"sfu_signaling_url"`

	// Optional Pir² context. When set, the agent reports the meeting in its
	// exported transcript and in logs.
	MeetingServiceURL string `yaml:"meeting_service_url"`
	MeetingID         string `yaml:"meeting_id"`
	MeetingSessionID  string `yaml:"meeting_session_id"`

	Language     string `yaml:"language"`
	SystemPrompt string `yaml:"system_prompt"`
	Greeting     string `yaml:"greeting"`
	MaxHistory   int    `yaml:"max_history"`

	VAD        VoiceAgentVADConfig        `yaml:"vad"`
	ASR        VoiceAgentASRConfig        `yaml:"asr"`
	LLM        VoiceAgentLLMConfig        `yaml:"llm"`
	TTS        VoiceAgentTTSConfig        `yaml:"tts"`
	WakeWord   VoiceAgentWakeWordConfig   `yaml:"wake_word"`
	Transcript VoiceAgentTranscriptConfig `yaml:"transcript"`
}

// VoiceAgentWakeWordConfig controls wake-word / keyword gating for the voice agent.
type VoiceAgentWakeWordConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Word           string   `yaml:"word"`
	Aliases        []string `yaml:"aliases"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	AckPhrase      string   `yaml:"ack_phrase"`
}

// VoiceAgentVADConfig tunes the energy-based voice activity detector that
// segments meeting audio into utterances.
type VoiceAgentVADConfig struct {
	EnergyThreshold float64 `yaml:"energy_threshold"`
	SilenceMS       int     `yaml:"silence_ms"`
	MinSpeechMS     int     `yaml:"min_speech_ms"`
	MaxSpeechMS     int     `yaml:"max_speech_ms"`
	PreRollMS       int     `yaml:"pre_roll_ms"`
}

// VoiceAgentASRConfig points at a whisper / faster-whisper compatible HTTP endpoint.
// The agent sends raw 16 kHz mono s16le PCM and expects {"text": "..."}.
type VoiceAgentASRConfig struct {
	Endpoint       string `yaml:"endpoint"`
	ModelPath      string `yaml:"model_path"`
	Device         string `yaml:"device"`
	Language       string `yaml:"language"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// VoiceAgentLLMConfig points at an OpenAI-compatible chat completions
// endpoint (Ollama, vLLM, llama.cpp server).
type VoiceAgentLLMConfig struct {
	Endpoint       string  `yaml:"endpoint"`
	APIKey         string  `yaml:"api_key"`
	Model          string  `yaml:"model"`
	Temperature    float64 `yaml:"temperature"`
	MaxTokens      int     `yaml:"max_tokens"`
	TimeoutSeconds int     `yaml:"timeout_seconds"`
	// KeepAlive is sent as Ollama's keep_alive so the model stays resident on
	// the GPU between utterances (-1 means forever).
	KeepAlive string `yaml:"keep_alive"`
}

// VoiceAgentTTSConfig points at an OpenAI-compatible speech endpoint. The
// response may be WAV or raw s16le PCM. A command fallback (for example
// piper) can be configured instead of an endpoint.
type VoiceAgentTTSConfig struct {
	Endpoint       string `yaml:"endpoint"`
	APIKey         string `yaml:"api_key"`
	Model          string `yaml:"model"`
	ModelPath      string `yaml:"model_path"`
	Voice          string `yaml:"voice"`
	Format         string `yaml:"format"`
	// SampleRate is used when Format is raw "pcm"; WAV carries its own rate.
	SampleRate     int    `yaml:"sample_rate"`
	Command        string `yaml:"command"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// VoiceAgentTranscriptConfig controls local transcript files and export to
// the Pir² recorder service.
type VoiceAgentTranscriptConfig struct {
	Dir           string `yaml:"dir"`
	RecorderURL   string `yaml:"recorder_url"`
	RecorderToken string `yaml:"recorder_token"`
	RecordingID   string `yaml:"recording_id"`
	Language      string `yaml:"language"`
}

// ApplyDefaults fills the zero value with the local GPU stack described in
// docs/voice-agent.md.
func (c *VoiceAgentConfig) ApplyDefaults() {
	if c.UID == "" {
		c.UID = "voice-agent"
	}
	if c.DisplayName == "" {
		c.DisplayName = "AI Assistant"
	}
	if c.Role == "" {
		c.Role = "guest"
	}
	if c.Language == "" {
		c.Language = "hu"
	}
	if c.MaxHistory <= 0 {
		c.MaxHistory = 20
	}
	if c.Greeting == "" {
		c.Greeting = "Jó napot kívánok! Miben segíthetek?"
	}
	if c.SystemPrompt == "" {
		c.SystemPrompt = "Te egy segítőkész, magyar nyelvű hangasszisztens vagy egy online megbeszélésen. " +
			"Röviden, természetes beszédstílusban válaszolj. Ha nem tudsz valamit, mondd meg őszintén."
	}

	if c.VAD.EnergyThreshold <= 0 {
		c.VAD.EnergyThreshold = 500
	}
	if c.VAD.SilenceMS <= 0 {
		c.VAD.SilenceMS = 450
	}
	if c.VAD.MinSpeechMS <= 0 {
		c.VAD.MinSpeechMS = 300
	}
	if c.VAD.MaxSpeechMS <= 0 {
		c.VAD.MaxSpeechMS = 5000
	}
	if c.VAD.PreRollMS <= 0 {
		c.VAD.PreRollMS = 250
	}

	if c.ASR.Endpoint == "" {
		c.ASR.Endpoint = "http://127.0.0.1:8099/inference"
	}
	if c.ASR.ModelPath == "" {
		c.ASR.ModelPath = "/opt/whisper/models/ggml-large-v3-turbo-q5_0.bin"
	}
	if c.ASR.Device == "" {
		c.ASR.Device = "1"
	}
	if c.ASR.Language == "" {
		c.ASR.Language = c.Language
	}
	if c.ASR.TimeoutSeconds <= 0 {
		c.ASR.TimeoutSeconds = 30
	}

	if c.LLM.Endpoint == "" {
		c.LLM.Endpoint = "http://127.0.0.1:11434"
	}
	if c.LLM.Model == "" {
		c.LLM.Model = "qwen2.5:1.5b-instruct"
	}
	if c.LLM.Temperature <= 0 {
		c.LLM.Temperature = 0.1
	}
	if c.LLM.MaxTokens <= 0 {
		c.LLM.MaxTokens = 256
	}
	if c.LLM.TimeoutSeconds <= 0 {
		c.LLM.TimeoutSeconds = 60
	}
	if c.LLM.KeepAlive == "" {
		c.LLM.KeepAlive = "-1"
	}

	if c.TTS.Endpoint == "" && c.TTS.Command == "" {
		c.TTS.Endpoint = "http://127.0.0.1:8880/v1/audio/speech"
	}
	if c.TTS.Command == "" {
		c.TTS.Command = "piper"
	}
	if c.TTS.ModelPath == "" {
		c.TTS.ModelPath = "/opt/piper/models/hu_HU-anna-medium.onnx"
	}
	if c.TTS.Model == "" {
		c.TTS.Model = "hu_HU-anna-medium.onnx"
	}
	if c.TTS.Voice == "" {
		c.TTS.Voice = "anna"
	}
	if c.TTS.Format == "" {
		c.TTS.Format = "wav"
	}
	if c.TTS.SampleRate <= 0 {
		c.TTS.SampleRate = 22050
	}
	if c.TTS.TimeoutSeconds <= 0 {
		c.TTS.TimeoutSeconds = 60
	}

	if c.WakeWord.Word == "" {
		c.WakeWord.Word = "maci"
	}
	if len(c.WakeWord.Aliases) == 0 {
		c.WakeWord.Aliases = []string{"marci", "márci", "macika", "márti", "márcik", "mátszik"}
	}
	if c.WakeWord.TimeoutSeconds <= 0 {
		c.WakeWord.TimeoutSeconds = 25
	}
	if c.WakeWord.AckPhrase == "" {
		c.WakeWord.AckPhrase = "Igen, hallgatom!"
	}

	if c.Transcript.Dir == "" {
		c.Transcript.Dir = "transcripts"
	}
	if c.Transcript.Language == "" {
		c.Transcript.Language = c.Language
	}
}

// ASRTimeout returns the configured ASR timeout.
func (c VoiceAgentASRConfig) Timeout() time.Duration {
	return secondsOr(c.TimeoutSeconds, 30)
}

// LLMTimeout returns the configured LLM timeout.
func (c VoiceAgentLLMConfig) Timeout() time.Duration {
	return secondsOr(c.TimeoutSeconds, 60)
}

// TTSTimeout returns the configured TTS timeout.
func (c VoiceAgentTTSConfig) Timeout() time.Duration {
	return secondsOr(c.TimeoutSeconds, 60)
}

func secondsOr(secs, def int) time.Duration {
	if secs <= 0 {
		secs = def
	}
	return time.Duration(secs) * time.Second
}
