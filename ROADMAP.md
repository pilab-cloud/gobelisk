# Gobelisk Roadmap

Gobelisk is a Go SIP PBX / SIP edge focused on Pir² meeting dial-in.
This roadmap compares Gobelisk against a typical FreePBX feature set.

Legend: ✅ implemented, 🟡 partial/scaffold, ❌ missing

Issue links point at the [PiR@ issue tracker](https://pira.hu) (`https://pira.hu/Projects/GOBELISK/Browse/<KEY>`).

## Core SIP

- ✅ SIP registration (UDP/TCP/TLS)
- ✅ INVITE/ACK/BYE/CANCEL/PRACK/UPDATE handling
- ✅ Call forwarding between registered endpoints
- ✅ Re-INVITE / hold-resume forwarding
- ✅ Hold/resume policy — [GOBELISK-10](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-10)
- ✅ Attended/blind transfer — [GOBELISK-11](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-11)
- ✅ Call parking / pickup — [GOBELISK-12](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-12)
- ✅ Call waiting — [GOBELISK-13](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-13)
- ✅ Do Not Disturb — [GOBELISK-14](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-14)
- ✅ Call forwarding (unconditional/busy/no-answer) — [GOBELISK-15](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-15)
- ❌ Paging / intercom — [GOBELISK-16](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-16)
- ❌ Callback / DISA — [GOBELISK-17](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-17)
- ✅ Feature codes — [GOBELISK-18](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-18)

## Routing and dialplan

- ✅ Dialplan rules with wildcard matching
- ✅ Extension mapping
- ✅ Room mapping
- ✅ Menu mapping
- ✅ Meeting mapping
- ✅ IVR decision-language config
- 🟡 Time conditions / time groups — [GOBELISK-19](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-19)
- ❌ Inbound routes (DID → destination) — [GOBELISK-20](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-20)
- ❌ Outbound routes (pattern → trunk) — [GOBELISK-21](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-21)
- ❌ Trunk selection / least-cost routing — [GOBELISK-22](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-22)
- ❌ Caller ID manipulation — [GOBELISK-23](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-23)

## Extensions and trunks

- ✅ Extension CRUD via management RPC
- ✅ Extension registration lookup
- ✅ Trunk provisioning (SIP/PSTN/IAX) — [GOBELISK-24](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-24)
- ✅ Trunk registration and health — [GOBELISK-25](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-25)
- ❌ Extension templates / bulk provisioning — [GOBELISK-26](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-26)
- ❌ Device provisioning (XML/HTTP) — [GOBELISK-27](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-27)

## IVR

- ✅ IVR menus
- ✅ DTMF collection via SIP INFO
- ✅ PIN collection per call
- 🟡 PIN validation against Pir² meeting access codes
- ❌ Nested/time-based IVR flows — [GOBELISK-28](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-28)
- ✅ TTS/ASR prompts & Neural AI IVR (Piper TTS + Vulkan GPU Whisper + Qwen 3B Instruct) — [GOBELISK-29](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-29)
- ❌ IVR recording — [GOBELISK-30](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-30)

## Voicemail

- 🟡 Voicemail storage scaffold
- ✅ Voicemail transcription via Whisper Large-v3-Turbo (GPU accelerated) — [GOBELISK-35](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-35)
- 🟡 Email notification scaffold
- ❌ Per-extension voicemail boxes — [GOBELISK-31](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-31)
- ❌ Greetings (busy/unavailable) — [GOBELISK-32](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-32)
- ❌ Message waiting indication (MWI) — [GOBELISK-33](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-33)
- ❌ Voicemail to email with attachments — [GOBELISK-34](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-34)
- ❌ Retention policies / admin — [GOBELISK-36](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-36)

## Conferencing

- ✅ Basic room participant tracking
- 🟡 Room list/create/remove via RPC
- ❌ Moderated conference bridge — [GOBELISK-37](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-37)
- ❌ Conference PINs — [GOBELISK-38](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-38)
- ❌ Conference recording — [GOBELISK-39](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-39)
- ❌ Mute/kick/moderation controls — [GOBELISK-40](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-40)
- ❌ Max participants / policies — [GOBELISK-41](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-41)

## Call center

- ❌ Queues / ACD — [GOBELISK-42](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-42)
- ❌ Agent login/logout — [GOBELISK-43](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-43)
- 🟡 Ring strategies (round-robin, least-recent, etc.) — [GOBELISK-44](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-44)
- ❌ Wrap-up time — [GOBELISK-45](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-45)
- ❌ Queue SLA reporting — [GOBELISK-46](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-46)
- ✅ Ring groups — [GOBELISK-47](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-47)

## Recording and analytics

- ❌ Call recording (start/stop/store/playback) — [GOBELISK-48](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-48)
- ❌ CDR / call detail records — [GOBELISK-49](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-49)
- ❌ Call reports and analytics — [GOBELISK-50](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-50)
- ❌ Call monitoring / whisper / barge — [GOBELISK-51](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-51)

## Pir² meeting integration

- ✅ Per-meeting access codes
- ✅ ResolveCallableMeeting client
- ✅ IVR PIN → meeting resolution
- 🟡 SIP bridge media interworking — [GOBELISK-52](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-52)
- ❌ RTP bridging to SFU peers — [GOBELISK-53](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-53)
- ❌ Admission policy (auto-admit vs waiting room) — [GOBELISK-54](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-54)
- ❌ End-to-end dial-in tests — [GOBELISK-55](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-55)

## Voice agent

Gobelisk hosts a GPU-backed voice agent that joins a Pir² meeting over WebRTC or acts as a local Hungarian AI customer service assistant
(see [docs/voice-agent.md](docs/voice-agent.md) and [docs/ai-customer-service/README.md](docs/ai-customer-service/README.md)).

- ✅ WebRTC agent joins the Pir² SFU with a service join grant
- ✅ Opus in / PCMU out audio path with barge-in
- ✅ Local GPU pipeline: Whisper Large-v3-Turbo (Vulkan compute) → Ollama LLM (Qwen2.5 1.5B/3B) → Piper Neural Hungarian TTS
- ✅ Interactive WebRTC/WebSocket customer service web UI (`gobelisk web`) with live audio visualizer
- ✅ Sliding-window real-time live captions & streaming speech transcription
- ✅ Wake-word gating ("Maci") with phonetic aliasing table, prompt biasing & instant wake acknowledgement
- ✅ Multi-turn conversational memory & acoustic feedback loop suppression
- ✅ Fully decoupled configuration via `gobelisk.yaml`
- ✅ Transcript export to the Pir² recorder
- ✅ [Inviting the agent to a conversation](docs/invite-agent.md)
- 🟡 Agent appears in the meeting roster / invitation UX
- ❌ SIP call → voice agent bridge — [GOBELISK-53](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-53)

## System and admin

- ✅ gRPC management API
- ✅ Config get/set/reload
- ✅ User management
- ✅ Dialplan management
- 🟡 Status/health
- ✅ Feature codes — [GOBELISK-18](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-18)
- ❌ Music on hold — [GOBELISK-56](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-56)
- ❌ Announcements — [GOBELISK-57](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-57)
- ❌ LDAP/AD integration — [GOBELISK-58](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-58)
- ❌ Backup/restore — [GOBELISK-59](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-59)
- ❌ Multi-tenant / multi-domain — [GOBELISK-60](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-60)
- ❌ Failover / HA / clustering — [GOBELISK-61](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-61)
- ❌ Security hardening (firewall, IDS, ACLs, SRTP enforcement) — [GOBELISK-62](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-62)

## Integrations

- ✅ Pir² Meeting Service client
- ❌ CRM/ERP integrations — [GOBELISK-63](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-63)
- ❌ REST/GraphQL APIs — [GOBELISK-64](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-64)
- ❌ WebRTC softphone provisioning — [GOBELISK-65](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-65)
- ❌ Fax (T.38) — [GOBELISK-66](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-66)

## Tracker epics

Umbrella issues in the tracker grouping the work above:

- [GOBELISK-1](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-1) FreePBX feature parity roadmap
- [GOBELISK-2](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-2) Trunks and inbound/outbound routes
- [GOBELISK-3](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-3) Full voicemail system
- [GOBELISK-4](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-4) Moderated conferencing bridge
- [GOBELISK-5](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-5) Call queues and ring groups
- [GOBELISK-6](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-6) Call recording and CDR/analytics
- [GOBELISK-7](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-7) Advanced IVR and feature codes
- [GOBELISK-8](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-8) System admin: MOH, announcements, security, HA
- [GOBELISK-9](https://pira.hu/Projects/GOBELISK/Browse/GOBELISK-9) Pir2 SIP bridge media interworking

## Notes

Gobelisk is intentionally scoped as a SIP edge for Pir² meetings. General PBX parity with FreePBX is not a near-term goal unless explicitly prioritized.
