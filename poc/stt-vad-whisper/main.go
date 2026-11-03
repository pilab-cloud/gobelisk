package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Config holds configuration parameters for VAD and ASR
type Config struct {
	SampleRate       int     // 16000 Hz
	FrameDurationMs  int     // 20 ms frames (320 samples per frame)
	VadEnergyThresh  float64 // Energy RMS threshold for simple VAD (or silence detection)
	SilenceTimeoutMs int     // Silence duration to trigger endpointing (e.g. 400ms)
	MinSpeechMs      int     // Minimum speech duration to bother transcribing (e.g. 300ms)
	ASRServerURL     string  // Python faster-whisper backend endpoint
	ListenAddr       string  // HTTP/WebSocket listen address
}

// UtteranceResult represents the JSON response sent to IVR/client
type UtteranceResult struct {
	Timestamp   string  `json:"timestamp"`
	DurationSec float64 `json:"duration_sec"`
	Text        string  `json:"text"`
	LatencyMs   int64   `json:"latency_ms"`
}

// VADState tracks speech vs silence
type VADState int

const (
	StateSilence VADState = iota
	StateSpeech
)

// AudioBuffer handles frame ingestion, silence endpointing, and flushing
type AudioBuffer struct {
	mu            sync.Mutex
	cfg           Config
	state         VADState
	speechBuffer  []byte
	silenceFrames int
	speechFrames  int
	onUtterance   func(audioPCM []byte, durationSec float64)
}

func NewAudioBuffer(cfg Config, onUtterance func(audioPCM []byte, durationSec float64)) *AudioBuffer {
	return &AudioBuffer{
		cfg:         cfg,
		state:       StateSilence,
		onUtterance: onUtterance,
	}
}

// Calculate RMS (Root Mean Square) energy of 16-bit linear PCM
func calculateRMS(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var sum float64
	sampleCount := len(pcm) / 2
	for i := 0; i < len(pcm); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		sum += float64(sample) * float64(sample)
	}
	return math.Sqrt(sum / float64(sampleCount))
}

// ProcessFrame receives a chunk of PCM (e.g. 20ms = 640 bytes for 16kHz 16-bit mono)
func (ab *AudioBuffer) ProcessFrame(pcmChunk []byte) {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	rms := calculateRMS(pcmChunk)
	isVoice := rms > ab.cfg.VadEnergyThresh

	frameDuration := ab.cfg.FrameDurationMs
	silenceLimitFrames := ab.cfg.SilenceTimeoutMs / frameDuration
	minSpeechFrames := ab.cfg.MinSpeechMs / frameDuration

	switch ab.state {
	case StateSilence:
		if isVoice {
			// Transition to speech
			ab.state = StateSpeech
			ab.speechFrames = 1
			ab.silenceFrames = 0
			ab.speechBuffer = append(ab.speechBuffer[:0], pcmChunk...)
		}
	case StateSpeech:
		ab.speechBuffer = append(ab.speechBuffer, pcmChunk...)
		if isVoice {
			ab.silenceFrames = 0
			ab.speechFrames++
		} else {
			ab.silenceFrames++
			if ab.silenceFrames >= silenceLimitFrames {
				// Endpoint reached!
				if ab.speechFrames >= minSpeechFrames {
					durSec := float64(len(ab.speechBuffer)) / float64(ab.cfg.SampleRate*2)
					bufCopy := make([]byte, len(ab.speechBuffer))
					copy(bufCopy, ab.speechBuffer)
					go ab.onUtterance(bufCopy, durSec)
				}
				// Reset state
				ab.state = StateSilence
				ab.speechBuffer = ab.speechBuffer[:0]
				ab.speechFrames = 0
				ab.silenceFrames = 0
			}
		}
	}
}

// Flush forces any pending speech buffer to be transcribed
func (ab *AudioBuffer) Flush() {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	if ab.state == StateSpeech && len(ab.speechBuffer) > 0 {
		minSpeechFrames := ab.cfg.MinSpeechMs / ab.cfg.FrameDurationMs
		if ab.speechFrames >= minSpeechFrames {
			durSec := float64(len(ab.speechBuffer)) / float64(ab.cfg.SampleRate*2)
			bufCopy := make([]byte, len(ab.speechBuffer))
			copy(bufCopy, ab.speechBuffer)
			go ab.onUtterance(bufCopy, durSec)
		}
		ab.state = StateSilence
		ab.speechBuffer = ab.speechBuffer[:0]
	}
}

