# Pir² Voice Agent & AI Customer Service (WebRTC + Vulkan GPU)

The Gobelisk voice agent provides a real-time, low-latency, 100% local Hungarian voice AI customer service and meeting assistant. It can operate in two primary deployment modes:
1. **Pir² Meeting Assistant**: Joins a live **Pir² meeting over WebRTC**, listens to meeting audio, transcribes participants, and responds into the room.
2. **Web / SIP Telephony Agent**: Operates as a live web or SIP PBX interactive voice agent with **real-time live captioning**, **wake-word activation ("Maci")**, and **multi-turn dialogue**.

Everything runs strictly on the local GPU stack (Whisper ASR on Vulkan compute, local Ollama LLM, and Piper Neural Hungarian TTS); **no external cloud APIs or third-party network egress are required**.

---

## Architecture Overview

```
                                [ Caller / Meeting Participant ]
                                  │                        ▲
               Opus / WebRTC / PCM│                        │ Audio Playback (PCMU / WAV)
                                  ▼                        │ (Microphone auto-muted during playback)
                     ┌───────────────────────────┐         │
                     │  RMS VAD & Audio Framing  │         │
                     └─────────────┬─────────────┘         │
                                   │                       │
                     ┌─────────────┴─────────────┐         │
                     │                           │         │
                     ▼                           ▼         │
        [ Continuous Ring Buffer ]      [ Utterance Buffer ]│
                     │                           │         │
         (Every ~600ms chunk)             (Endpointed at)  │
                     │                     (~640ms silence)│
                     ▼                           │         │
         [ Real-Time Live Caption ]              ▼         │
           (UI streaming text)         ┌───────────────────┴───┐
                                       │ Whisper Large-v3-Turbo│
                                       │ (Vulkan GPU Compute)  │
                                       └───────────┬───────────┘
                                                   │ Hungarian Transcript
                                                   ▼
                                       ┌───────────────────────┐
                                       │  Wake Word Detector   │
                                       │  ("Maci" + Aliases)   │
                                       └───────────┬───────────┘
                                                   │
                        ┌──────────────────────────┴──────────────────────────┐
                        │ [STANDBY: Discard speech]                           │ [ACTIVE / Wake Triggered]
                        ▼                                                     ▼
               (Wait for wake word)                          ┌────────────────────────────────┐
                                                             │ Ollama LLM (Qwen2.5-Instruct)  │
                                                             │ ├── Multi-Turn Session History │
                                                             │ ├── English MCP Tool Calling   │
                                                             │ └── Hungarian Spoken Output    │
                                                             └───────────────┬────────────────┘
                                                                             │
                                                                             ▼
                                                             ┌────────────────────────────────┐
                                                             │ Piper Neural TTS (Anna Voice)  │
                                                             └───────────────┬────────────────┘
                                                                             │ Synthesized Audio
                                                                             └─────────────────────┘
```

---

## Core Components & Capabilities

### 1. Whisper ASR on Vulkan GPU (`whisper.cpp`)
- **Engine**: Local `whisper-server` powered by `whisper.cpp` with Vulkan compute shader acceleration (`--device 1` targeting the dedicated NVIDIA Quadro T2000 GPU).
- **Model**: `ggml-large-v3-turbo-q5_0.bin` (5-bit quantization, ~1.1 GB VRAM footprint).
- **Benchmark Performance**: Transcribes a 3–4 second Hungarian audio segment in **~1.1 seconds** (real-time factor < 0.3x).
- **Prompt Biasing**: Initial prompt passed during server startup and inference (`--prompt "Maci, maci, Szia Maci. Gobelisk ügyfélszolgálat."`) primes the acoustic decoder toward Hungarian domain vocabulary and wake words.

