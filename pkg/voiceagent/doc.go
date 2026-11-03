// Package voiceagent runs a GPU-backed AI participant inside a Pir² meeting.
//
// The agent joins the Pir² SFU over WebRTC using a service join grant, decodes
// the meeting audio (Opus or G.711), segments it with a small energy VAD,
// transcribes each utterance through a faster-whisper HTTP service, asks an
// OpenAI-compatible local LLM for a reply, synthesizes the reply with a local
// GPU TTS server and publishes it back to the meeting as G.711 (PCMU).
//
// Use NewSFUClient to join a session, NewPipeline to build the conversation
// loop, and wire them together:
//
//	sfu := voiceagent.NewSFUClient(cfg, log)
//	pipe := voiceagent.NewPipeline(cfg, log)
//	sfu.OnAudio(pipe.HandlePCM)
//	pipe.SetSpeaker(sfu)
//	if err := sfu.Connect(ctx); err != nil { ... }
//	go pipe.Run(ctx)
//
// The pipeline also writes a local transcript and can export it to the Pir²
// recorder. See docs/voice-agent.md for the deployment and configuration
// reference.
package voiceagent
