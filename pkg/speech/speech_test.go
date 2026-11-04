package speech

import (
	"errors"
	"testing"
)

func TestNoopEngine(t *testing.T) {
	var e Engine = Noop{}

	if _, err := e.Transcribe("audio.wav"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Transcribe error = %v, want ErrUnavailable", err)
	}
	if err := e.Synthesize("hello", "out.wav", "anna"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Synthesize error = %v, want ErrUnavailable", err)
	}
}
