# Gobelisk Roadmap

Gobelisk is a Go SIP PBX / SIP edge focused on Pir² meeting dial-in.
This roadmap compares Gobelisk against a typical FreePBX feature set.

Legend: ✅ implemented, 🟡 partial/scaffold, ❌ missing

## Core SIP

- ✅ SIP registration (UDP/TCP/TLS)
- ✅ INVITE/ACK/BYE/CANCEL/PRACK/UPDATE handling
- ✅ Call forwarding between registered endpoints
- ✅ Re-INVITE / hold-resume forwarding
- 🟡 Hold/resume policy
- ❌ Attended/blind transfer
- ❌ Call parking / pickup
- ❌ Call waiting
- ❌ Do Not Disturb
- ❌ Call forwarding (unconditional/busy/no-answer)
- ❌ Paging / intercom
- ❌ Callback / DISA
- ❌ Feature codes

## Routing and dialplan

- ✅ Dialplan rules with wildcard matching
- ✅ Extension mapping
- ✅ Room mapping
- ✅ Menu mapping
- ✅ Meeting mapping
- ✅ IVR decision-language config
- 🟡 Time conditions / time groups
- ❌ Inbound routes (DID → destination)
- ❌ Outbound routes (pattern → trunk)
- ❌ Trunk selection / least-cost routing
- ❌ Caller ID manipulation

## Extensions and trunks

- ✅ Extension CRUD via management RPC
- ✅ Extension registration lookup
- ❌ Trunk provisioning (SIP/PSTN/IAX)
- ❌ Trunk registration and health
- ❌ Extension templates / bulk provisioning
- ❌ Device provisioning (XML/HTTP)

## IVR

- ✅ IVR menus
- ✅ DTMF collection via SIP INFO
- ✅ PIN collection per call
- 🟡 PIN validation against Pir² meeting access codes
- ❌ Nested/time-based IVR flows
- ❌ TTS/ASR prompts
- ❌ IVR recording

## Voicemail

- 🟡 Voicemail storage scaffold
- 🟡 Transcription placeholder
- 🟡 Email notification scaffold
- ❌ Per-extension voicemail boxes
- ❌ Greetings (busy/unavailable)
- ❌ Message waiting indication (MWI)
- ❌ Voicemail to email with attachments
- ❌ Retention policies / admin

## Conferencing

- ✅ Basic room participant tracking
- 🟡 Room list/create/remove via RPC
- ❌ Moderated conference bridge
- ❌ Conference PINs
- ❌ Conference recording
- ❌ Mute/kick/moderation controls
- ❌ Max participants / policies

## Call center

- ❌ Queues / ACD
- ❌ Agent login/logout
- ❌ Ring strategies (round-robin, least-recent, etc.)
- ❌ Wrap-up time
- ❌ Queue SLA reporting
- ❌ Ring groups

## Recording and analytics

- ❌ Call recording (start/stop/store/playback)
- ❌ CDR / call detail records
- ❌ Call reports and analytics
- ❌ Call monitoring / whisper / barge

## Pir² meeting integration

- ✅ Per-meeting access codes
- ✅ ResolveCallableMeeting client
- ✅ IVR PIN → meeting resolution
- 🟡 SIP bridge media interworking
- ❌ RTP bridging to SFU peers
- ❌ Admission policy (auto-admit vs waiting room)
- ❌ End-to-end dial-in tests

## System and admin

- ✅ gRPC management API
- ✅ Config get/set/reload
- ✅ User management
- ✅ Dialplan management
- 🟡 Status/health
- ❌ Feature codes
- ❌ Music on hold
- ❌ Announcements
- ❌ LDAP/AD integration
- ❌ Backup/restore
- ❌ Multi-tenant / multi-domain
- ❌ Failover / HA / clustering
- ❌ Security hardening (firewall, IDS, ACLs, SRTP enforcement)

## Integrations

- ✅ Pir² Meeting Service client
- ❌ CRM/ERP integrations
- ❌ REST/GraphQL APIs
- ❌ WebRTC softphone provisioning
- ❌ Fax (T.38)

## Notes

Gobelisk is intentionally scoped as a SIP edge for Pir² meetings. General PBX parity with FreePBX is not a near-term goal unless explicitly prioritized.