// Call ASR backend via HTTP POST with PCM audio
func transcribeAudio(asrURL string, pcmData []byte) (string, error) {
	resp, err := http.Post(asrURL, "application/octet-stream", bytes.NewReader(pcmData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("server error (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Text, nil
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	mode := flag.String("mode", "simulate", "Mode: 'simulate' (simulate RTP stream from wav/pcm file) or 'server' (WebSocket/HTTP audio receiver)")
	inputFile := flag.String("input", "", "Path to wav or pcm file for simulation (16kHz 16-bit mono or any format converted via ffmpeg)")
	asrURL := flag.String("asr-url", "http://127.0.0.1:8000/transcribe", "Whisper ASR HTTP backend")
	listenAddr := flag.String("listen", ":8088", "WebSocket receiver port")
	silenceMs := flag.Int("silence-ms", 400, "VAD silence endpointing threshold in ms")
	energyThresh := flag.Float64("energy-thresh", 500.0, "RMS energy threshold for silence/speech boundary")
	flag.Parse()

	cfg := Config{
		SampleRate:       16000,
		FrameDurationMs:  20,
		VadEnergyThresh:  *energyThresh,
		SilenceTimeoutMs: *silenceMs,
		MinSpeechMs:      300,
		ASRServerURL:     *asrURL,
		ListenAddr:       *listenAddr,
	}

	onUtterance := func(pcm []byte, durSec float64) {
		start := time.Now()
		text, err := transcribeAudio(cfg.ASRServerURL, pcm)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			log.Printf("[VAD Endpoint] Flush %.2fs PCM -> ASR Error: %v", durSec, err)
			return
		}
		if text == "" {
			return
		}
		log.Printf("\n=== [IVR DISPATCH] ===")
		log.Printf("Beszéd hossza: %.2f sec", durSec)
		log.Printf("Whisper leirat: %q", text)
		log.Printf("Feldolgozási idő (ASR Latency): %d ms", latency)
		log.Printf("=====================\n")
	}

	if *mode == "simulate" {
		if *inputFile == "" {
			log.Fatal("Kérlek adj meg egy bemeneti hangfájlt a -input paraméterrel (pl: -input test.wav)")
		}
		runSimulation(cfg, *inputFile, onUtterance)
	} else {
		runWSServer(cfg, onUtterance)
	}
}

func runSimulation(cfg Config, filePath string, onUtterance func([]byte, float64)) {
	log.Printf("--- RTP / WebRTC Stream Szimuláció indul ---")
	log.Printf("Fájl: %s | Endpointing csend: %d ms | VAD küszöb: %.1f", filePath, cfg.SilenceTimeoutMs, cfg.VadEnergyThresh)

	// Convert any audio file to raw 16kHz 16-bit mono PCM stream using ffmpeg
	cmd := exec.Command("ffmpeg", "-i", filePath, "-ar", "16000", "-ac", "1", "-f", "s16le", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("ffmpeg hiba: %v", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatalf("ffmpeg indítási hiba: %v", err)
	}

	audioBuffer := NewAudioBuffer(cfg, onUtterance)
	// 20ms frame = 16000 * 0.02 = 320 samples = 640 bytes
	frameSize := (cfg.SampleRate * cfg.FrameDurationMs / 1000) * 2
	frame := make([]byte, frameSize)

	ticker := time.NewTicker(time.Duration(cfg.FrameDurationMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		n, err := io.ReadFull(stdout, frame)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			log.Printf("Olvasási hiba: %v", err)
			break
		}
		if n == frameSize {
			audioBuffer.ProcessFrame(frame)
		}
		// Simulate real-time 20ms arrival pace
		<-ticker.C
	}

	audioBuffer.Flush()
	_ = cmd.Wait()
	log.Println("Szimuláció véget ért. Várakozás az utolsó leiratozásra...")
	time.Sleep(2 * time.Second)
}

func runWSServer(cfg Config, onUtterance func([]byte, float64)) {
	http.HandleFunc("/ws/audio", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		log.Printf("Új kliens csatlakozott az audio streamhez: %s", conn.RemoteAddr())
		buf := NewAudioBuffer(cfg, func(pcm []byte, dur float64) {
			start := time.Now()
			text, err := transcribeAudio(cfg.ASRServerURL, pcm)
			latency := time.Since(start).Milliseconds()
			res := UtteranceResult{
				Timestamp:   time.Now().Format(time.RFC3339),
				DurationSec: dur,
				Text:        text,
				LatencyMs:   latency,
			}
			if err == nil && text != "" {
				conn.WriteJSON(res)
			}
			onUtterance(pcm, dur)
		})

		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				buf.Flush()
				break
			}
			if messageType == websocket.BinaryMessage {
				buf.ProcessFrame(data)
			}
		}
	})

	log.Printf("WebSocket ASR Audio szerver figyel: ws://%s/ws/audio", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, nil))
}
