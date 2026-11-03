package voiceagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/devopshaven/gobelisk/pkg/config"
)

func testConfig() config.VoiceAgentConfig {
	cfg := config.VoiceAgentConfig{MeetingSessionID: "session-1", UID: "agent", DisplayName: "Agent"}
	cfg.ApplyDefaults()
	cfg.VAD.EnergyThreshold = 500
	cfg.VAD.MinSpeechMS = 200
	cfg.VAD.SilenceMS = 300
	cfg.VAD.PreRollMS = 100
	cfg.Greeting = ""
	return cfg
}

func TestULawRoundTrip(t *testing.T) {
	for _, sample := range []int16{0, 1, -1, 1000, -1000, 30000, -30000, 32767, -32768} {
		got := DecodeULaw(EncodeULaw(sample))
		diff := int(sample) - int(got)
		if diff < 0 {
			diff = -diff
		}
		if diff > 800 {
			t.Errorf("μ-law round trip %d -> %d (diff %d)", sample, got, diff)
		}
	}
}

func TestResample(t *testing.T) {
	in := []int16{0, 100, 200, 300}
	got := Resample(in, 16000, 8000)
	if len(got) != 2 || got[0] != 50 || got[1] != 250 {
		t.Errorf("16k->8k = %v, want [50 250]", got)
	}
	back := Resample(got, 8000, 16000)
	if len(back) != 4 {
		t.Errorf("8k->16k length = %d, want 4", len(back))
	}
}

func toneFrame(level int16) []int16 {
	frame := make([]int16, vadFrameSamples)
	for i := range frame {
		if i%2 == 0 {
			frame[i] = level
		} else {
			frame[i] = -level
		}
	}
	return frame
}

func TestVADSegment(t *testing.T) {
	vad := NewVAD(config.VoiceAgentVADConfig{
		EnergyThreshold: 500,
		SilenceMS:       300,
		MinSpeechMS:     200,
		PreRollMS:       100,
	})

	var utterances [][]int16
	var started int
	// 300 ms silence, 400 ms speech, 500 ms silence.
	for i := 0; i < 15; i++ {
		u, s := vad.Push(toneFrame(0))
		if s {
			started++
		}
		utterances = append(utterances, u...)
	}
	for i := 0; i < 20; i++ {
		u, s := vad.Push(toneFrame(4000))
		if s {
			started++
		}
		utterances = append(utterances, u...)
	}
	for i := 0; i < 25; i++ {
		u, s := vad.Push(toneFrame(0))
		if s {
			started++
		}
		utterances = append(utterances, u...)
	}

	if started != 1 {
		t.Errorf("speech starts = %d, want 1", started)
	}
	if len(utterances) != 1 {
		t.Fatalf("utterances = %d, want 1", len(utterances))
	}
	// Speech plus pre-roll plus a short tail.
	if got := len(utterances[0]); got < 400*16 {
		t.Errorf("utterance samples = %d, want at least 6400", got)
	}
}

func TestVADDropsShortBlip(t *testing.T) {
	vad := NewVAD(config.VoiceAgentVADConfig{
		EnergyThreshold: 500,
		SilenceMS:       100,
		MinSpeechMS:     300,
		PreRollMS:       50,
	})
	var utterances [][]int16
	for i := 0; i < 3; i++ { // 60 ms of speech
		u, _ := vad.Push(toneFrame(4000))
		utterances = append(utterances, u...)
	}
	for i := 0; i < 10; i++ {
		u, _ := vad.Push(toneFrame(0))
		utterances = append(utterances, u...)
	}
	if len(utterances) != 0 {
		t.Errorf("short blip produced %d utterances", len(utterances))
	}
}

func TestHTTPASR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		if r.URL.Query().Get("language") != "hu" {
			t.Errorf("language = %q", r.URL.Query().Get("language"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"text": " jó napot ", "language": "hu"})
	}))
	defer srv.Close()

	asr := NewHTTPASR(config.VoiceAgentASRConfig{Endpoint: srv.URL, Language: "hu", TimeoutSeconds: 5})
	got, err := asr.Transcribe(context.Background(), []int16{1, 2, 3})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got.Text != "jó napot" {
		t.Errorf("text = %q", got.Text)
	}
	if got.DurationMS == 0 {
		t.Error("duration not set")
	}
}

type fakeLLMServer struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeLLMServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()

	if call == 1 {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id":   "call-1",
					"type": "function",
					"function": map[string]any{
						"name":      "lookup",
						"arguments": `{"key":"value"}`,
					},
				}},
			}}},
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "the answer"}}},
	})
}

func TestOpenAILLMToolLoop(t *testing.T) {
	fake := &fakeLLMServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()

	llm := NewOpenAILLM(config.VoiceAgentLLMConfig{Endpoint: srv.URL, Model: "test", MaxTokens: 16, TimeoutSeconds: 5})
	var gotArgs string
	llm.WithTools(Tool{
		Name:       "lookup",
		Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			gotArgs = string(args)
			return `{"ok":true}`, nil
		},
	})

	reply, err := llm.Chat(context.Background(), "system", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply != "the answer" {
		t.Errorf("reply = %q", reply)
	}
	if gotArgs != `{"key":"value"}` {
		t.Errorf("tool args = %q", gotArgs)
	}
	if fake.calls != 2 {
		t.Errorf("calls = %d, want 2", fake.calls)
	}
}

func makeWAV(samples []int16, rate int) []byte {
	data := PCM16LEBytes(samples)
	size := 36 + len(data)
	out := make([]byte, 44+len(data))
	copy(out[0:4], "RIFF")
	put32(out[4:], uint32(size))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	put32(out[16:], 16)
	put16(out[20:], 1)
	put16(out[22:], 1)
	put32(out[24:], uint32(rate))
	put32(out[28:], uint32(rate*2))
	put16(out[32:], 2)
	put16(out[34:], 16)
	copy(out[36:40], "data")
	put32(out[40:], uint32(len(data)))
	copy(out[44:], data)
	return out
}

