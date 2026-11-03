package voiceagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/devopshaven/gobelisk/pkg/config"
)

const (
	userRole      = "user"
	assistantRole = "assistant"
)

// Speaker plays synthesized speech into the meeting.
type Speaker interface {
	WritePCM(ctx context.Context, speech Speech) error
}

// Pipeline is the conversation loop: VAD -> ASR -> LLM -> TTS -> speaker.
type Pipeline struct {
	cfg config.VoiceAgentConfig
	log zerolog.Logger

	asr   ASR
	llm   LLM
	tts   TTS
	vad   *VAD
	speak Speaker

	transcript *Transcript
	exporter   Exporter

	utterances chan []int16

	mu          sync.Mutex
	history     []Message
	speakCancel context.CancelFunc
	speakGen    uint64

	closed sync.Once
}

// NewPipeline builds the default local-GPU pipeline from configuration.
// NewPipeline builds the pipeline from an already-defaulted configuration.
// Call config.VoiceAgentConfig.ApplyDefaults first if fields may be empty.
func NewPipeline(cfg config.VoiceAgentConfig, log zerolog.Logger) *Pipeline {
	var tts TTS
	if strings.TrimSpace(cfg.TTS.Command) != "" {
		tts = NewCommandTTS(cfg.TTS)
	} else {
		tts = NewOpenAITTS(cfg.TTS)
	}

	return &Pipeline{
		cfg:        cfg,
		log:        log,
		asr:        NewHTTPASR(cfg.ASR),
		llm:        NewOpenAILLM(cfg.LLM),
		tts:        tts,
		vad:        NewVAD(cfg.VAD),
		transcript: NewTranscript(cfg.MeetingSessionID),
		utterances: make(chan []int16, 16),
	}
}

// SetSpeaker connects the pipeline to an audio sink.
func (p *Pipeline) SetSpeaker(speaker Speaker) { p.speak = speaker }

// SetExporter installs a transcript exporter (for example the Pir² recorder).
func (p *Pipeline) SetExporter(exporter Exporter) { p.exporter = exporter }

// AddTool registers a function the LLM may call.
func (p *Pipeline) AddTool(tool Tool) {
	if llm, ok := p.llm.(*OpenAILLM); ok {
		llm.WithTools(tool)
	}
}

// Transcript returns the conversation record.
func (p *Pipeline) Transcript() *Transcript { return p.transcript }

// HandlePCM receives decoded meeting audio. It is safe to call from the SFU
// read goroutine and never blocks on inference.
func (p *Pipeline) HandlePCM(pcm16k []int16) {
	utterances, speechStarted := p.vad.Push(pcm16k)
	if speechStarted {
		// Barge-in: stop the current spoken answer immediately.
		p.interrupt()
	}
	for _, utterance := range utterances {
		if len(utterance) == 0 {
			continue
		}
		select {
		case p.utterances <- utterance:
		default:
			p.log.Warn().Msg("voice agent: utterance queue full, dropping turn")
		}
	}
}

// Run processes utterances until ctx is cancelled.
func (p *Pipeline) Run(ctx context.Context) {
	if p.speak == nil {
		p.log.Warn().Msg("voice agent: no speaker configured")
	}

	if greeting := strings.TrimSpace(p.cfg.Greeting); greeting != "" && p.speak != nil {
		p.say(ctx, greeting, 0)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case utterance := <-p.utterances:
			p.handleUtterance(ctx, utterance)
		}
	}
}