### 2. Wake-Word Gating & Phonetic Aliasing
To prevent the agent from interrupting background room noise, conversations, or TV audio, the agent incorporates a **Wake-Word Gating** state machine:
- **Standby Mode**: Inbound audio is transcribed via Whisper. If the wake word is not present, the transcript is dropped with zero LLM or TTS resource consumption.
- **Activation Keyword**: Defaults to `"maci"` (case-insensitive).
- **Phonetic Aliasing**: Short 2-syllable isolated words can trigger Whisper's internal Hungarian language model priors. When a user says *"Maci"* alone, Whisper may output phonetic variants such as *"Marci"*, *"Márci"*, *"Macika"*, *"Márti"*, or *"Mátszik"*. The [`VoiceAgentWakeWordConfig`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/pkg/config/voiceagent.go#L46-L52) includes an `aliases` list matching these phonetic variants.
- **Instant Acknowledgement**: When the user utters only the wake word (e.g., *"Maci"*), the agent bypasses LLM inference entirely and instantly synthesizes an immediate Hungarian acknowledgement: `"Igen, hallgatom!"` in **~2.5s total turnaround time** (ASR ~1.1s + TTS ~1.4s), transitioning the session to `ACTIVE` state.
- **Inactivity Timeout**: The session remains active for conversational exchanges for 25 seconds (configurable via `timeout_seconds`). If no speech occurs within this window, the session returns to `STANDBY`.

### 3. Multi-Turn Conversational Memory & Anti-Looping
- **Session History**: [`WebCallSession`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/cmd/web.go) maintains an in-memory `History []OllamaMessage` across turns.
- **Greedy Repetition Elimination**: By passing conversational history and elevating LLM temperature to `0.5`, small quantized models (e.g. Qwen2.5 1.5B/3B) avoid collapsing into repetitive attractor loops (such as repeating greeting messages on every turn).
- **Acoustic Echo Suppression**: WebRTC browser microphones are paused (`if (!audioEl.paused && !audioEl.ended) return;`) while agent speech is actively rendering through speakers, preventing acoustic loopback and self-triggering.

### 4. Real-Time Sliding-Window Live Captions
- In addition to utterance-based endpointing, the web interface streams audio in continuous sliding windows (~600ms chunks).
- Live captions are delivered through WebSocket frames (`transcript_partial` and `transcript_final`), providing instant visual feedback on screen while speech is occurring.

---

## Configuration (`gobelisk.yaml`)

