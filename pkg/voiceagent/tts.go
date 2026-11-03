package voiceagent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os/exec"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/config"
)

// Speech is synthesized audio in 16-bit mono PCM.
type Speech struct {
	PCM        []int16
	SampleRate int
}

// TTS turns text into speech.
type TTS interface {
	Synthesize(ctx context.Context, text string) (Speech, error)
}

// OpenAITTS calls an OpenAI-compatible /v1/audio/speech endpoint. This works
// with GPU servers such as Kokoro-FastAPI and openedai-speech (XTTS).
type OpenAITTS struct {
	endpoint   string
	apiKey     string
	model      string
	voice      string
	format     string
	sampleRate int
	client     *http.Client
}

// NewOpenAITTS builds the HTTP TTS client.
func NewOpenAITTS(cfg config.VoiceAgentTTSConfig) *OpenAITTS {
	return &OpenAITTS{
		endpoint:   cfg.Endpoint,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		voice:      cfg.Voice,
		format:     cfg.Format,
		sampleRate: cfg.SampleRate,
		client:     &http.Client{Timeout: cfg.Timeout()},
	}
}

// Synthesize implements TTS.
func (t *OpenAITTS) Synthesize(ctx context.Context, text string) (Speech, error) {
	payload := map[string]any{
		"model":           t.model,
		"input":           text,
		"voice":           t.voice,
		"response_format": t.format,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Speech{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return Speech{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/*")
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return Speech{}, fmt.Errorf("tts request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Speech{}, fmt.Errorf("tts read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Speech{}, fmt.Errorf("tts: %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return decodeSpeech(raw, t.format, t.sampleRate)
}

// CommandTTS runs an external synthesizer (for example piper) and reads WAV or
// raw PCM from stdout. Text is written to stdin.
type CommandTTS struct {
	command    string
	format     string
	sampleRate int
}

// NewCommandTTS builds a command-backed synthesizer.
func NewCommandTTS(cfg config.VoiceAgentTTSConfig) *CommandTTS {
	cmdStr := cfg.Command
	if strings.TrimSpace(cmdStr) == "piper" {
		model := cfg.ModelPath
		if model == "" && cfg.Voice != "" {
			model = "/opt/piper/models/hu_HU-" + cfg.Voice + "-medium.onnx"
		}
		if model != "" {
			cmdStr = fmt.Sprintf("piper --model %s --output-raw", model)
		}
	}
	format := cfg.Format
	if format == "" {
		format = "pcm"
	}
	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 22050
	}
	return &CommandTTS{command: cmdStr, format: format, sampleRate: sampleRate}
}

// Synthesize implements TTS.
func (t *CommandTTS) Synthesize(ctx context.Context, text string) (Speech, error) {
	fields := strings.Fields(t.command)
	if len(fields) == 0 {
		return Speech{}, fmt.Errorf("tts command is empty")
	}
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmd.Stdin = strings.NewReader(text)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Speech{}, fmt.Errorf("tts command: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return decodeSpeech(stdout.Bytes(), t.format, t.sampleRate)
}

func decodeSpeech(raw []byte, format string, fallbackRate int) (Speech, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "wav" || (len(raw) >= 12 && string(raw[0:4]) == "RIFF" && string(raw[8:12]) == "WAVE") {
		return DecodeWAV(raw)
	}
	rate := fallbackRate
	if rate <= 0 {
		rate = 24000
	}
	return Speech{PCM: BytesToPCM16LE(raw), SampleRate: rate}, nil
}

// DecodeWAV decodes a PCM or IEEE-float WAV file to mono 16-bit samples.
func DecodeWAV(data []byte) (Speech, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return Speech{}, fmt.Errorf("tts: not a WAV file")
	}

	var (
		audioFormat uint16
		channels    uint16
		sampleRate  uint32
		bits        uint16
		pcm         []byte
	)

	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4:]))
		body := off + 8
		end := body + size
		if end > len(data) {
			end = len(data)
		}

		switch id {
		case "fmt ":
			if size < 16 || body+16 > len(data) {
				return Speech{}, fmt.Errorf("tts: invalid fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(data[body:])
			channels = binary.LittleEndian.Uint16(data[body+2:])
			sampleRate = binary.LittleEndian.Uint32(data[body+4:])
			bits = binary.LittleEndian.Uint16(data[body+14:])
		case "data":
			pcm = data[body:end]
		}
		off = end + (size & 1)
	}

	if channels == 0 || sampleRate == 0 {
		return Speech{}, fmt.Errorf("tts: missing fmt chunk")
	}

	var samples []int16
	switch {
	case audioFormat == 1 && bits == 16:
		samples = BytesToPCM16LE(pcm)
	case audioFormat == 3 && bits == 32:
		samples = make([]int16, len(pcm)/4)
		for i := range samples {
			bits := binary.LittleEndian.Uint32(pcm[i*4:])
			f := math.Float32frombits(bits)
			samples[i] = int16(math.Max(-1, math.Min(1, float64(f))) * 32767)
		}
	case audioFormat == 1 && bits == 8:
		samples = make([]int16, len(pcm))
		for i, b := range pcm {
			samples[i] = int16(int(b)-128) << 8
		}
	default:
		return Speech{}, fmt.Errorf("tts: unsupported WAV format %d/%d-bit", audioFormat, bits)
	}

	if channels > 1 {
		mono := make([]int16, len(samples)/int(channels))
		for i := range mono {
			var sum int32
			for c := 0; c < int(channels); c++ {
				sum += int32(samples[i*int(channels)+c])
			}
			mono[i] = int16(sum / int32(channels))
		}
		samples = mono
	}

	return Speech{PCM: samples, SampleRate: int(sampleRate)}, nil
}
