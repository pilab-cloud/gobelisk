package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/devopshaven/gobelisk/gen/go/gobelisk/v1"
	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/voicemail"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var webPort int

// CallState tracks a live mock/PBX call in the web tester
type WebCallSession struct {
	CallID         string          `json:"call_id"`
	From           string          `json:"from"`
	To             string          `json:"to"`
	StartTime      time.Time       `json:"start_time"`
	DTMFBuffer     string          `json:"dtmf_buffer"`
	Transcript     []string        `json:"transcript"`
	State          string          `json:"state"`       // idle, connected, ended
	AgentState     string          `json:"agent_state"` // standby, active
	LastActiveTime time.Time       `json:"last_active_time"`
	History        []OllamaMessage `json:"history"`
}

// containsWakeWord checks if the user's transcript contains the configured wake word or aliases (case-insensitive)
func containsWakeWord(text, wakeWord string, aliases []string) bool {
	cleanText := strings.ToLower(text)
	cleanWord := strings.ToLower(strings.TrimSpace(wakeWord))
	if cleanWord != "" && strings.Contains(cleanText, cleanWord) {
		return true
	}
	for _, alias := range aliases {
		a := strings.ToLower(strings.TrimSpace(alias))
		if a != "" && strings.Contains(cleanText, a) {
			return true
		}
	}
	return false
}

// stripWakeWord removes the wake word or matched alias and common punctuation / prefixes ("Szia Maci", "Maci,")
func stripWakeWord(text, wakeWord string, aliases []string) string {
	lower := strings.ToLower(text)
	candidates := append([]string{wakeWord}, aliases...)
	for _, cand := range candidates {
		cleanWord := strings.ToLower(strings.TrimSpace(cand))
		if cleanWord == "" {
			continue
		}
		idx := strings.Index(lower, cleanWord)
		if idx == -1 {
			continue
		}
		after := strings.TrimSpace(text[idx+len(cleanWord):])
		after = strings.TrimLeft(after, ",:!? -")
		if after != "" {
			return after
		}
		before := strings.TrimSpace(text[:idx])
		before = strings.TrimRight(before, ",:!? -")
		if before != "" {
			return before
		}
	}
	return text
}

var (
	callSessionsMu sync.Mutex
	activeCall     *WebCallSession
	wsUpgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

var (
	loadedConfig      *config.Config
	whisperServerCmd  *exec.Cmd
	whisperServerPort = 8099
)

func parsePortFromURL(rawURL string, defaultPort int) int {
	u, err := url.Parse(rawURL)
	if err == nil && u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
			return p
		}
	}
	return defaultPort
}