func put16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func put32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func TestOpenAITTSWAV(t *testing.T) {
	wav := makeWAV([]int16{100, -100, 200, -200}, 24000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer srv.Close()

	tts := NewOpenAITTS(config.VoiceAgentTTSConfig{Endpoint: srv.URL, Model: "xtts", Voice: "hu", Format: "wav", TimeoutSeconds: 5})
	speech, err := tts.Synthesize(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if speech.SampleRate != 24000 {
		t.Errorf("sample rate = %d", speech.SampleRate)
	}
	if len(speech.PCM) != 4 || speech.PCM[0] != 100 {
		t.Errorf("pcm = %v", speech.PCM)
	}
}

func TestTranscriptSave(t *testing.T) {
	tr := NewTranscript("session/with spaces")
	tr.Add("user", "hello", 10)
	tr.Add("assistant", "hi there", 20)

	dir := t.TempDir()
	path, err := tr.Save(dir)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("path = %q", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "User: hello") || !strings.Contains(string(raw), "Assistant: hi there") {
		t.Errorf("transcript = %s", raw)
	}
	if !strings.Contains(filepath.Base(path), "session_with_spaces") {
		t.Errorf("unsafe filename: %s", path)
	}
}

func TestRecorderExporter(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var createBody map[string]string

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&createBody)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"txn-1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	exp := &RecorderExporter{BaseURL: srv.URL, Language: "hu"}
	entries := []Entry{{At: time.Now(), Role: "user", Text: "hello"}}
	if err := exp.Export(context.Background(), "session-1", entries, "/tmp/t.txt"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/v1/transcriptions" {
		t.Fatalf("paths = %v", paths)
	}
	if createBody["recording_id"] != "agent-session-1" {
		t.Errorf("recording id = %q", createBody["recording_id"])
	}
	if !strings.Contains(createBody["transcript"], "hello") || createBody["transcript_uri"] != "/tmp/t.txt" {
		t.Errorf("create body = %v", createBody)
	}
}

type fakeASR struct{ text string }

func (f fakeASR) Transcribe(context.Context, []int16) (Recognition, error) {
	return Recognition{Text: f.text, LatencyMS: 5}, nil
}

type fakeLLM struct{ reply string }

func (f fakeLLM) Chat(context.Context, string, []Message) (string, error) { return f.reply, nil }

type fakeTTS struct{ rate int }

func (f fakeTTS) Synthesize(context.Context, string) (Speech, error) {
	return Speech{PCM: make([]int16, f.rate/10), SampleRate: f.rate}, nil
}

type fakeSpeaker struct {
	mu   sync.Mutex
	text int
	done chan struct{}
}

func (f *fakeSpeaker) WritePCM(ctx context.Context, speech Speech) error {
	f.mu.Lock()
	f.text += len(speech.PCM)
	f.mu.Unlock()
	select {
	case f.done <- struct{}{}:
	default:
	}
	return nil
}

func TestPipelineConversation(t *testing.T) {
	cfg := testConfig()
	p := NewPipeline(cfg, zerolog.Nop())
	p.asr = fakeASR{text: "mennyi az idő?"}
	p.llm = fakeLLM{reply: "Délután kettő."}
	p.tts = fakeTTS{rate: 16000}
	speaker := &fakeSpeaker{done: make(chan struct{}, 1)}
	p.SetSpeaker(speaker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	// 400 ms of speech followed by 400 ms of silence.
	for i := 0; i < 20; i++ {
		p.HandlePCM(toneFrame(4000))
	}
	for i := 0; i < 20; i++ {
		p.HandlePCM(toneFrame(0))
	}

	select {
	case <-speaker.done:
	case <-time.After(2 * time.Second):
		t.Fatal("assistant never spoke")
	}

	entries := p.Transcript().Entries()
	if len(entries) < 2 {
		t.Fatalf("entries = %v", entries)
	}
	if entries[0].Role != "user" || entries[0].Text != "mennyi az idő?" {
		t.Errorf("first entry = %+v", entries[0])
	}
	if entries[1].Role != "assistant" || entries[1].Text != "Délután kettő." {
		t.Errorf("second entry = %+v", entries[1])
	}
	cancel()
	p.Close(context.Background())
}

func TestPipelineBargeIn(t *testing.T) {
	cfg := testConfig()
	p := NewPipeline(cfg, zerolog.Nop())
	p.asr = fakeASR{text: "stop"}
	p.llm = fakeLLM{reply: "long answer"}
	p.tts = fakeTTS{rate: 16000}

	blocking := &blockingSpeaker{started: make(chan struct{}, 1), cancelled: make(chan struct{}, 1)}
	p.SetSpeaker(blocking)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	for i := 0; i < 20; i++ {
		p.HandlePCM(toneFrame(4000))
	}
	for i := 0; i < 20; i++ {
		p.HandlePCM(toneFrame(0))
	}

	select {
	case <-blocking.started:
	case <-time.After(2 * time.Second):
		t.Fatal("assistant never started speaking")
	}

	// New speech must cancel playback.
	for i := 0; i < 5; i++ {
		p.HandlePCM(toneFrame(4000))
	}
	select {
	case <-blocking.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("barge-in did not cancel playback")
	}
	cancel()
}

type blockingSpeaker struct {
	started   chan struct{}
	cancelled chan struct{}
}

func (b *blockingSpeaker) WritePCM(ctx context.Context, speech Speech) error {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	b.cancelled <- struct{}{}
	return ctx.Err()
}