func (p *Pipeline) handleUtterance(ctx context.Context, pcm []int16) {
	asrCtx, cancel := context.WithTimeout(ctx, p.cfg.ASR.Timeout())
	transcript, err := p.asr.Transcribe(asrCtx, pcm)
	cancel()
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			p.log.Warn().Err(err).Msg("voice agent: transcription failed")
		}
		return
	}
	text := strings.TrimSpace(transcript.Text)
	if text == "" {
		return
	}
	p.log.Info().Str("text", text).Int64("asr_ms", transcript.LatencyMS).Msg("voice agent: heard")
	p.transcript.Add(userRole, text, transcript.LatencyMS)

	p.mu.Lock()
	p.history = append(p.history, Message{Role: userRole, Content: text})
	p.trimHistoryLocked()
	history := append([]Message(nil), p.history...)
	p.history = history
	p.mu.Unlock()

	llmCtx, llmCancel := context.WithTimeout(ctx, p.cfg.LLM.Timeout())
	reply, err := p.llm.Chat(llmCtx, p.cfg.SystemPrompt, history)
	llmCancel()
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			p.log.Warn().Err(err).Msg("voice agent: llm failed")
		}
		return
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}

	p.mu.Lock()
	p.history = append(p.history, Message{Role: assistantRole, Content: reply})
	p.trimHistoryLocked()
	p.mu.Unlock()

	p.say(ctx, reply, 0)
}

func (p *Pipeline) say(ctx context.Context, text string, latencyMS int64) {
	if p.speak == nil {
		p.transcript.Add(assistantRole, text, latencyMS)
		return
	}

	ttsCtx, cancel := context.WithTimeout(ctx, p.cfg.TTS.Timeout())
	speech, err := p.tts.Synthesize(ttsCtx, text)
	cancel()
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			p.log.Warn().Err(err).Msg("voice agent: tts failed")
		}
		p.transcript.Add(assistantRole, text, latencyMS)
		return
	}

	playCtx, playCancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.speakGen++
	gen := p.speakGen
	p.speakCancel = playCancel
	p.mu.Unlock()

	err = p.speak.WritePCM(playCtx, speech)
	playCancel()

	p.mu.Lock()
	if p.speakGen == gen {
		p.speakCancel = nil
	}
	p.mu.Unlock()

	if err != nil && !errors.Is(err, context.Canceled) {
		p.log.Warn().Err(err).Msg("voice agent: playback failed")
	}
	p.transcript.Add(assistantRole, text, latencyMS)
}

// interrupt cancels the currently playing answer, if any.
func (p *Pipeline) interrupt() {
	p.mu.Lock()
	cancel := p.speakCancel
	p.speakGen++
	p.speakCancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *Pipeline) trimHistoryLocked() {
	// Keep the system prompt out of history (it is passed separately) and
	// bound the context sent to the LLM.
	max := p.cfg.MaxHistory
	if max <= 0 {
		max = 20
	}
	if len(p.history) > max {
		p.history = p.history[len(p.history)-max:]
	}
}

// Close persists the transcript locally and exports it when configured.
func (p *Pipeline) Close(ctx context.Context) {
	p.closed.Do(func() {
		path, err := p.transcript.Save(p.cfg.Transcript.Dir)
		if err != nil {
			p.log.Warn().Err(err).Msg("voice agent: save transcript")
		} else {
			p.log.Info().Str("path", path).Msg("voice agent: transcript saved")
		}

		if p.exporter == nil {
			return
		}
		exportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := p.exporter.Export(exportCtx, p.cfg.MeetingSessionID, p.transcript.Entries(), path); err != nil {
			p.log.Warn().Err(err).Msg("voice agent: export transcript")
		} else {
			p.log.Info().Msg("voice agent: transcript exported")
		}
	})
}

// RecorderExporterFromConfig builds the Pir² recorder exporter.
func RecorderExporterFromConfig(cfg config.VoiceAgentConfig) *RecorderExporter {
	if strings.TrimSpace(cfg.Transcript.RecorderURL) == "" {
		return nil
	}
	return &RecorderExporter{
		BaseURL:     cfg.Transcript.RecorderURL,
		Token:       cfg.Transcript.RecorderToken,
		RecordingID: cfg.Transcript.RecordingID,
		Language:    cfg.Transcript.Language,
	}
}

func nowMillis() int64 { return time.Now().UnixMilli() }

// String describes the configured agent for logs.
func (p *Pipeline) String() string {
	return fmt.Sprintf("voice-agent(uid=%s, session=%s)", p.cfg.UID, p.cfg.MeetingSessionID)
}
