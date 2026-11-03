# Inviting the Voice Agent to a Live Conversation

This guide shows how to put the Gobelisk voice agent into a **live Pir²
meeting** (the audio conversation). There are three ways to invite it, from
most automated to most manual:

| Method | Who mints the grant | Best for |
|---|---|---|
| Agent self-service | the agent (using the media internal token) | trusted hosts, Docker Compose |
| Operator invite | `gobelisk agent invite` | handing a short-lived grant to an agent |
| Raw media API | any control plane (curl/HTTP) | custom schedulers, bots |

> **What "invite" means here:** the agent is not added to the chat thread or
> the meeting roster yet. It joins the media session as an SFU peer with the
> `uid` you choose. See [Visibility](#visibility) below.

---

## Prerequisites

1. Pir² is running: `media` (`:8090`), `sfu` signaling (`:7000`), `meeting`,
   and optionally `recorder` (`:8080`).
2. A meeting is **started** and its session is **ACTIVE**. A scheduled meeting
   with no live session cannot be joined.
3. You have the media internal token (`MEDIA_INTERNAL_TOKEN` in the Pir²
   deployment) for the self-service/invite paths.
4. The GPU stack is up: Whisper ASR, Ollama, TTS. See
   [voice-agent.md](voice-agent.md).
5. `gobelisk` is built (`go build -o gobelisk .`) or use `go run .`.

---

## Step 1 — Find the meeting session id

The agent needs the **meeting session id** (`meeting_session_id`), not the
meeting id. Ask the media service for active sessions:

```bash
export MEDIA=http://127.0.0.1:8090
export MEDIA_INTERNAL_TOKEN=change-me

curl -s -H "Authorization: Bearer $MEDIA_INTERNAL_TOKEN" \
  "$MEDIA/v1/internal/sessions" | jq
```

```json
{
  "sessions": [
    {
      "sid": "6a0c2f7e9b1d4c3a8e5f0b2d",
      "epoch": "e-1",
      "created_by": "internal",
      "created_at": "2026-11-03T19:00:00Z",
      "lease_expires_at": "2026-11-03T23:00:00Z"
    }
  ]
}
```

The `sid` is the meeting session id. You can also get it from the
MeetingService `StartMeeting` / `GetMeetingSession` response
(`meetingSessionId`) in your control plane.

```bash
export SID=6a0c2f7e9b1d4c3a8e5f0b2d
```

---

## Step 2 — Configure the agent

Minimal `config.yaml` (see
[`deploy/voice-agent/config.example.yaml`](../deploy/voice-agent/config.example.yaml)
for the full file):

```yaml
voice_agent:
  uid: voice-agent
  display_name: AI Assistant
  role: guest
  language: hu

  media_service_url: http://127.0.0.1:8090
  media_internal_token: "change-me"          # literal token, not ${VAR}
  sfu_signaling_url: ws://127.0.0.1:7000/ws  # internal SFU port

  asr:
    endpoint: http://127.0.0.1:8000/transcribe
    language: hu
  llm:
    endpoint: http://127.0.0.1:11434/v1/chat/completions
    model: qwen2.5:3b-instruct
    keep_alive: "-1"
  tts:
    endpoint: http://127.0.0.1:8880/v1/audio/speech
    model: tts-1-hd
    voice: alloy
    format: wav
  transcript:
    dir: ./transcripts
    recorder_url: http://127.0.0.1:8080
```

`sfu_signaling_url` must be the **SFU** signaling endpoint, not the media
service or the browser gateway. In the Pir² Docker deployment that is
`ws://127.0.0.1:7000/ws` when the agent runs on the same host.

---

## Step 3 — Invite and start the agent

### Option A — Agent self-service (recommended for Compose)

The agent mints its own grant through the media internal API:

```bash
gobelisk agent run --config config.yaml --session "$SID"
```

or, with Docker Compose:

```bash
cd deploy/voice-agent
MEETING_SESSION_ID="$SID" docker compose up -d agent
```

### Option B — Operator invite

Mint a grant now and hand it to the agent. This is useful when the agent runs
somewhere that should not hold the media internal token:

```bash
gobelisk agent invite \
  --config config.yaml \
  --session "$SID" \
  --display-name "AI Assistant" > invite.json

jq . invite.json
```

```json
{
  "grant": "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9...",
  "signaling_url": "ws://127.0.0.1:7000/ws",
  "session_id": "6a0c2f7e9b1d4c3a8e5f0b2d",
  "uid": "voice-agent",
  "display_name": "AI Assistant",
  "run_command": "gobelisk agent run --session ... --grant ... --signaling-url ws://127.0.0.1:7000/ws"
}
```

Then run the printed command:

```bash
grant=$(jq -r .grant invite.json)
url=$(jq -r .signaling_url invite.json)

gobelisk agent run \
  --config config.yaml \
  --session "$SID" \
  --grant "$grant" \
  --signaling-url "$url"
```

> Grants are short-lived (`MEDIA_GRANT_TTL`, default **5 minutes**) and bound
> to the session id and uid. Start the agent promptly; mint a new grant if it
> expired.

### Option C — Raw media API

Any control plane can invite the agent with one HTTP call:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MEDIA_INTERNAL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"uid":"voice-agent","display_name":"AI Assistant","role":"guest"}' \
  "$MEDIA/v1/internal/sessions/$SID/grant" | jq
