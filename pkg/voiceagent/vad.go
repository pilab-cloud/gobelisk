package voiceagent

import "github.com/devopshaven/gobelisk/pkg/config"

const vadFrameSamples = SampleRate16k * 20 / 1000 // 20 ms

// VAD segments a continuous 16 kHz PCM stream into utterances with a simple
// RMS energy gate and a trailing-silence endpoint. It is intentionally
// dependency-free; the expensive part (ASR) runs only once per utterance.
type VAD struct {
	cfg config.VoiceAgentVADConfig

	pending []int16
	preRoll []int16

	speaking     bool
	buf          []int16
	voiced       int
	lastVoicedAt int
	silenceFrame int
}

// NewVAD returns a voice activity detector with the given tuning.
func NewVAD(cfg config.VoiceAgentVADConfig) *VAD {
	if cfg.EnergyThreshold <= 0 {
		cfg.EnergyThreshold = 500
	}
	if cfg.SilenceMS <= 0 {
		cfg.SilenceMS = 450
	}
	if cfg.MinSpeechMS <= 0 {
		cfg.MinSpeechMS = 300
	}
	if cfg.PreRollMS <= 0 {
		cfg.PreRollMS = 250
	}
	return &VAD{cfg: cfg}
}

// Reset discards buffered audio and returns to the silence state.
func (v *VAD) Reset() {
	v.pending = v.pending[:0]
	v.preRoll = v.preRoll[:0]
	v.buf = v.buf[:0]
	v.speaking = false
	v.voiced = 0
	v.lastVoicedAt = 0
	v.silenceFrame = 0
}

// Push feeds PCM and returns any utterances that ended in this batch, plus
// whether speech started. speechStarted lets the caller interrupt the current
// spoken answer (barge-in).
func (v *VAD) Push(pcm []int16) (utterances [][]int16, speechStarted bool) {
	v.pending = append(v.pending, pcm...)

	for len(v.pending) >= vadFrameSamples {
		frame := v.pending[:vadFrameSamples]
		v.pending = v.pending[vadFrameSamples:]
		utterance, started := v.pushFrame(frame)
		if started {
			speechStarted = true
		}
		if utterance != nil {
			utterances = append(utterances, utterance)
		}
	}
	return utterances, speechStarted
}

func (v *VAD) pushFrame(frame []int16) ([]int16, bool) {
	voiced := rms(frame) > v.cfg.EnergyThreshold

	if !v.speaking {
		v.pushPreRoll(frame)
		if !voiced {
			return nil, false
		}
		// Start with the pre-roll so the first phoneme is not clipped.
		v.speaking = true
		v.silenceFrame = 0
		v.voiced = len(frame)
		v.buf = append(v.buf[:0], v.preRoll...)
		v.lastVoicedAt = len(v.buf)
		return nil, true
	}

	v.buf = append(v.buf, frame...)
	if voiced {
		v.voiced += len(frame)
		v.silenceFrame = 0
		v.lastVoicedAt = len(v.buf)
		return nil, false
	}

	v.silenceFrame++
	if v.silenceFrame*vadFrameSamples*1000/SampleRate16k < v.cfg.SilenceMS {
		return nil, false
	}

	utterance := v.finishUtterance()
	return utterance, false
}

func (v *VAD) finishUtterance() []int16 {
	defer func() {
		v.speaking = false
		v.buf = v.buf[:0]
		v.voiced = 0
		v.lastVoicedAt = 0
		v.silenceFrame = 0
	}()

	if v.voiced*1000/SampleRate16k < v.cfg.MinSpeechMS {
		return nil
	}

	// Keep 200 ms of trailing silence; Whisper uses it as an endpoint hint
	// without padding the request with most of the silence window.
	tail := SampleRate16k * 200 / 1000
	end := v.lastVoicedAt + tail
	if end > len(v.buf) {
		end = len(v.buf)
	}

	out := make([]int16, end)
	copy(out, v.buf[:end])
	return out
}

func (v *VAD) pushPreRoll(frame []int16) {
	v.preRoll = append(v.preRoll, frame...)
	max := SampleRate16k * v.cfg.PreRollMS / 1000
	if len(v.preRoll) > max {
		v.preRoll = v.preRoll[len(v.preRoll)-max:]
	}
}

// Speaking reports whether the detector is currently inside an utterance.
func (v *VAD) Speaking() bool { return v.speaking }