All parameters are configured in [`gobelisk.yaml`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/gobelisk.yaml) and automatically mapped to [`VoiceAgentConfig`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/pkg/config/voiceagent.go#L8-L43):

```yaml
voice_agent:
  enabled: true
  uid: "voice-agent"
  display_name: "Gobelisk AI Asszisztens"
  role: "agent"
  language: "hu"
  max_history: 20

  greeting: "Üdvözlöm a Gobelisk telefonos ügyfélszolgálatán! Kérjük, válasszon menüpontot: 1-es gomb számlázás, 2-es gomb műszaki hibabejelentés, 3-as gomb értékesítés. Vagy csak egyszerűen beszéljen, figyeljük a vonalat!"

  system_prompt: |
    Te vagy Maci, a Gobelisk PBX intelligens telefonos ügyfélszolgálati AI asszisztense.
    Mindig válaszolj közvetlenül és érdemben arra, amit a hívó kérdez, természetes, tiszta magyar nyelven 1-2 tömör mondatban.
    Ha a hívó viccet kér, mesélj el egy rövid, vicces magyar poént vagy faviccet.
    Ha a beszélgetés már zajlik, SOHA ne köszönj újra feleslegesen, hanem közvetlenül a kérdésre vagy kérésre válaszolj!

    Tool Execution Rules:
    - When the user asks to speak with billing, technical support, or sales, call 'transfer_call'.
    - When the user asks about bills, invoices, or amounts owed, call 'get_billing_invoice'.
    - When the user reports an outage or equipment malfunction, call 'create_incident_ticket'.
    - When the user orders freight or logistics shipment, call 'create_freight_order'.

  vad:
    energy_threshold: 500.0  # RMS audio energy threshold
    silence_ms: 640          # Trailing silence duration to finalize utterance (5 * 128ms)
    min_speech_ms: 380       # Minimum speech duration to trigger processing (3 * 128ms)
    max_speech_ms: 5120      # Maximum utterance duration before forcing endpoint (40 * 128ms)
    pre_roll_ms: 250

  asr:
    endpoint: "http://127.0.0.1:8099/inference"
    model_path: "/opt/whisper/models/ggml-large-v3-turbo-q5_0.bin"
    device: "1"              # GPU device ID (NVIDIA Quadro T2000)
    language: "hu"
    timeout_seconds: 30

  llm:
    endpoint: "http://127.0.0.1:11434"
    model: "qwen2.5:1.5b-instruct"
    temperature: 0.5
    max_tokens: 256
    timeout_seconds: 15
    keep_alive: "-1"         # Keep model resident in VRAM forever

  tts:
    command: "piper"
    model_path: "/opt/piper/models/hu_HU-anna-medium.onnx"
    voice: "anna"
    format: "wav"
    sample_rate: 22050
    timeout_seconds: 30

  wake_word:
    enabled: true
    word: "maci"             # Case-insensitive activation keyword
    aliases:                 # Phonetic variants Whisper may output for short "Maci"
      - "marci"
      - "márci"
      - "macika"
      - "márti"
      - "márcik"
      - "mátszik"
    timeout_seconds: 25      # Returns to standby after 25s of inactivity
    ack_phrase: "Igen, hallgatom!" # Instant acknowledgement when only wake word is spoken
```

---

## How it connects to Pir² Meetings

1. `POST {media_service_url}/v1/internal/sessions/{meeting_session_id}/grant` with `Authorization: Bearer {media_internal_token}`. The media control service mints an Ed25519 join grant and returns the SFU signaling URL.
2. The agent opens a WebSocket to the SFU signaling endpoint and sends the `join` JSON-RPC request (`sid`, `uid`, `token`, SDP offer).
3. The SFU auto-subscribes the agent to the meeting audio and renegotiates down-tracks with `offer` notifications.
4. The agent publishes one PCMU (G.711 μ-law) audio track natively forwardable without transcoding.
5. When the meeting ends, the transcript is exported to the Pir² recorder (`/v1/recordings`, `/v1/transcriptions`).

---

## Running the Web Portal & AI Agent

Start the WebRTC & PBX management portal:
```bash
./bin/gobelisk web
```
Open `http://localhost:8088` in your browser. The portal provides:
- Live SIP status, active calls, registered extensions.
- Interactive WebRTC / WebSocket microphone audio stream with real-time waveform visualizer.
- Live streaming Hungarian speech captions.
- Wake word status badge (`STANDBY` 🟡 $\leftrightarrow$ `ACTIVE` 🟢).
- Natural synthesized Hungarian voice responses via Piper.

To run the standalone meeting agent joining an existing Pir² meeting:
```bash
./bin/gobelisk agent run --config ./gobelisk.yaml --session <meeting-session-id>
```

---

## English MCP Tool Calling (Function Calling)

All internal function names, parameter definitions, and tool schemas follow English engineering standards, while caller interaction remains 100% natural Hungarian.

```go
pipeline.AddTool(voiceagent.Tool{
    Name:        "get_billing_invoice",
    Description: "Retrieve recent billing invoice status and pending balances for customer account.",
    Parameters: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "account_id": map[string]any{"type": "string"},
        },
        "required": []string{"account_id"},
    },
    Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
        return `{"status":"paid","amount":0,"currency":"HUF"}`, nil
    },
})
```

See [MCP_SYSTEM_PROMPT.md](file:///home/paalgyula/wspace/projects/pilab/gobelisk/docs/ai-customer-service/MCP_SYSTEM_PROMPT.md) and [docs/ai-customer-service/README.md](file:///home/paalgyula/wspace/projects/pilab/gobelisk/docs/ai-customer-service/README.md) for full tool calling examples.