```

The response contains `grant`, `signaling_url`, `expires_at`, and ICE servers.
Use the grant with `gobelisk agent run --grant ... --signaling-url ...`, or
implement the WebRTC client yourself (the JSON-RPC protocol is described in
[voice-agent.md](voice-agent.md)).

---

## Step 4 — Verify the agent is in the conversation

Agent logs should show:

```text
voice agent joined meeting   session=6a0c... uid=voice-agent sfu=ws://127.0.0.1:7000/ws
voice agent: peer connection state state=connected
voice agent running; press Ctrl-C to leave
voice agent: heard           text="Jó napot, miben segíthetek?"
```

Check the SFU peer list through the media service:

```bash
curl -s -H "Authorization: Bearer $MEDIA_INTERNAL_TOKEN" \
  "$MEDIA/v1/internal/sessions/$SID/peers" | jq
```

You should see a peer with `uid = voice-agent`.

Speak in the meeting. The agent waits for an endpoint (about 450 ms of
silence), then replies. The transcript is written under
`transcript.dir`:

```bash
ls -1 transcripts/
# 6a0c2f7e9b1d4c3a8e5f0b2d.json
# 6a0c2f7e9b1d4c3a8e5f0b2d.txt
```

If `transcript.recorder_url` is set, the same transcript is exported to the
Pir² recorder as a completed transcription job.

---

## Step 5 — Remove the agent

```bash
# direct run
Ctrl-C

# docker compose
cd deploy/voice-agent
docker compose stop agent
```

Stopping the agent leaves the media session. The remaining participants stay
in the meeting. The agent does not end the meeting.

---

## Visibility

The agent joins as an **SFU service peer**:

- ✅ Other participants hear it; it hears the meeting mix.
- ✅ It appears in the SFU/media peer list (`uid`).
- ❌ It does **not** appear in the Pir² meeting participant roster or the chat
  thread yet. The current meeting service only models user and guest
  participants, and the agent bypasses the signaling gateway (which enforces
  roster admission) because it connects directly to the SFU.

If organizers need to see and manage the agent in the UI, the next step is a
small Pir² meeting-service change: add a service/agent principal type and an
internal participant upsert. Until then, use the peer list and logs to confirm
it is present.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `grant: 401 Unauthorized` | missing/wrong `media_internal_token` | use the exact `MEDIA_INTERNAL_TOKEN` from Pir² |
| `grant: 404` | session id not found or meeting not started | list `/v1/internal/sessions`; start the meeting |
| `sfu dial ... 404` / `unexpected EOF` | `sfu_signaling_url` points at media/gateway | use the SFU endpoint, e.g. `ws://127.0.0.1:7000/ws` |
| `join rejected: permission denied` | grant for another session/uid, or issuer/audience mismatch | mint a fresh grant for this `SID` and `uid` |
| Granted, but no audio is transcribed | SFU did not subscribe a track, or VAD threshold too high | check debug logs for `remote audio track`; lower `vad.energy_threshold` |
| Agent answers itself / loops | echo of its own published track | keep `uid` unique; the client filters the `voice-agent` stream, do not disable it |
| First reply is silent, TTS times out | XTTS model still loading (cold start) | wait for the TTS container to preload, or increase `tts.timeout_seconds` |
| `llm: 404 model not found` | model not pulled | `docker compose exec ollama ollama pull qwen2.5:3b-instruct` |
| ASR returns 500 | CUDA/cuBLAS/ctranslate2 problem | check the Whisper container logs and `WHISPER_DEVICE=cuda` |

Enable debug logging (`zerolog` global level) to see signaling, remote tracks
and VAD decisions.

---

## Security notes

- `media_internal_token` can mint grants for any session. Keep it on trusted
  hosts and never expose the media internal API publicly.
- Grants are short-lived bearer credentials bound to one session and uid.
  Treat `invite.json` as a secret and delete it after use.
- The agent connects to the SFU's **internal** signaling port. Do not publish
  that port; use host networking or a private overlay network.
- The agent has no SSO identity and no chat/roster permissions. It only
  carries a `media.join` grant.
