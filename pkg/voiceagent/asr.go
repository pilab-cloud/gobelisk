package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/config"
)

// ASR turns one utterance into text.
type ASR interface {
	Transcribe(ctx context.Context, pcm16k []int16) (Recognition, error)
}

// Recognition is the result of one speech-to-text call.
type Recognition struct {
	Text       string `json:"text"`
	Language   string `json:"language,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
}

// HTTPASR posts raw 16 kHz s16le PCM to a faster-whisper compatible endpoint.
// This is the same contract the Pir² recorder uses (RECORDER_ASR_URL).
type HTTPASR struct {
	endpoint string
	language string
	client   *http.Client
}

// NewHTTPASR builds an ASR client. The endpoint may include a language query
// parameter; the configured language is also sent as a header.
func NewHTTPASR(cfg config.VoiceAgentASRConfig) *HTTPASR {
	return &HTTPASR{
		endpoint: cfg.Endpoint,
		language: cfg.Language,
		client:   &http.Client{Timeout: cfg.Timeout()},
	}
}

// Transcribe implements ASR.
func (a *HTTPASR) Transcribe(ctx context.Context, pcm16k []int16) (Recognition, error) {
	start := nowMillis()
	body := PCM16LEBytes(pcm16k)
	endpoint := a.endpoint
	if a.language != "" {
		u, err := url.Parse(endpoint)
		if err == nil {
			q := u.Query()
			if q.Get("language") == "" {
				q.Set("language", a.language)
				u.RawQuery = q.Encode()
				endpoint = u.String()
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Recognition{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	if a.language != "" {
		req.Header.Set("X-Language", a.language)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return Recognition{}, fmt.Errorf("asr request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Recognition{}, fmt.Errorf("asr read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Recognition{}, fmt.Errorf("asr: %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	out := Recognition{}
	if json.Unmarshal(raw, &out) != nil {
		// Plain-text ASR servers are also accepted.
		out.Text = strings.TrimSpace(string(raw))
	}
	out.Text = strings.TrimSpace(out.Text)
	if out.Language == "" {
		out.Language = a.language
	}
	if out.LatencyMS == 0 {
		out.LatencyMS = nowMillis() - start
	}
	if out.DurationMS == 0 {
		out.DurationMS = int64((len(pcm16k)*1000 + SampleRate16k - 1) / SampleRate16k)
	}
	return out, nil
}
