# Local Hungarian AI Voice Customer Service Architecture (PBX + Whisper + Local LLM + Piper TTS + MCP)

> Production Reference: See [voice-agent.md](../voice-agent.md) for WebRTC meeting integration and [pkg/voiceagent](file:///home/paalgyula/wspace/projects/pilab/gobelisk/pkg/voiceagent) for Go source packages.

This document describes the complete local Hungarian AI customer service architecture for the [`gobelisk`](file:///home/paalgyula/wspace/projects/pilab/gobelisk) PBX. It covers real-time speech processing, Vulkan GPU acceleration, wake-word gating, empirical latency benchmarks, English MCP tool calling, and multi-turn dialogue management without external cloud dependencies.

---

## 1. System Architecture Overview

The system processes real-time inbound audio from SIP/RTP telephony or in-browser WebRTC/WebSocket streams. All models run 100% on-premise on dedicated GPU hardware (e.g., NVIDIA Quadro T2000, 4 GB VRAM).

```
                                [ Customer Call (SIP / WebRTC) ]
                                                │
                                                ▼ (PCM 16 kHz Mono)
                                  ┌───────────────────────────┐
                                  │   RMS VAD & Audio Buffer  │
                                  └─────────────┬─────────────┘
                                                │
                          ┌─────────────────────┴─────────────────────┐
                          │                                           │
                          ▼                                           ▼
             [ Continuous Audio Chunks ]                    [ Utterance Buffer ]
             (Every ~600ms sliding frame)                   (Endpointed at 640ms silence)
                          │                                           │
                          ▼                                           ▼
             [ Real-Time Live Captions ]                    ┌─────────────────────────┐
             (Streaming text to caller UI)                  │ Whisper Large-v3-Turbo  │
                                                            │ (Vulkan GPU Device 1)   │
                                                            └───────────┬─────────────┘
                                                                        │ Hungarian Text
                                                                        ▼
                                                            ┌─────────────────────────┐
                                                            │    Wake Word Gating     │
                                                            │   ("Maci" + Aliases)    │
                                                            └───────────┬─────────────┘
                                                                        │
                                       ┌────────────────────────────────┴────────────────────────────────┐
                                       │ [STANDBY: Wake Word absent]                                     │ [ACTIVE / Wake Word detected]
                                       ▼                                                                 ▼
                             (Discard speech frame,                                        ┌─────────────────────────────┐
                              zero LLM/TTS compute)                                        │  Ollama LLM (Qwen2.5-3B)    │
                                                                                           │  ├── Multi-turn History     │
                                                                                           │  ├── English Tool / MCP     │
                                                                                           │  └── Hungarian Persona      │
                                                                                           └──────────────┬──────────────┘
                                                                                                          │
                                                                           ┌──────────────────────────────┴──────────────────────────────┐
                                                                           ▼                                                             ▼
                                                                 [ English MCP Tool Call ]                                     [ Natural Speech Text ]
                                                                 (e.g., get_billing_invoice,                                             │
                                                                  transfer_call, create_order)                                           ▼
                                                                                                                               ┌─────────────────────────┐
                                                                                                                               │ Piper Neural TTS (Anna) │
                                                                                                                               └────────────┬────────────┘
                                                                                                                                            │ Synthesized Audio (WAV)
                                                                                                                                            ▼
                                                                                                                               [ Audio Streamed to Caller ]
                                                                                                                               (Mic muted during playback)
```

---

## 2. Empirical Benchmark & Latency Measurements

Tested directly on a local workstation equipped with an **NVIDIA Quadro T2000 GPU (4 GB VRAM)** running Ubuntu Linux:

| Pipeline Stage | Engine / Model | Hardware Acceleration | Latency / Turnaround | Notes |
|---|---|---|---|---|
| **Audio VAD & Endpointing** | Pure-Go RMS Energy Gate | Host CPU | ~640 ms trailing silence | Configurable in [`gobelisk.yaml`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/gobelisk.yaml) |
| **Speech-to-Text (ASR)** | `whisper-server` (`large-v3-turbo-q5_0`) | Vulkan Compute Shaders (GPU 1) | **~1.1s** | Transcribes a 3–4s utterance with real-time factor < 0.3x |
| **Wake Acknowledgement** | Instant bypass (`"Igen, hallgatom!"`) | GPU Piper TTS | **~2.5s total turnaround** | 0 ms LLM overhead; instant feedback to caller |
| **LLM Inference** | Ollama (`qwen2.5:1.5b-instruct` / `3b`) | GPU Offloading (VRAM resident) | **~5.5s – 6.8s** | Multi-turn contextual reasoning & prompt formatting |
| **Text-to-Speech (TTS)** | Piper Neural TTS (`hu_HU-anna-medium`) | ONNX Runtime | **~1.4s – 1.9s** | Natural, human-like Hungarian voice synthesis |
| **Full Conversational Turn** | End-to-end (ASR + LLM + TTS) | Local Vulkan / Ollama GPU | **~9.5s total** | Utterance completion to caller speaker output |

---

## 3. Wake Word / Keyword Spotting (KWS) Gate ("Maci")

To ensure privacy, prevent false triggers from background ambient speech, and optimize GPU compute, the voice agent implements a dedicated wake-word gate.

### Hungarian Acoustic Prior & Phonetic Aliasing
When transcribing short, isolated 2-syllable Hungarian words (such as *"Maci"*), general-purpose Whisper language models exhibit a statistical bias toward common first names or morphological variants:
- User speaks: `"Maci"`
- Raw Whisper output without bias: `"Márci."`, `"Marci."`, or `"Márti."`

To guarantee 100% reliable detection without requiring an extra trained keyword-spotting neural network:
1. **Prompt Biasing**: The Whisper server and inference requests are injected with a domain prompt:
   ```bash
   --prompt "Maci, maci, Szia Maci. Gobelisk ügyfélszolgálat."
   ```
2. **Phonetic Aliasing Table**: Configured declaratively in [`gobelisk.yaml`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/gobelisk.yaml):
   ```yaml
   wake_word:
     enabled: true
     word: "maci"
     aliases:
       - "marci"
       - "márci"
       - "macika"
       - "márti"
       - "márcik"
       - "mátszik"
     timeout_seconds: 25
     ack_phrase: "Igen, hallgatom!"
   ```
3. **Instant Acknowledgement**: Uttering only the wake word triggers immediate audio feedback (`"Igen, hallgatom!"`), setting the session state to `ACTIVE` for 25 seconds of continuous dialogue.

---

## 4. Multi-Turn Dialogue & Acoustic Echo Suppression

Small language models (1.5B–3B parameters) are prone to repetitive attractor loops when history is omitted or temperature is too low (e.g. continually repeating the first greeting). The architecture resolves this through three mechanisms:

1. **Session History Retention**: [`WebCallSession`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/cmd/web.go) accumulates messages in `History []OllamaMessage`, enabling natural follow-up dialogue (e.g. asking for jokes, answering clarifying questions, executing tasks).
2. **Balanced Sampling Temperature**: Set to `0.5` in [`gobelisk.yaml`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/gobelisk.yaml) to maintain factual coherence while allowing stylistic variety.
3. **Acoustic Feedback Elimination**:
   - WebRTC / Web Audio mic sampling is conditionally paused during agent speech playback:
     ```javascript
     if (!audioEl.paused && !audioEl.ended) return; // Mute input while agent speaks
     ```
   - Audio graph routing isolates microphone input through a zero-gain Web Audio node, preventing browser playback of the user's own microphone into local laptop speakers.
   - Hardware acoustic echo cancellation (`echoCancellation: true`) is enabled on browser audio tracks.

---

## 5. Real-Time Sliding-Window Live Captions

For enhanced caller accessibility and user feedback, the web client features dual-stream speech handling:
- **Streaming Live Caption Path**: Audio frames are batched into ~600ms chunks and posted to the real-time captioning pipeline, sending WebSocket events (`transcript_partial`, `transcript_final`) to render dynamic speech captions on the UI in near real-time.
- **Utterance Action Path**: The RMS VAD continuously monitors signal energy. Once trailing silence exceeds 640ms, the full utterance is packaged and dispatched for wake-word gating and LLM conversational handling.

---

## 6. Decoupled Model Configuration (`gobelisk.yaml`)

Model names, endpoints, paths, and timeouts are fully decoupled from Go source code. The PBX automatically loads [`gobelisk.yaml`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/gobelisk.yaml) on startup (or falls back to safe embedded defaults):

```yaml
voice_agent:
  enabled: true
  language: "hu"
  asr:
    endpoint: "http://127.0.0.1:8099/inference"
    model_path: "/opt/whisper/models/ggml-large-v3-turbo-q5_0.bin"
    device: "1"
  llm:
    endpoint: "http://127.0.0.1:11434"
    model: "qwen2.5:1.5b-instruct"
    temperature: 0.5
    keep_alive: "-1"
  tts:
    command: "piper"
    model_path: "/opt/piper/models/hu_HU-anna-medium.onnx"
    voice: "anna"
```

---

## 7. English Tool Calling & Model Context Protocol (MCP)

In accordance with enterprise standards, **all tool definitions, function names, parameter schemas, and API documentation are 100% English**. Only caller dialogue and conversational prompts are in Hungarian.

### Example JSON Function Payload to Ollama:

```json
{
  "model": "qwen2.5:1.5b-instruct",
  "messages": [
    {
      "role": "system",
      "content": "Te vagy Maci, a Gobelisk PBX intelligens telefonos ügyfélszolgálati asszisztense. Az ügyféllel udvariasan, természetes magyar nyelven beszélj. Amikor művelet végrehajtására van szükség, hívd meg a megfelelő angol nyelvű függvényt."
    },
    {
      "role": "user",
      "content": "Szeretném megkérdezni, hogy mikor jár le a számlám, az ügyfél-azonosítóm CUST-8831."
    }
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_billing_invoice",
        "description": "Retrieve current billing invoice status and due date for customer.",
        "parameters": {
          "type": "object",
          "properties": {
            "customer_id": {
              "type": "string",
              "description": "Unique customer account identifier"
            }
          },
          "required": ["customer_id"]
        }
      }
    }
  ],
  "stream": false
}
```

### Model Output:
```json
{
  "name": "get_billing_invoice",
  "arguments": {
    "customer_id": "CUST-8831"
  }
}
```

The Go PBX host executes the tool, returns the result as a `tool` role message, and Ollama produces the natural Hungarian spoken answer:
> *"A CUST-8831 azonosítójú számla fizetési határideje november 15., a fennálló összeg 14 500 Ft."*

The response text is immediately synthesized into speech by Piper Neural TTS and streamed back to the caller.
