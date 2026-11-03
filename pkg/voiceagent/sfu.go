package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/opus"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/rs/zerolog"

	"github.com/devopshaven/gobelisk/pkg/config"
)

const (
	agentTrackID     = "voice-agent-audio"
	agentStreamID    = "voice-agent"
	pcmuFrameBytes   = SampleRate8k * 20 / 1000 // 160 bytes for 20 ms
	pcmuSilenceByte  = 0xff
	joinTimeout      = 20 * time.Second
	trickleTargetAll = 0
)

// AudioHandler receives decoded 16 kHz mono PCM from the meeting.
type AudioHandler func(pcm16k []int16)

// SFUClient joins a Pir² media session over WebRTC, publishes a G.711 audio
// track (what the agent says) and forwards decoded meeting audio to a handler.
type SFUClient struct {
	cfg config.VoiceAgentConfig
	log zerolog.Logger

	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticSample
	ws    *websocket.Conn

	writeMu sync.Mutex
	reqID   atomic.Uint64
	joinID  atomic.Uint64
	respCh  chan rpcMessage

	onAudio AudioHandler

	closeOnce sync.Once
	done      chan struct{}
	ready     chan struct{}
	readyOnce sync.Once
}

// NewSFUClient builds a client. Call Connect to join the session.
func NewSFUClient(cfg config.VoiceAgentConfig, log zerolog.Logger) *SFUClient {
	cfg.ApplyDefaults()
	return &SFUClient{
		cfg:    cfg,
		log:    log,
		respCh: make(chan rpcMessage, 16),
		done:   make(chan struct{}),
		ready:  make(chan struct{}),
	}
}

// OnAudio registers the decoded meeting-audio callback.
func (c *SFUClient) OnAudio(handler AudioHandler) { c.onAudio = handler }

// Connect mints a join grant through the Pir² media control service and joins
// the SFU session.
func (c *SFUClient) Connect(ctx context.Context) error {
	if strings.TrimSpace(c.cfg.MeetingSessionID) == "" {
		return fmt.Errorf("voice agent: meeting_session_id is required")
	}

	grant, signalingURL, err := IssueGrant(ctx, c.cfg)
	if err != nil {
		return err
	}
	// The media service usually returns the browser-facing "/ws" gateway
	// path. A configured SFU signaling URL wins for the direct service path.
	if strings.TrimSpace(c.cfg.SFUSignalingURL) != "" &&
		(signalingURL == "" || strings.HasPrefix(signalingURL, "/")) {
		signalingURL = c.cfg.SFUSignalingURL
	}
	return c.Join(ctx, grant, signalingURL)
}

// Join joins an SFU session with a pre-issued grant. This is the path used
// when a meeting organizer invites the agent by minting a grant elsewhere.
func (c *SFUClient) Join(ctx context.Context, grant, signalingURL string) error {
	if strings.TrimSpace(grant) == "" {
		return fmt.Errorf("voice agent: join grant is required")
	}
	if strings.TrimSpace(c.cfg.MeetingSessionID) == "" {
		return fmt.Errorf("voice agent: meeting_session_id is required")
	}
	if strings.TrimSpace(signalingURL) == "" {
		signalingURL = c.cfg.SFUSignalingURL
	}
	signalingURL = resolveSignalingURL(c.cfg.MediaServiceURL, signalingURL)
	if signalingURL == "" {
		return fmt.Errorf("voice agent: no SFU signaling url configured")
	}

	pc, track, err := c.newPeerConnection()
	if err != nil {
		return err
	}
	c.pc = pc
	c.track = track

	ws, _, err := websocket.DefaultDialer.DialContext(ctx, signalingURL, nil)
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("voice agent: sfu dial %s: %w", signalingURL, err)
	}
	c.ws = ws

	go c.readLoop()

	if err := c.sendJoin(ctx, grant); err != nil {
		c.Close()
		return err
	}

	c.log.Info().
		Str("session", c.cfg.MeetingSessionID).
		Str("uid", c.cfg.UID).
		Str("displayName", c.cfg.DisplayName).
		Str("sfu", signalingURL).
		Msg("voice agent joined meeting")

	return nil
}

func (c *SFUClient) newPeerConnection() (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, nil, fmt.Errorf("voice agent: media engine: %w", err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, fmt.Errorf("voice agent: peer connection: %w", err)
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: SampleRate8k, Channels: 1},
		agentTrackID, agentStreamID,
	)
	if err != nil {
		_ = pc.Close()
		return nil, nil, fmt.Errorf("voice agent: audio track: %w", err)
	}

	if _, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendrecv,
	}); err != nil {
		_ = pc.Close()
		return nil, nil, fmt.Errorf("voice agent: transceiver: %w", err)
	}

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		// Ignore an echo of our own published stream.
		if remote.StreamID() == agentStreamID {
			return
		}
		c.log.Debug().Str("codec", remote.Codec().MimeType).Str("stream", remote.StreamID()).Msg("voice agent: remote audio track")
		go c.readTrack(remote)
	})

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		_ = c.sendNotification("trickle", map[string]any{
			"candidate": candidate.ToJSON(),
			"target":    trickleTargetAll,
		})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.log.Info().Str("state", state.String()).Msg("voice agent: peer connection state")
		if state == webrtc.PeerConnectionStateConnected {
			c.readyOnce.Do(func() { close(c.ready) })
		}
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			c.Close()
		}
	})

	return pc, track, nil
}