func startWhisperServerInMemory(asrCfg config.VoiceAgentASRConfig, wakeWord string) {
	modelPath := asrCfg.ModelPath
	if modelPath == "" {
		modelPath = "/opt/whisper/models/ggml-large-v3-turbo-q5_0.bin"
	}
	if _, err := os.Stat(modelPath); err != nil {
		fmt.Printf("⚠️ Whisper modell nem található: %s (fallback CLI mód)\n", modelPath)
		return
	}

	whisperBin := "whisper-server"
	var extraArgs []string
	if _, err := exec.LookPath("whisper-server-gpu"); err == nil {
		whisperBin = "whisper-server-gpu"
		dev := asrCfg.Device
		if dev == "" {
			dev = "1"
		}
		extraArgs = append(extraArgs, "-dev", dev)
		fmt.Printf("🚀 [ASR Engine] GPU Vulkan gyorsítás észlelve (device %s)\n", dev)
	}

	if wakeWord != "" {
		prompt := fmt.Sprintf("%s, %s, Szia %s. Gobelisk ügyfélszolgálat.", strings.Title(wakeWord), strings.ToLower(wakeWord), strings.Title(wakeWord))
		extraArgs = append(extraArgs, "--prompt", prompt)
	}

	whisperServerPort = parsePortFromURL(asrCfg.Endpoint, 8099)
	lang := asrCfg.Language
	if lang == "" {
		lang = "hu"
	}

	fmt.Printf("⏳ [ASR Engine] Whisper modell előtöltése a memóriába (%s, 4 threads, greedy mode)...\n", filepath.Base(modelPath))
	args := append([]string{
		"-m", modelPath,
		"-l", lang,
		"--port", fmt.Sprintf("%d", whisperServerPort),
		"--host", "127.0.0.1",
		"-t", "4",
		"-bs", "1",
		"-bo", "1",
		"-nf",
	}, extraArgs...)

	cmd := exec.Command(whisperBin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		fmt.Printf("⚠️ %s indítási hiba: %v\n", whisperBin, err)
		return
	}
	whisperServerCmd = cmd

	// Wait up to 5s for whisper-server to bind
	for i := 0; i < 25; i++ {
		time.Sleep(200 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", whisperServerPort))
		if err == nil {
			resp.Body.Close()
			fmt.Printf("✅ [ASR Engine] Whisper GPU modell sikeresen betöltve a memóriába (port: %d)!\n\n", whisperServerPort)
			return
		}
	}
	fmt.Println("⚠️ [ASR Engine] whisper-server időtúllépés, fallback CLI mód aktív")
}

func stopWhisperServerInMemory() {
	if whisperServerCmd != nil && whisperServerCmd.Process != nil {
		_ = whisperServerCmd.Process.Kill()
		_ = whisperServerCmd.Wait()
	}
}

func transcribeInMemory(wavPath string, endpoint string, prompt string) (string, error) {
	if endpoint == "" {
		endpoint = fmt.Sprintf("http://127.0.0.1:%d/inference", whisperServerPort)
	} else if !strings.HasSuffix(endpoint, "/inference") {
		endpoint = strings.TrimRight(endpoint, "/") + "/inference"
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	f, err := os.Open(wavPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	part, err := w.CreateFormFile("file", filepath.Base(wavPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	_ = w.WriteField("response_format", "json")
	_ = w.WriteField("temperature", "0.0")
	_ = w.WriteField("temperature_inc", "0.0")
	if prompt != "" {
		_ = w.WriteField("prompt", prompt)
	}
	w.Close()

	req, err := http.NewRequest("POST", endpoint, &b)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Text), nil
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "start web dial pad & interactive AI IVR tester",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Printf("⚠️ Config betöltési hiba: %v (alapértelmezett beállítások használata)\n", err)
			cfg = config.Default()
		}
		loadedConfig = cfg
		startWhisperServerInMemory(cfg.VoiceAgent.ASR, cfg.VoiceAgent.WakeWord.Word)
		defer stopWhisperServerInMemory()

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, dialPadHTML)
		})

		// API: Dial / Start Call
		http.HandleFunc("/api/dial", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			callSessionsMu.Lock()
			initAgentState := "active"
			if loadedConfig != nil && loadedConfig.VoiceAgent.WakeWord.Enabled {
				initAgentState = "standby"
			}
			activeCall = &WebCallSession{
				CallID:         fmt.Sprintf("call-%d", time.Now().UnixNano()),
				From:           req.From,
				To:             req.To,
				StartTime:      time.Now(),
				DTMFBuffer:     "",
				Transcript:     []string{"Hívás csatlakoztatva: " + req.To},
				State:          "connected",
				AgentState:     initAgentState,
				LastActiveTime: time.Now(),
			}
			callSessionsMu.Unlock()

			// Optionally notify gRPC if PBX is listening
			go func() {
				client, conn, err := dialRPC()
				if err == nil {
					defer conn.Close()
					_, _ = client.OriginateCall(withAuth(context.Background()), &gobeliskv1.OriginateCallRequest{From: req.From, To: req.To})
				}
			}()

			welcomeText := loadedConfig.VoiceAgent.Greeting
			if welcomeText == "" {
				welcomeText = "Üdvözlöm a Gobelisk telefonos ügyfélszolgálatán! Kérjük, válasszon menüpontot: 1-es gomb számlázás, 2-es gomb műszaki hibabejelentés, 3-as gomb értékesítés. Vagy csak egyszerűen beszéljen, figyeljük a vonalat!"
			}
			audioURL := ""
			welcomeFile := filepath.Join(os.TempDir(), "ivr_welcome_prompt.wav")
			if _, err := os.Stat(welcomeFile); os.IsNotExist(err) {
				_ = voicemail.TTSWithConfig(welcomeText, welcomeFile, loadedConfig.VoiceAgent.TTS.Command, loadedConfig.VoiceAgent.TTS.ModelPath, loadedConfig.VoiceAgent.TTS.Voice)
			}
			if _, err := os.Stat(welcomeFile); err == nil {
				audioURL = "/api/audio?file=" + filepath.Base(welcomeFile) + "&t=" + fmt.Sprintf("%d", time.Now().UnixNano())
			}

			callSessionsMu.Lock()
			if activeCall != nil {
				activeCall.Transcript = append(activeCall.Transcript, "IVR Menü: "+welcomeText)
				activeCall.History = append(activeCall.History, OllamaMessage{Role: "assistant", Content: welcomeText})
			}
			callSessionsMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"message":      "Hívás felépült",
				"call_id":      activeCall.CallID,
				"to":           req.To,
				"welcome_text": welcomeText,
				"audio_url":    audioURL,
			})
		})

		// API: Send DTMF digit
		http.HandleFunc("/api/dtmf", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Digit string `json:"digit"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}

			callSessionsMu.Lock()
			if activeCall == nil || activeCall.State != "connected" {
				activeCall = &WebCallSession{
					CallID:     fmt.Sprintf("call-%d", time.Now().UnixNano()),
					From:       "100",
					To:         "IVR",
					StartTime:  time.Now(),
					DTMFBuffer: "",
					State:      "connected",
				}
			}
			activeCall.DTMFBuffer += req.Digit
			activeCall.Transcript = append(activeCall.Transcript, fmt.Sprintf("DTMF gomb lenyomva: [%s]", req.Digit))
			currBuf := activeCall.DTMFBuffer
			callSessionsMu.Unlock()

			// DTMF IVR logic
			replyText := ""
			switch req.Digit {
			case "1":
				replyText = "Ön az 1-es gombot választotta: Számlázási és pénzügyi osztály. Miben segíthetek a számlájával kapcsolatban?"
			case "2":
				replyText = "Ön a 2-es gombot választotta: Műszaki segítségnyújtás és hibabejelentés. Kérem, mondja el a hiba részleteit!"
			case "3":
				replyText = "Ön a 3-as gombot választotta: Új szerződéskötés és értékesítés. Milyen termékünk iránt érdeklődik?"
			case "#":
				replyText = fmt.Sprintf("PIN vagy azonosító megerősítve: %s. Köszönjük!", currBuf)
			case "*":
				replyText = "Visszalépés a főmenübe. Kérjük válasszon menüpontot vagy mondja el kérdését!"
			default:
				replyText = fmt.Sprintf("A megadott opció [%s] rögzítve. Kérjük beszéljen nyugodtan!", req.Digit)
			}

			// Generate TTS audio with configured synthesizer
			audioURL := ""
			tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("ivr_resp_%d.wav", time.Now().UnixNano()))
			if err := voicemail.TTSWithConfig(replyText, tmpFile, loadedConfig.VoiceAgent.TTS.Command, loadedConfig.VoiceAgent.TTS.ModelPath, loadedConfig.VoiceAgent.TTS.Voice); err == nil {
				audioURL = "/api/audio?file=" + filepath.Base(tmpFile)
			}

			callSessionsMu.Lock()
			if activeCall != nil {
				activeCall.Transcript = append(activeCall.Transcript, "IVR: "+replyText)
			}
			callSessionsMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"digit":     req.Digit,
				"buffer":    currBuf,
				"reply":     replyText,
				"audio_url": audioURL,
			})
		})

		// WebSocket: Continuous Real-Time RTP / WebRTC PCM Stream with VAD Endpointing
		http.HandleFunc("/ws/audio", func(w http.ResponseWriter, r *http.Request) {
			conn, err := wsUpgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			var bufLock sync.Mutex
			var speechBuffer []byte
			var silenceFrames int
			var speechFrames int
			var isSpeaking bool
			var interimBusy atomic.Bool
			var wsWriteLock sync.Mutex

			safeWriteJSON := func(v any) {
				wsWriteLock.Lock()
				defer wsWriteLock.Unlock()
				_ = conn.WriteJSON(v)
			}

			vadCfg := loadedConfig.VoiceAgent.VAD
			energyThresh := vadCfg.EnergyThreshold
			if energyThresh <= 0 {
				energyThresh = 500.0
			}
			silenceMS := vadCfg.SilenceMS
			if silenceMS <= 0 {
				silenceMS = 640
			}
			silenceLimitFrames := int(math.Ceil(float64(silenceMS) / 128.0))

			minSpeechMS := vadCfg.MinSpeechMS
			if minSpeechMS <= 0 {
				minSpeechMS = 380
			}
			minSpeechFrames := int(math.Max(1, math.Ceil(float64(minSpeechMS)/128.0)))

			maxSpeechMS := vadCfg.MaxSpeechMS
			if maxSpeechMS <= 0 {
				maxSpeechMS = 5100
			}
			maxSpeechFrames := int(math.Ceil(float64(maxSpeechMS) / 128.0))
			const sampleRate = 16000

			flushUtterance := func(pcm []byte) {
				if len(pcm) < 16000*2/5 { // kevesebb mint 0.2s
					return
				}
				durSec := float64(len(pcm)) / float64(sampleRate*2)

				// Save PCM to temporary WAV
				tmpWav := filepath.Join(os.TempDir(), fmt.Sprintf("vad_flush_%d.wav", time.Now().UnixNano()))
				saveRawPCMToWav(pcm, sampleRate, tmpWav)
				defer os.Remove(tmpWav)

				// Step 1: Run In-Memory Whisper ASR (no re-loading of model from disk)
				t0 := time.Now()
				var userText string
				var err error

				whisperPrompt := ""
				if loadedConfig != nil && loadedConfig.VoiceAgent.WakeWord.Enabled {
					w := loadedConfig.VoiceAgent.WakeWord.Word
					whisperPrompt = fmt.Sprintf("%s, %s, Szia %s. Gobelisk ügyfélszolgálat.", strings.Title(w), strings.ToLower(w), strings.Title(w))
				}

				userText, err = transcribeInMemory(tmpWav, loadedConfig.VoiceAgent.ASR.Endpoint, whisperPrompt)
				if err != nil || strings.TrimSpace(userText) == "" {
					// Fallback to CLI
					userText, err = voicemail.TranscribeWithConfig(tmpWav, loadedConfig.VoiceAgent.ASR.ModelPath, loadedConfig.VoiceAgent.ASR.Device, loadedConfig.VoiceAgent.ASR.Language)
				}
				asrLatency := time.Since(t0).Milliseconds()

				if idx := strings.Index(userText, "\n"); idx != -1 && strings.Contains(userText[:idx], "backend") {
					userText = strings.TrimSpace(userText[idx+1:])
				}
				userText = strings.TrimSpace(userText)
				if userText == "" {
					fmt.Printf("[VAD] Utterance %.2fs -> No intelligible speech detected (Whisper: %dms)\n", durSec, asrLatency)
					return
				}

				// Wake word gating logic
				wakeCfg := loadedConfig.VoiceAgent.WakeWord
				if wakeCfg.Enabled {
					var isStandby bool
					callSessionsMu.Lock()
					if activeCall != nil {
						// Check if active session timed out back to standby
						if activeCall.AgentState == "active" && wakeCfg.TimeoutSeconds > 0 {
							if time.Since(activeCall.LastActiveTime) > time.Duration(wakeCfg.TimeoutSeconds)*time.Second {
								activeCall.AgentState = "standby"
								fmt.Printf("💤 [Wake Word] Inaktivitási időkorlát (%ds) lejárt -> Visszatérés STANDBY módba\n", wakeCfg.TimeoutSeconds)
							}
						}
						if activeCall.AgentState == "" || activeCall.AgentState == "standby" {
							isStandby = true
						}
					}
					callSessionsMu.Unlock()

					if isStandby {
						if !containsWakeWord(userText, wakeCfg.Word, wakeCfg.Aliases) {
							fmt.Printf("⏸️  [Wake Word Gate] STANDBY: %q eldobva (hívószó: %q hiányzik)\n", userText, wakeCfg.Word)
							safeWriteJSON(map[string]any{
								"event":       "wake_word_standby",
								"user_text":   userText,
								"wake_word":   wakeCfg.Word,
								"agent_state": "standby",
							})
							return // Discard! 0 LLM compute, 0 TTS compute!
						}

						// Wake word matched! Activate session
						callSessionsMu.Lock()
						if activeCall != nil {
							activeCall.AgentState = "active"
							activeCall.LastActiveTime = time.Now()
						}
						callSessionsMu.Unlock()
						fmt.Printf("🔔 [Wake Word Detected] Hívószó (%q) felismerve! Állapot: ACTIVE\n", wakeCfg.Word)
						safeWriteJSON(map[string]any{
							"event":       "wake_word_activated",
							"wake_word":   wakeCfg.Word,
							"agent_state": "active",
						})
					} else {
						// Already active: update last active timestamp
						callSessionsMu.Lock()
						if activeCall != nil {
							activeCall.LastActiveTime = time.Now()
						}
						callSessionsMu.Unlock()
					}
				}

				cleanUserText := userText
				if wakeCfg.Enabled {
					cleanUserText = stripWakeWord(userText, wakeCfg.Word, wakeCfg.Aliases)
				}

				callSessionsMu.Lock()
				var convHistory []OllamaMessage
				if activeCall != nil {
					activeCall.Transcript = append(activeCall.Transcript, "Ügyfél: "+userText)
					convHistory = append(convHistory, activeCall.History...)
				}
				callSessionsMu.Unlock()

				// Step 2: Query Local LLM with Tool Calling (Ollama)
				t1 := time.Now()
				var aiReply, toolCallStr string
				var llmLatency int64

				isOnlyWakeWord := false
				if wakeCfg.Enabled {
					lowerClean := strings.ToLower(strings.TrimSpace(cleanUserText))
					lowerClean = strings.Trim(lowerClean, ".,!?:; -")
					if lowerClean == "" || lowerClean == "szia" || lowerClean == "halló" || lowerClean == "jó napot" {
						isOnlyWakeWord = true
					}
				}

				if isOnlyWakeWord && wakeCfg.AckPhrase != "" {
					aiReply = wakeCfg.AckPhrase
					llmLatency = time.Since(t1).Milliseconds()
				} else {
					promptText := cleanUserText
					if strings.TrimSpace(promptText) == "" {
						promptText = userText
					}
					aiReply, toolCallStr = askLocalQwen(context.Background(), promptText, convHistory, loadedConfig.VoiceAgent.LLM, loadedConfig.VoiceAgent.SystemPrompt)
					llmLatency = time.Since(t1).Milliseconds()
				}

				callSessionsMu.Lock()
				if activeCall != nil {
					if toolCallStr != "" {
						activeCall.Transcript = append(activeCall.Transcript, "Tool: "+toolCallStr)
					}
					activeCall.Transcript = append(activeCall.Transcript, "IVR: "+aiReply)
					activeCall.History = append(activeCall.History,
						OllamaMessage{Role: "user", Content: userText},
						OllamaMessage{Role: "assistant", Content: aiReply},
					)
				}
				callSessionsMu.Unlock()

				// Step 3: Neural TTS
				t2 := time.Now()
				audioURL := ""
				replyWav := filepath.Join(os.TempDir(), fmt.Sprintf("ivr_reply_%d.wav", time.Now().UnixNano()))
				if err := voicemail.TTSWithConfig(aiReply, replyWav, loadedConfig.VoiceAgent.TTS.Command, loadedConfig.VoiceAgent.TTS.ModelPath, loadedConfig.VoiceAgent.TTS.Voice); err == nil {
					audioURL = "/api/audio?file=" + filepath.Base(replyWav)
				}
				ttsLatency := time.Since(t2).Milliseconds()
				totalLatency := time.Since(t0).Milliseconds()

				fmt.Printf("\n==================== [AI TELEPHONY LATENCY METRICS] ====================\n")
				fmt.Printf("⏱️  Audio Duration: %.2f sec\n", durSec)
				fmt.Printf("🎙️  ASR (%s): %q (%d ms)\n", filepath.Base(loadedConfig.VoiceAgent.ASR.ModelPath), userText, asrLatency)
				if toolCallStr != "" {
					fmt.Printf("⚙️  Tool Call: %s\n", toolCallStr)
				}
				fmt.Printf("🤖  LLM (%s): %q (%d ms)\n", loadedConfig.VoiceAgent.LLM.Model, aiReply, llmLatency)
				fmt.Printf("🔊  TTS (%s %s): %s (%d ms)\n", loadedConfig.VoiceAgent.TTS.Command, loadedConfig.VoiceAgent.TTS.Voice, filepath.Base(replyWav), ttsLatency)
				fmt.Printf("⚡  TOTAL TURNAROUND LATENCY: %d ms\n", totalLatency)
				fmt.Printf("=========================================================================\n\n")

				// Send back full turn result to browser WebSocket
				safeWriteJSON(map[string]any{
					"event":         "utterance",
					"user_text":     userText,
					"ai_reply":      aiReply,
					"tool_call":     toolCallStr,
					"audio_url":     audioURL,
					"duration_sec":  durSec,
					"asr_latency":   asrLatency,
					"llm_latency":   llmLatency,
					"tts_latency":   ttsLatency,
					"total_latency": totalLatency,
				})
			}

			for {
				msgType, data, err := conn.ReadMessage()
				if err != nil {
					break
				}
				if msgType == websocket.BinaryMessage && len(data) > 0 {
					rms := calcRMS(data)
					isVoice := rms > energyThresh

					bufLock.Lock()
					if !isSpeaking {
						if isVoice {
							isSpeaking = true
							speechFrames = 1
							silenceFrames = 0
							speechBuffer = append(speechBuffer[:0], data...)
							safeWriteJSON(map[string]string{"event": "vad_speech_start"})
						}
					} else {
						speechBuffer = append(speechBuffer, data...)
						if isVoice {
							silenceFrames = 0
							speechFrames++

							// Real-time live caption update every 6 frames (~760ms of speech)
							if speechFrames >= 6 && speechFrames%6 == 0 {
								if interimBusy.CompareAndSwap(false, true) {
									snap := make([]byte, len(speechBuffer))
									copy(snap, speechBuffer)
									go func(pcm []byte) {
										defer interimBusy.Store(false)
										tmpInterim := filepath.Join(os.TempDir(), fmt.Sprintf("interim_%d.wav", time.Now().UnixNano()))
										saveRawPCMToWav(pcm, sampleRate, tmpInterim)
										defer os.Remove(tmpInterim)

										whisperPrompt := ""
										if loadedConfig != nil && loadedConfig.VoiceAgent.WakeWord.Enabled {
											w := loadedConfig.VoiceAgent.WakeWord.Word
											whisperPrompt = fmt.Sprintf("%s, %s, Szia %s. Gobelisk ügyfélszolgálat.", strings.Title(w), strings.ToLower(w), strings.Title(w))
										}
										txt, err := transcribeInMemory(tmpInterim, loadedConfig.VoiceAgent.ASR.Endpoint, whisperPrompt)
										if err == nil {
											txt = strings.TrimSpace(txt)
											if idx := strings.Index(txt, "\n"); idx != -1 && strings.Contains(txt[:idx], "backend") {
												txt = strings.TrimSpace(txt[idx+1:])
											}
											if txt != "" {
												safeWriteJSON(map[string]any{
													"event": "partial_transcript",
													"text":  txt,
												})
											}
										}
									}(snap)
								}
							}

							if speechFrames >= maxSpeechFrames {
								// Max speech length reached (~5s), force endpointing
								pcmCopy := make([]byte, len(speechBuffer))
								copy(pcmCopy, speechBuffer)
								safeWriteJSON(map[string]string{"event": "vad_speech_end"})
								go flushUtterance(pcmCopy)
								isSpeaking = false
								speechBuffer = speechBuffer[:0]
								speechFrames = 0
								silenceFrames = 0
							}
						} else {
							silenceFrames++
							if silenceFrames >= silenceLimitFrames {
								// Endpointed on silence!
								if speechFrames >= minSpeechFrames {
									pcmCopy := make([]byte, len(speechBuffer))
									copy(pcmCopy, speechBuffer)
									safeWriteJSON(map[string]string{"event": "vad_speech_end"})
									go flushUtterance(pcmCopy)
								}
								isSpeaking = false
								speechBuffer = speechBuffer[:0]
								speechFrames = 0
								silenceFrames = 0
							}
						}
					}
					bufLock.Unlock()
				}
			}
		})

		// API: Serve generated TTS audio
		http.HandleFunc("/api/audio", func(w http.ResponseWriter, r *http.Request) {
			fileName := filepath.Base(r.URL.Query().Get("file"))
			filePath := filepath.Join(os.TempDir(), fileName)
			http.ServeFile(w, r, filePath)
		})

		// API: Call Hangup
		http.HandleFunc("/api/hangup", func(w http.ResponseWriter, r *http.Request) {
			callSessionsMu.Lock()
			if activeCall != nil {
				activeCall.State = "ended"
				activeCall.Transcript = append(activeCall.Transcript, "Hívás befejezve.")
			}
			callSessionsMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"message": "Hívás bontva"})
		})

		addr := fmt.Sprintf(":%d", webPort)
		fmt.Printf("\n=======================================================\n")
		fmt.Printf("🚀 Gobelisk Web Dial Pad & AI IVR Telefonközpont Elindult!\n")
		fmt.Printf("👉 Elérhető a böngészőből: http://localhost%s\n", addr)
		fmt.Printf("=======================================================\n\n")
		http.ListenAndServe(addr, nil)
	},
}

func calcRMS(pcm []byte) float64 {
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

func saveRawPCMToWav(pcm []byte, sampleRate int, outputPath string) {
	// Use ffmpeg to turn raw s16le PCM to proper WAV
	cmd := exec.Command("ffmpeg", "-y", "-f", "s16le", "-ar", fmt.Sprintf("%d", sampleRate), "-ac", "1", "-i", "-", outputPath)
	cmd.Stdin = bytes.NewReader(pcm)
	_ = cmd.Run()
}

// askAI calls Gemini 2.5 Flash for realistic telephone IVR conversation
func askAI(ctx context.Context, userPrompt string) string {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "Köszönjük hívását! Kérése rögzítésre került. Az ügyfélszolgálat hamarosan visszahívja."
	}

	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + apiKey
	bodyObj := map[string]any{
		"contents": []any{
			map[string]any{
				"parts": []any{
					map[string]any{
						"text": userPrompt,
					},
				},
			},
		},
		"systemInstruction": map[string]any{
			"parts": []any{
				map[string]any{
					"text": "Te vagy a Gobelisk PBX intelligens telefonos ügyfélszolgálati asszisztense. Beszélj udvarias, természetes, professzionális telefonos hangnemben magyarul. Nagyon fontos: válaszaid rövidek és lényegretörőek legyenek (maximum 1-2 mondat), pontosan úgy, ahogyan egy telefonközpont bot beszél a vonalban.",
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(bodyObj)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "Értettem, feldolgozom a kérését. Miben segíthetek még?"
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "Köszönöm az információt, azonnal továbbítom munkatársainknak."
	}
	defer resp.Body.Close()

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "Köszönöm, rögzítettem kérését."
	}
	if len(res.Candidates) > 0 && len(res.Candidates[0].Content.Parts) > 0 {
		return strings.TrimSpace(res.Candidates[0].Content.Parts[0].Text)
	}
	return "Köszönjük a megkeresést, miben segíthetek még?"
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
}

type OllamaToolCall struct {
	Function struct {
		Index     int            `json:"index,omitempty"`
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type OllamaChatRequest struct {
	Model    string           `json:"model"`
	Messages []OllamaMessage  `json:"messages"`
	Tools    []ToolDefinition `json:"tools,omitempty"`
	Options  map[string]any   `json:"options,omitempty"`
	Stream   bool             `json:"stream"`
}

type OllamaChatResponse struct {
	Model   string        `json:"model"`
	Message OllamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

var customerServiceTools = []ToolDefinition{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "transfer_call",
			Description: "Transfer the caller to a specific department or human agent extension",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"department": map[string]any{
						"type":        "string",
						"enum":        []string{"billing", "tech_support", "sales", "human_agent"},
						"description": "Target department: 'billing' for bills/invoicing, 'tech_support' for outages/modem faults, 'sales' for contracts/packages, 'human_agent' for general inquiries",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Short explanation of transfer rationale",
					},
				},
				"required": []string{"department"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "get_billing_invoice",
			Description: "Retrieve customer account balance, latest monthly invoice amount, or payment due date",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"customer_id": map[string]any{
						"type":        "string",
						"description": "Customer account ID (or empty if not provided)",
					},
				},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "create_incident_ticket",
			Description: "Create a technical malfunction or outage incident ticket",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"description": map[string]any{
						"type":        "string",
						"description": "Description of the reported malfunction",
					},
					"priority": map[string]any{
						"type":        "string",
						"enum":        []string{"low", "medium", "critical"},
						"description": "Priority level of the incident",
					},
				},
				"required": []string{"description"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "create_freight_order",
			Description: "Book a logistics freight transport shipment",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"origin": map[string]any{
						"type":        "string",
						"description": "Pickup city or location",
					},
					"destination": map[string]any{
						"type":        "string",
						"description": "Delivery city or destination location",
					},
					"cargo_weight_kg": map[string]any{
						"type":        "number",
						"description": "Cargo weight in kilograms",
					},
				},
				"required": []string{"origin", "destination"},
			},
		},
	},
}

const qwenSystemPrompt = `You are the Gobelisk PBX telephone customer service AI assistant.
Always answer callers in clear, polite, natural Hungarian in 1 concise sentence.
Never use Russian, Slavic characters or strange letters (no Ř).

Examples of good responses:
User: Jó napot kívánok!
Assistant: Jó napot kívánok! Miben segíthetek ma Önnek?
User: Milyen szolgáltatásaik vannak?
Assistant: Internet, telefon és televízió előfizetéseket kínálunk lakossági és üzleti ügyfeleknek.
User: Kivel beszélek?
Assistant: A Gobelisk automatizált telefonos ügyfélszolgálatával beszél, készséggel segítek bármilyen kérdésben.

Tool Execution Rules:
- When the user asks to speak with billing, technical support, or sales, call 'transfer_call'.
- When the user asks about bills, invoices, or amounts owed, call 'get_billing_invoice'.
- When the user reports an outage or equipment malfunction, call 'create_incident_ticket'.
- When the user orders freight or logistics shipment, call 'create_freight_order'.`

func askLocalQwen(ctx context.Context, userText string, history []OllamaMessage, llmCfg config.VoiceAgentLLMConfig, systemPrompt string) (string, string) {
	endpoint := llmCfg.Endpoint
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	if envHost := os.Getenv("OLLAMA_HOST"); envHost != "" {
		endpoint = envHost
	}

	chatURL := endpoint
	if strings.HasSuffix(chatURL, "/v1/chat/completions") {
		chatURL = strings.TrimSuffix(chatURL, "/v1/chat/completions") + "/api/chat"
	} else if !strings.HasSuffix(chatURL, "/api/chat") {
		chatURL = strings.TrimRight(chatURL, "/") + "/api/chat"
	}

	model := llmCfg.Model
	if model == "" {
		model = "qwen2.5:1.5b-instruct"
	}
	if envModel := os.Getenv("OLLAMA_MODEL"); envModel != "" {
		model = envModel
	}

	prompt := systemPrompt
	if strings.TrimSpace(prompt) == "" {
		prompt = qwenSystemPrompt
	}

	messages := make([]OllamaMessage, 0, len(history)+2)
	messages = append(messages, OllamaMessage{
		Role:    "system",
		Content: prompt,
	})
	if len(history) > 6 {
		messages = append(messages, history[len(history)-6:]...)
	} else {
		messages = append(messages, history...)
	}
	messages = append(messages, OllamaMessage{
		Role:    "user",
		Content: userText,
	})

	temp := llmCfg.Temperature
	if temp <= 0 {
		temp = 0.1
	}

	reqBody := OllamaChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    customerServiceTools,
		Options: map[string]any{
			"temperature":    temp,
			"repeat_penalty": 1.15,
		},
		Stream: false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return askAI(ctx, userText), ""
	}

	timeoutSec := llmCfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return askAI(ctx, userText), ""
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return askAI(ctx, userText), ""
	}
	defer resp.Body.Close()

	var chatResp OllamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return askAI(ctx, userText), ""
	}

	// Check if model called an English tool
	if len(chatResp.Message.ToolCalls) > 0 {
		toolCall := chatResp.Message.ToolCalls[0]
		toolName := toolCall.Function.Name
		toolArgs := toolCall.Function.Arguments
		_, toolSummary, spokenReply := executeCustomerServiceTool(toolName, toolArgs)
		if spokenReply != "" {
			return spokenReply, toolSummary
		}
	}

	if reply := strings.TrimSpace(chatResp.Message.Content); reply != "" {
		return reply, ""
	}

	return askAI(ctx, userText), ""
}

func executeCustomerServiceTool(name string, args map[string]any) (string, string, string) {
	switch name {
	case "transfer_call":
		dept := fmt.Sprint(args["department"])
		ext := "201"
		deptName := "Számlázási Osztály"
		switch dept {
		case "tech_support":
			ext = "202"
			deptName = "Műszaki Hibabejelentés"
		case "sales":
			ext = "203"
			deptName = "Értékesítési Osztály"
		case "human_agent":
			ext = "200"
			deptName = "Ügyfélszolgálat"
		}
		callSessionsMu.Lock()
		if activeCall != nil {
			activeCall.To = fmt.Sprintf("%s (Mellék: %s)", deptName, ext)
		}
		callSessionsMu.Unlock()
		res := fmt.Sprintf(`{"status":"success","department":"%s","target_extension":"%s","queue_wait_seconds":10}`, dept, ext)
		summary := fmt.Sprintf("transfer_call(department=%s, ext=%s)", dept, ext)
		spoken := fmt.Sprintf("Természetesen, máris átkapcsolom a %sra a %s-es mellékre. Kérem tartsa a vonalat!", deptName, ext)
		return res, summary, spoken

	case "get_billing_invoice":
		custID := fmt.Sprint(args["customer_id"])
		if custID == "" || custID == "<nil>" {
			custID = "100234"
		}
		res := fmt.Sprintf(`{"status":"success","customer_id":"%s","invoice_period":"Előző havi internet","amount_huf":8990,"payment_due":"2026-11-15","is_paid":false}`, custID)
		summary := fmt.Sprintf("get_billing_invoice(customer_id=%s, amount=8990 HUF)", custID)
		spoken := "A múlt havi internet számla összege 8990 Ft, a fizetési határidő 2026. november 15."
		return res, summary, spoken

	case "create_incident_ticket":
		desc := fmt.Sprint(args["description"])
		prio := fmt.Sprint(args["priority"])
		ticketID := fmt.Sprintf("INC-%d", time.Now().Unix()%100000)
		res := fmt.Sprintf(`{"status":"created","ticket_id":"%s","description":"%s","priority":"%s"}`, ticketID, desc, prio)
		summary := fmt.Sprintf("create_incident_ticket(id=%s, priority=%s)", ticketID, prio)
		spoken := fmt.Sprintf("Rögzítettem a hibabejelentést %s azonosítóval. Munkatársaink hamarosan kivizsgálják a hibát.", ticketID)
		return res, summary, spoken

	case "create_freight_order":
		origin := fmt.Sprint(args["origin"])
		dest := fmt.Sprint(args["destination"])
		orderID := fmt.Sprintf("FR-%d", time.Now().Unix()%100000)
		res := fmt.Sprintf(`{"status":"booked","order_id":"%s","route":"%s -> %s"}`, orderID, origin, dest)
		summary := fmt.Sprintf("create_freight_order(id=%s, %s -> %s)", orderID, origin, dest)
		spoken := fmt.Sprintf("A fuvarmegbízást felvettük %s azonosítóval %s és %s között.", orderID, origin, dest)
		return res, summary, spoken

	default:
		return `{"status":"unknown_tool"}`, fmt.Sprintf("unknown_tool(%s)", name), "A kérését rögzítettük."
	}
}

const dialPadHTML = `<!DOCTYPE html>
<html lang="hu">
<head>
  <meta charset="UTF-8">
  <title>Gobelisk PBX - AI IVR & DTMF Tesztelő</title>
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg: #0f172a;
      --card-bg: #1e293b;
      --accent: #38bdf8;
      --accent-hover: #0ea5e9;
      --text: #f8fafc;
      --text-muted: #94a3b8;
      --border: #334155;
      --success: #22c55e;
      --danger: #ef4444;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: 'Inter', sans-serif;
      background: var(--bg);
      color: var(--text);
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 100vh;
      padding: 20px;
    }
    .container {
      display: grid;
      grid-template-columns: 360px 520px;
      gap: 24px;
      max-width: 940px;
      width: 100%;
    }
    .card {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 24px;
      box-shadow: 0 10px 25px -5px rgba(0,0,0,0.5);
    }
    h1, h2 { font-size: 1.25rem; font-weight: 600; margin-bottom: 16px; display: flex; align-items: center; gap: 8px; }
    .badge {
      font-size: 0.75rem;
      padding: 3px 8px;
      border-radius: 9999px;
      background: rgba(56, 189, 248, 0.2);
      color: var(--accent);
      font-weight: 500;
    }
    .input-group {
      display: flex;
      gap: 8px;
      margin-bottom: 16px;
    }
    input {
      background: #0f172a;
      border: 1px solid var(--border);
      color: var(--text);
      padding: 10px 14px;
      border-radius: 8px;
      font-size: 0.95rem;
      outline: none;
      width: 100%;
    }
    input:focus { border-color: var(--accent); }
    .display {
      background: #090d16;
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 12px 16px;
      font-family: monospace;
      font-size: 1.4rem;
      min-height: 52px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 16px;
      color: #38bdf8;
    }
    .grid-keypad {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 12px;
      margin-bottom: 16px;
    }
    .key-btn {
      background: #334155;
      border: 1px solid rgba(255,255,255,0.08);
      color: var(--text);
      font-size: 1.4rem;
      font-weight: 600;
      padding: 16px;
      border-radius: 12px;
      cursor: pointer;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      transition: all 0.15s ease;
      user-select: none;
    }
    .key-btn span {
      font-size: 0.65rem;
      font-weight: 400;
      color: var(--text-muted);
      margin-top: 2px;
    }
    .key-btn:active {
      transform: scale(0.95);
      background: var(--accent);
      color: #000;
    }
    .action-btns {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 12px;
    }
    .btn {
      padding: 14px;
      border-radius: 10px;
      font-weight: 600;
      font-size: 0.95rem;
      border: none;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      transition: all 0.2s;
    }
    .btn-dial { background: var(--success); color: #fff; }
    .btn-dial:hover { background: #16a34a; }
    .btn-hangup { background: var(--danger); color: #fff; }
    .btn-hangup:hover { background: #dc2626; }
    .chat-box {
      display: flex;
      flex-direction: column;
      height: 500px;
    }
    .transcript-log {
      flex: 1;
      background: #090d16;
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 16px;
      overflow-y: auto;
      display: flex;
      flex-direction: column;
      gap: 12px;
      font-size: 0.9rem;
    }
    .msg {
      padding: 10px 14px;
      border-radius: 10px;
      line-height: 1.4;
      max-width: 88%;
      word-wrap: break-word;
    }
    .msg-system {
      align-self: center;
      background: rgba(255,255,255,0.06);
      color: var(--text-muted);
      font-size: 0.8rem;
    }
    .msg-user {
      align-self: flex-end;
      background: #2563eb;
      color: #fff;
    }
    .msg-live {
      align-self: flex-end;
      background: rgba(37, 99, 235, 0.35);
      color: #93c5fd;
      border: 1px dashed var(--accent);
      font-style: italic;
    }
    .msg-ai {
      align-self: flex-start;
      background: #334155;
      color: #38bdf8;
      border-left: 3px solid var(--accent);
    }
    .status-bar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-top: 12px;
      font-size: 0.85rem;
      color: var(--text-muted);
      background: #0f172a;
      padding: 8px 12px;
      border-radius: 8px;
    }
    .status-item {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .status-dot {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: #64748b;
    }
    .status-dot.active { background: var(--success); }
    .status-dot.speaking {
      background: #ef4444;
      box-shadow: 0 0 10px #ef4444;
      animation: pulse 1s infinite;
    }
    @keyframes pulse {
      0% { opacity: 1; }
      50% { opacity: 0.5; }
      100% { opacity: 1; }
    }
    audio { display: none; }
  </style>
</head>
<body>

<div class="container">
  <!-- Dialpad & Controls -->
  <div class="card">
    <h1>📞 Dial Pad <span class="badge">SIP / DTMF</span></h1>
    
    <div class="input-group">
      <input id="ext-from" placeholder="From (pl: 100)" value="100" />
      <input id="ext-to" placeholder="To (pl: 200 IVR)" value="200" />
    </div>

    <div class="display">
      <span id="dtmf-display">---</span>
      <span style="font-size: 0.8rem; color: #64748b;" id="call-status">Készenlét</span>
    </div>

    <div class="grid-keypad">
      <button class="key-btn" onclick="sendDTMF('1')">1<span>.</span></button>
      <button class="key-btn" onclick="sendDTMF('2')">2<span>ABC</span></button>
      <button class="key-btn" onclick="sendDTMF('3')">3<span>DEF</span></button>
      <button class="key-btn" onclick="sendDTMF('4')">4<span>GHI</span></button>
      <button class="key-btn" onclick="sendDTMF('5')">5<span>JKL</span></button>
      <button class="key-btn" onclick="sendDTMF('6')">6<span>MNO</span></button>
      <button class="key-btn" onclick="sendDTMF('7')">7<span>PQRS</span></button>
      <button class="key-btn" onclick="sendDTMF('8')">8<span>TUV</span></button>
      <button class="key-btn" onclick="sendDTMF('9')">9<span>WXYZ</span></button>
      <button class="key-btn" onclick="sendDTMF('*')">*<span>MENU</span></button>
      <button class="key-btn" onclick="sendDTMF('0')">0<span>+</span></button>
      <button class="key-btn" onclick="sendDTMF('#')">#<span>OK</span></button>
    </div>

    <div class="action-btns">
      <button class="btn btn-dial" id="btn-call" onclick="startCall()">Hívás Indítása</button>
      <button class="btn btn-hangup" id="btn-hangup" onclick="hangup()">Bontás</button>
    </div>
  </div>

  <!-- Live Transcript & AI Voicebot -->
  <div class="card chat-box">
    <h2>🤖 AI IVR Élő Beszélgetés <span class="badge">GPU Whisper STT + Qwen 1.5B LLM + Piper TTS</span></h2>
    
    <div class="transcript-log" id="transcript-log">
      <div class="msg msg-system">Kattints a "Hívás Indítása" gombra! A hívás alatt a vonal folyamatosan nyitva marad, a VAD automatikusan érzékeli amikor beszélsz.</div>
    </div>

    <div class="status-bar">
      <div class="status-item">
        <div class="status-dot" id="status-indicator"></div>
        <span id="audio-status">Vonal bontva</span>
      </div>
      <div class="status-item">
        <div class="status-dot" id="vad-indicator"></div>
        <span id="vad-status">VAD: Csend</span>
      </div>
    </div>
  </div>
</div>

<audio id="ivr-audio" autoplay></audio>

<script>
  let isCalling = false;
  let ws = null;
  let audioContext = null;
  let mediaStream = null;
  let scriptProcessor = null;
  let liveMsgEl = null;
  const audioEl = document.getElementById('ivr-audio');

  function showLiveCaption(text) {
    const log = document.getElementById('transcript-log');
    if (!liveMsgEl) {
      liveMsgEl = document.createElement('div');
      liveMsgEl.className = 'msg msg-live';
      liveMsgEl.id = 'live-caption-bubble';
      log.appendChild(liveMsgEl);
    }
    liveMsgEl.textContent = '🎙️ ' + text + ' ▌';
    log.scrollTop = log.scrollHeight;
  }

  function hideLiveCaption() {
    if (liveMsgEl && liveMsgEl.parentNode) {
      liveMsgEl.parentNode.removeChild(liveMsgEl);
      liveMsgEl = null;
    }
  }

  function logMsg(role, text) {
    const log = document.getElementById('transcript-log');
    const msg = document.createElement('div');
    msg.className = 'msg msg-' + role;
    msg.textContent = text;
    log.appendChild(msg);
    log.scrollTop = log.scrollHeight;
  }

  function playAudio(url) {
    if (!url) return;
    audioEl.src = url;
    audioEl.play().catch(e => console.log('Autoplay:', e));
  }

  async function startCall() {
    try {
      const ctx = new (window.AudioContext || window.webkitAudioContext)();
      if (ctx.state === 'suspended') ctx.resume();
      audioEl.play().catch(() => {});
    } catch(e) {}

    const from = document.getElementById('ext-from').value;
    const to = document.getElementById('ext-to').value;
    document.getElementById('call-status').textContent = 'Csatlakozás...';

    try {
      const resp = await fetch('/api/dial', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({from, to})
      });
      const data = await resp.json();

      isCalling = true;
      document.getElementById('call-status').textContent = 'Hívásban';
      document.getElementById('status-indicator').classList.add('active');
      document.getElementById('audio-status').textContent = 'Vonal él: Folyamatos mikrofon stream';
      logMsg('system', 'Hívás felépült: ' + to);
      if (data.welcome_text) {
        logMsg('ai', data.welcome_text);
      }
      if (data.audio_url) {
        playAudio(data.audio_url);
      }

      // Start continuous audio stream via WebSocket to backend VAD
      startAudioStream();
    } catch (err) {
      document.getElementById('call-status').textContent = 'Hiba';
      logMsg('system', 'Csatlakozási hiba: ' + err);
    }
  }

  function hangup() {
    stopAudioStream();
    hideLiveCaption();
    fetch('/api/hangup', {method: 'POST'})
      .then(r => r.json())
      .then(() => {
        isCalling = false;
        document.getElementById('call-status').textContent = 'Bontva';
        document.getElementById('dtmf-display').textContent = '---';
        document.getElementById('status-indicator').classList.remove('active');
        document.getElementById('vad-indicator').className = 'status-dot';
        document.getElementById('audio-status').textContent = 'Vonal bontva';
        document.getElementById('vad-status').textContent = 'VAD: Inaktív';
        logMsg('system', 'Hívás befejezve.');
      });
  }

  function startAudioStream() {
    const wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(wsProto + '//' + window.location.host + '/ws/audio');
    ws.binaryType = 'arraybuffer';

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.event === 'vad_speech_start') {
          document.getElementById('vad-indicator').className = 'status-dot speaking';
          document.getElementById('vad-status').textContent = 'VAD: Beszéd... (Élő feliratozás)';
          showLiveCaption('...');
        } else if (data.event === 'partial_transcript') {
          showLiveCaption(data.text);
        } else if (data.event === 'vad_speech_end') {
          document.getElementById('vad-indicator').className = 'status-dot';
          document.getElementById('vad-status').textContent = 'VAD: Feldolgozás...';
        } else if (data.event === 'wake_word_standby') {
          hideLiveCaption();
          document.getElementById('vad-status').textContent = 'VAD: Készenlét (Mondd: "' + data.wake_word + '")';
          logMsg('system', '💤 Készenlét: beszéd figyelmen kívül hagyva (hívószó: "' + data.wake_word + '" szükséges)');
        } else if (data.event === 'wake_word_activated') {
          document.getElementById('vad-status').textContent = '🔔 Aktív: Hívószó ("' + data.wake_word + '") felismerve!';
          logMsg('system', '🔔 Hívószó ("' + data.wake_word + '") detektálva! Asszisztens bekapcsolva.');
        } else if (data.event === 'utterance') {
          hideLiveCaption();
          const stats = 'ASR: ' + data.asr_latency + 'ms | LLM: ' + data.llm_latency + 'ms | TTS: ' + data.tts_latency + 'ms | Turn: ' + data.total_latency + 'ms';
          document.getElementById('vad-status').textContent = 'VAD: Készenlét';
          if (data.user_text) {
            logMsg('user', '🎙️ ' + data.user_text);
          }
          if (data.tool_call) {
            logMsg('system', '⚙️ MCP Tool: ' + data.tool_call);
          }
          if (data.ai_reply) {
            logMsg('ai', data.ai_reply);
          }
          if (data.audio_url) {
            playAudio(data.audio_url);
          }
          logMsg('system', '⚡ Latency: ' + stats);
        }
      } catch(e) {}
    };

    navigator.mediaDevices.getUserMedia({ 
      audio: { 
        channelCount: 1, 
        sampleRate: 16000,
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true
      } 
    })
      .then(stream => {
        mediaStream = stream;
        audioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 16000 });
        const source = audioContext.createMediaStreamSource(stream);

        // 2048 buffer size at 16kHz ~= 128ms chunks
        scriptProcessor = audioContext.createScriptProcessor(2048, 1, 1);
        source.connect(scriptProcessor);

        // Mute mic feedback through speakers
        const muteGain = audioContext.createGain();
        muteGain.gain.value = 0;
        scriptProcessor.connect(muteGain);
        muteGain.connect(audioContext.destination);

        scriptProcessor.onaudioprocess = (e) => {
          if (!isCalling || !ws || ws.readyState !== WebSocket.OPEN) return;
          // Prevent acoustic loop: do not send mic frames while agent audio is actively playing!
          if (!audioEl.paused && !audioEl.ended) return;

          const inputData = e.inputBuffer.getChannelData(0);

          // Convert float32 to int16 linear PCM
          const pcm16 = new Int16Array(inputData.length);
          for (let i = 0; i < inputData.length; i++) {
            let s = Math.max(-1, Math.min(1, inputData[i]));
            pcm16[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
          }
          ws.send(pcm16.buffer);
        };
      })
      .catch(err => {
        logMsg('system', 'Mikrofon hiba: ' + err);
      });
  }

  function stopAudioStream() {
    if (scriptProcessor) { scriptProcessor.disconnect(); scriptProcessor = null; }
    if (audioContext) { audioContext.close(); audioContext = null; }
    if (mediaStream) { mediaStream.getTracks().forEach(t => t.stop()); mediaStream = null; }
    if (ws) { ws.close(); ws = null; }
  }

  function sendDTMF(digit) {
    document.getElementById('dtmf-display').textContent = digit;
    playTone(digit);
    logMsg('user', 'DTMF: ' + digit);

    fetch('/api/dtmf', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({digit})
    })
    .then(r => r.json())
    .then(data => {
      if (data.reply) {
        logMsg('ai', data.reply);
      }
      if (data.audio_url) {
        playAudio(data.audio_url);
      }
    });
  }

  // DTMF Web Audio API synthesizer for realistic telephone tone
  function playTone(digit) {
    const dtmfFreqs = {
      '1': [697, 1209], '2': [697, 1336], '3': [697, 1477],
      '4': [770, 1209], '5': [770, 1336], '6': [770, 1477],
      '7': [852, 1209], '8': [852, 1336], '9': [852, 1477],
      '*': [941, 1209], '0': [941, 1336], '#': [941, 1477]
    };
    if (!dtmfFreqs[digit]) return;
    try {
      const ctx = new (window.AudioContext || window.webkitAudioContext)();
      const [f1, f2] = dtmfFreqs[digit];
      const o1 = ctx.createOscillator();
      const o2 = ctx.createOscillator();
      const gain = ctx.createGain();
      gain.gain.value = 0.15;
      o1.frequency.value = f1;
      o2.frequency.value = f2;
      o1.connect(gain);
      o2.connect(gain);
      gain.connect(ctx.destination);
      o1.start();
      o2.start();
      setTimeout(() => { o1.stop(); o2.stop(); ctx.close(); }, 120);
    } catch(e) {}
  }
</script>
</body>
</html>`

func init() {
	webCmd.Flags().IntVar(&webPort, "port", 8088, "web port")
	rootCmd.AddCommand(webCmd)
}
