package voiceagent

import (
	"encoding/binary"
	"math"
)

// SampleRate16k is the rate used by the VAD and the ASR contract.
const SampleRate16k = 16000

// SampleRate8k is the rate used by the G.711 (PCMU) RTP track that the agent
// publishes to the SFU.
const SampleRate8k = 8000

// PCM16LEBytes serializes 16-bit samples as little-endian PCM, the format the
// faster-whisper ASR service expects.
func PCM16LEBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

// BytesToPCM16LE parses little-endian 16-bit PCM.
func BytesToPCM16LE(data []byte) []int16 {
	out := make([]int16, len(data)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return out
}

// EncodeULaw converts one 16-bit linear sample to G.711 μ-law.
func EncodeULaw(sample int16) byte {
	const (
		bias = 0x84
		clip = 32635
	)

	s := int(sample)
	sign := (s >> 8) & 0x80
	if sign != 0 {
		s = -s
	}
	if s > clip {
		s = clip
	}
	s += bias

	exponent := 7
	for mask := 0x4000; exponent > 0 && s&mask == 0; mask >>= 1 {
		exponent--
	}
	mantissa := (s >> (exponent + 3)) & 0x0f
	return byte(^(sign | (exponent << 4) | mantissa))
}

// DecodeULaw converts one G.711 μ-law byte to a 16-bit linear sample.
func DecodeULaw(u byte) int16 {
	u = ^u
	sign := u & 0x80
	exponent := (u >> 4) & 0x07
	mantissa := u & 0x0f
	sample := ((int(mantissa) << 3) + 0x84) << exponent
	sample -= 0x84
	if sign != 0 {
		return int16(-sample)
	}
	return int16(sample)
}

// EncodeULawFrame resamples 16 kHz PCM to 8 kHz and encodes it as μ-law.
func EncodeULawFrame(pcm16k []int16) []byte {
	pcm8k := Resample(pcm16k, SampleRate16k, SampleRate8k)
	out := make([]byte, len(pcm8k))
	for i, s := range pcm8k {
		out[i] = EncodeULaw(s)
	}
	return out
}

// DecodeULawFrame decodes μ-law payload to 16-bit PCM at 8 kHz.
func DecodeULawFrame(payload []byte) []int16 {
	out := make([]int16, len(payload))
	for i, b := range payload {
		out[i] = DecodeULaw(b)
	}
	return out
}

// Resample converts PCM between sample rates with linear interpolation. A 2:1
// decimation averages sample pairs so it does not alias as badly as dropping
// every second sample.
func Resample(in []int16, from, to int) []int16 {
	if from <= 0 || to <= 0 || from == to || len(in) == 0 {
		out := make([]int16, len(in))
		copy(out, in)
		return out
	}
	if to*2 == from {
		out := make([]int16, 0, len(in)/2)
		for i := 0; i+1 < len(in); i += 2 {
			out = append(out, int16((int32(in[i])+int32(in[i+1]))/2))
		}
		return out
	}

	outLen := int(math.Round(float64(len(in)) * float64(to) / float64(from)))
	if outLen <= 0 {
		return nil
	}
	out := make([]int16, outLen)
	step := float64(from) / float64(to)
	for i := range out {
		pos := float64(i) * step
		idx := int(pos)
		frac := pos - float64(idx)
		if idx+1 < len(in) {
			out[i] = int16(float64(in[idx])*(1-frac) + float64(in[idx+1])*frac)
		} else {
			out[i] = in[idx]
		}
	}
	return out
}

// rms returns the root-mean-square energy of a PCM frame.
func rms(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}