func (c *SFUClient) sendJoin(ctx context.Context, grant string) error {
	offer, err := c.pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("voice agent: create offer: %w", err)
	}
	if err := c.pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("voice agent: set local description: %w", err)
	}

	select {
	case <-webrtc.GatheringCompletePromise(c.pc):
	case <-ctx.Done():
		return ctx.Err()
	}

	id := c.reqID.Add(1)
	c.joinID.Store(id)
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "join",
		"params": map[string]any{
			"sid":    c.cfg.MeetingSessionID,
			"uid":    c.cfg.UID,
			"token":  grant,
			"offer":  c.pc.LocalDescription(),
			"config": map[string]any{},
		},
	}
	if err := c.write(msg); err != nil {
		return err
	}

	timeout := time.NewTimer(joinTimeout)
	defer timeout.Stop()
	for {
		select {
		case resp := <-c.respCh:
			if resp.ID == nil || *resp.ID != id {
				continue
			}
			if resp.Error != nil {
				return fmt.Errorf("voice agent: join rejected: %s", resp.Error.Message)
			}
			var answer webrtc.SessionDescription
			if err := json.Unmarshal(resp.Result, &answer); err != nil {
				return fmt.Errorf("voice agent: decode answer: %w", err)
			}
			if answer.SDP == "" {
				return fmt.Errorf("voice agent: empty answer")
			}
			if err := c.pc.SetRemoteDescription(answer); err != nil {
				return fmt.Errorf("voice agent: set remote description: %w", err)
			}
			c.joinID.Store(0)
			return nil
		case <-timeout.C:
			return fmt.Errorf("voice agent: join timed out")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *SFUClient) readLoop() {
	for {
		var msg rpcMessage
		if err := c.ws.ReadJSON(&msg); err != nil {
			select {
			case <-c.done:
			default:
				c.log.Debug().Err(err).Msg("voice agent: signaling closed")
				c.Close()
			}
			return
		}
		if msg.Method != "" {
			c.handleNotification(msg)
			continue
		}
		// Only the join request has a response we consume. Replies to
		// server-initiated renegotiation answers are intentionally ignored.
		if msg.ID == nil || *msg.ID != c.joinID.Load() {
			continue
		}
		select {
		case c.respCh <- msg:
		default:
			c.log.Warn().Msg("voice agent: signaling response queue full")
		}
	}
}

func (c *SFUClient) handleNotification(msg rpcMessage) {
	switch msg.Method {
	case "offer":
		var offer webrtc.SessionDescription
		if err := json.Unmarshal(msg.Params, &offer); err != nil {
			c.log.Warn().Err(err).Msg("voice agent: bad server offer")
			return
		}
		if err := c.pc.SetRemoteDescription(offer); err != nil {
			c.log.Warn().Err(err).Msg("voice agent: set server offer")
			return
		}
		answer, err := c.pc.CreateAnswer(nil)
		if err != nil {
			c.log.Warn().Err(err).Msg("voice agent: create answer")
			return
		}
		if err := c.pc.SetLocalDescription(answer); err != nil {
			c.log.Warn().Err(err).Msg("voice agent: set local answer")
			return
		}
		local := c.pc.LocalDescription()
		_ = c.write(map[string]any{
			"jsonrpc": "2.0",
			"id":      c.reqID.Add(1),
			"method":  "answer",
			"params":  map[string]any{"desc": local},
		})
	case "trickle":
		var params struct {
			Candidate webrtc.ICECandidateInit `json:"candidate"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			c.log.Debug().Err(err).Msg("voice agent: bad trickle")
			return
		}
		if params.Candidate.Candidate != "" {
			if err := c.pc.AddICECandidate(params.Candidate); err != nil {
				c.log.Debug().Err(err).Msg("voice agent: add ice candidate")
			}
		}
	}
}

func (c *SFUClient) readTrack(track *webrtc.TrackRemote) {
	codec := track.Codec()
	switch strings.ToLower(codec.MimeType) {
	case "audio/opus":
		c.readOpus(track)
	case "audio/pcmu":
		c.readPCMU(track)
	case "audio/pcma":
		c.readPCMA(track)
	default:
		// Drain unsupported tracks so the SFU does not stall.
		for {
			if _, _, err := track.ReadRTP(); err != nil {
				return
			}
		}
	}
}

func (c *SFUClient) readOpus(track *webrtc.TrackRemote) {
	decoder, err := opus.NewDecoderWithOutput(SampleRate16k, 1)
	if err != nil {
		c.log.Warn().Err(err).Msg("voice agent: opus decoder")
		return
	}
	buf := make([]int16, 5760) // 120 ms at 16 kHz
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		n, err := decoder.DecodeToInt16(pkt.Payload, buf)
		if err != nil || n == 0 {
			continue
		}
		c.deliver(buf[:n])
	}
}

func (c *SFUClient) readPCMU(track *webrtc.TrackRemote) {
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		pcm8k := DecodeULawFrame(pkt.Payload)
		c.deliver(Resample(pcm8k, SampleRate8k, SampleRate16k))
	}
}

func (c *SFUClient) readPCMA(track *webrtc.TrackRemote) {
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		pcm8k := make([]int16, len(pkt.Payload))
		for i, b := range pkt.Payload {
			pcm8k[i] = decodeALaw(b)
		}
		c.deliver(Resample(pcm8k, SampleRate8k, SampleRate16k))
	}
}

func (c *SFUClient) deliver(pcm []int16) {
	if c.onAudio == nil || len(pcm) == 0 {
		return
	}
	c.onAudio(pcm)
}

// WritePCM publishes speech to the meeting as G.711 μ-law, paced in 20 ms
// frames. It returns when the whole speech has been sent or ctx is cancelled
// (barge-in).
func (c *SFUClient) WritePCM(ctx context.Context, speech Speech) error {
	if c.track == nil {
		return fmt.Errorf("voice agent: not connected")
	}
	if len(speech.PCM) == 0 {
		return nil
	}
	pcm8k := Resample(speech.PCM, speech.SampleRate, SampleRate8k)
	payload := make([]byte, len(pcm8k))
	for i, s := range pcm8k {
		payload[i] = EncodeULaw(s)
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for off := 0; off < len(payload); off += pcmuFrameBytes {
		end := off + pcmuFrameBytes
		if end > len(payload) {
			end = len(payload)
		}
		frame := make([]byte, pcmuFrameBytes)
		copy(frame, payload[off:end])
		for i := end - off; i < pcmuFrameBytes; i++ {
			frame[i] = pcmuSilenceByte
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		if err := c.track.WriteSample(media.Sample{Data: frame, Duration: 20 * time.Millisecond}); err != nil {
			return fmt.Errorf("voice agent: write audio: %w", err)
		}
	}
	return nil
}

// WaitReady blocks until ICE finishes connecting or ctx is done.
func (c *SFUClient) WaitReady(ctx context.Context) error {
	select {
	case <-c.ready:
		return nil
	case <-c.done:
		return fmt.Errorf("voice agent: meeting connection closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close leaves the meeting and releases the peer connection.
func (c *SFUClient) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.ws != nil {
			_ = c.ws.Close()
		}
		if c.pc != nil {
			_ = c.pc.Close()
		}
	})
}

func (c *SFUClient) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.ws == nil {
		return fmt.Errorf("voice agent: signaling not connected")
	}
	return c.ws.WriteJSON(v)
}

func (c *SFUClient) sendNotification(method string, params any) error {
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

type rpcMessage struct {
	ID     *uint64         `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// IssueGrant asks the Pir² media control service for a service join grant.
// This is the same internal path the Pir² recorder uses; it does not require
// an end-user SSO token.
func IssueGrant(ctx context.Context, cfg config.VoiceAgentConfig) (grant, signalingURL string, err error) {
	if strings.TrimSpace(cfg.MediaServiceURL) == "" {
		return "", "", fmt.Errorf("voice agent: media_service_url is required")
	}
	body, _ := json.Marshal(map[string]string{
		"uid":          cfg.UID,
		"display_name": cfg.DisplayName,
		"role":         cfg.Role,
	})
	base := strings.TrimRight(cfg.MediaServiceURL, "/")
	endpoint := base + "/v1/internal/sessions/" + url.PathEscape(cfg.MeetingSessionID) + "/grant"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.MediaInternalToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.MediaInternalToken)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("voice agent: grant request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("voice agent: grant: %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Grant        string `json:"grant"`
		SignalingURL string `json:"signaling_url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("voice agent: decode grant: %w", err)
	}
	if out.Grant == "" {
		return "", "", fmt.Errorf("voice agent: empty grant")
	}
	return out.Grant, out.SignalingURL, nil
}

func resolveSignalingURL(mediaURL, signalingURL string) string {
	signalingURL = strings.TrimSpace(signalingURL)
	if strings.HasPrefix(signalingURL, "ws://") || strings.HasPrefix(signalingURL, "wss://") {
		return signalingURL
	}
	if strings.HasPrefix(signalingURL, "/") && mediaURL != "" {
		u, err := url.Parse(mediaURL)
		if err == nil {
			scheme := "ws"
			if u.Scheme == "https" {
				scheme = "wss"
			}
			return scheme + "://" + u.Host + signalingURL
		}
	}
	return signalingURL
}

// decodeALaw converts one G.711 A-law byte to 16-bit PCM.
func decodeALaw(a byte) int16 {
	a ^= 0x55
	sign := a & 0x80
	exponent := (a >> 4) & 0x07
	mantissa := a & 0x0f
	var sample int
	if exponent == 0 {
		sample = int(mantissa)<<4 + 8
	} else {
		sample = (int(mantissa)<<4 + 0x108) << (exponent - 1)
	}
	if sign == 0 {
		return int16(sample)
	}
	return int16(-sample)
}

// ensure rtp and media stay referenced for future tuning without changing the
// public surface.
var _ = rtp.Packet{}
