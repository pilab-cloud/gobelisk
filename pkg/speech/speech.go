// Package speech defines the pluggable speech processing contract used by the
// PBX. The core build ships a Noop engine; the AI side-project provides the
// Whisper/Piper-backed implementation.
package speech

import "errors"

// ErrUnavailable is returned by the Noop engine.
var ErrUnavailable = errors.New("speech engine unavailable: install the AI side-project implementation")

// Engine provides speech-to-text and text-to-speech.
type Engine interface {
	// Transcribe returns the text spoken in audioPath.
	Transcribe(audioPath string) (string, error)
	// Synthesize writes text spoken with voice to outputPath.
	Synthesize(text, outputPath, voice string) error
}

// Noop is the default Engine. It performs no work and reports that speech
// processing is unavailable.
type Noop struct{}

// Transcribe implements Engine.
func (Noop) Transcribe(string) (string, error) {
	return "", ErrUnavailable
}

// Synthesize implements Engine.
func (Noop) Synthesize(string, string, string) error {
	return ErrUnavailable
}
