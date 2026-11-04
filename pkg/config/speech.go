package config

// SpeechConfig selects the pluggable speech-to-text / text-to-speech engine.
// The core ships a no-op engine; AI implementations register themselves with
// speech.Register and are selected by name here.
type SpeechConfig struct {
	// Engine is the registered engine name, for example "whisper-piper".
	// Empty (or "noop") keeps the built-in no-op engine.
	Engine string `yaml:"engine"`
	// Options is passed verbatim to the engine factory. Its schema is defined
	// by the engine implementation.
	Options map[string]any `yaml:"options"`
}
