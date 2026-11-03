package voiceagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/rs/zerolog"

	"github.com/devopshaven/gobelisk/pkg/config"
)

func TestIssueGrant(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"grant":"token-1","signaling_url":"/ws"}`))
	}))
	defer srv.Close()

	cfg := config.VoiceAgentConfig{
		UID:                "agent",
		DisplayName:        "Agent",
		Role:               "guest",
		MeetingSessionID:   "session 1",
		MediaServiceURL:    srv.URL,
		MediaInternalToken: "secret",
	}
	grant, signaling, err := IssueGrant(context.Background(), cfg)
	if err != nil {
		t.Fatalf("IssueGrant: %v", err)
	}
	if grant != "token-1" || signaling != "/ws" {
		t.Errorf("grant = %q, signaling = %q", grant, signaling)
	}
	if !strings.Contains(gotPath, "session%201") {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotBody["uid"] != "agent" || gotBody["display_name"] != "Agent" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestResolveSignalingURL(t *testing.T) {
	cases := []struct {
		media, signaling, want string
	}{
		{"http://media:8090", "/ws", "ws://media:8090/ws"},
		{"https://media.example", "/ws", "wss://media.example/ws"},
		{"http://media:8090", "ws://sfu:7000/ws", "ws://sfu:7000/ws"},
		{"", "/ws", "/ws"},
	}
	for _, c := range cases {
		if got := resolveSignalingURL(c.media, c.signaling); got != c.want {
			t.Errorf("resolveSignalingURL(%q,%q) = %q, want %q", c.media, c.signaling, got, c.want)
		}
	}
}

// TestSFUHandshakeAndAudio runs a real Pion peer connection against a fake SFU
// that speaks the same JSON-RPC signaling as Pir². It verifies the join
// handshake, trickle ICE, received meeting audio and published agent audio.
func TestSFUHandshakeAndAudio(t *testing.T) {
	var (
		mu          sync.Mutex
		serverTrack *webrtc.TrackLocalStaticSample
	)

	connected := make(chan struct{})
	var connectedOnce sync.Once
	received := make(chan *rtp.Packet, 256)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var wsWriteMu sync.Mutex
	writeJSON := func(conn *websocket.Conn, v any) {
		wsWriteMu.Lock()
		defer wsWriteMu.Unlock()
		_ = conn.WriteJSON(v)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/internal/sessions/{sid}/grant", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"grant":"test-grant","signaling_url":"/ws"}`))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		me := &webrtc.MediaEngine{}
		_ = me.RegisterDefaultCodecs()
		api := webrtc.NewAPI(webrtc.WithMediaEngine(me))
		pc, err := api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			return
		}
		defer pc.Close()

		track, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1},
			"sfu-audio", "peer-1",
		)
		if err != nil {
			return
		}
		if _, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendrecv,
		}); err != nil {
			return
		}

		pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			for {
				pkt, _, err := remote.ReadRTP()
				if err != nil {
					return
				}
				select {
				case received <- pkt:
				default:
				}
			}
		})
		pc.OnICECandidate(func(c *webrtc.ICECandidate) {
			if c == nil {
				return
			}
			writeJSON(conn, map[string]any{
				"jsonrpc": "2.0",
				"method":  "trickle",
				"params":  map[string]any{"candidate": c.ToJSON(), "target": 0},
			})
		})
		pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			if state == webrtc.PeerConnectionStateConnected {
				connectedOnce.Do(func() { close(connected) })
			}
		})

		mu.Lock()
		serverTrack = track
		mu.Unlock()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			switch method {
			case "join":
				offerMap, _ := params["offer"].(map[string]any)
				offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerMap["sdp"].(string)}
				if err := pc.SetRemoteDescription(offer); err != nil {
					return
				}
				answer, err := pc.CreateAnswer(nil)
				if err != nil {
					return
				}
				if err := pc.SetLocalDescription(answer); err != nil {
					return
				}
				writeJSON(conn, map[string]any{
					"jsonrpc": "2.0",
					"id":      msg["id"],
					"result":  pc.LocalDescription(),
				})
			case "trickle":
				candMap, _ := params["candidate"].(map[string]any)
				raw, _ := json.Marshal(candMap)
				var init webrtc.ICECandidateInit
				if json.Unmarshal(raw, &init) == nil {
					_ = pc.AddICECandidate(init)
				}
			case "answer":
				descMap, _ := params["desc"].(map[string]any)
				desc := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: descMap["sdp"].(string)}
				_ = pc.SetRemoteDescription(desc)
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.VoiceAgentConfig{
		UID:                "agent",
		DisplayName:        "Agent",
		MeetingSessionID:   "session-1",
		MediaServiceURL:    srv.URL,
		MediaInternalToken: "secret",
	}
	client := NewSFUClient(cfg, zerolog.Nop())
	defer client.Close()

	var audioMu sync.Mutex
	var heard int
	client.OnAudio(func(pcm []int16) {
		audioMu.Lock()
		heard += len(pcm)
		audioMu.Unlock()
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := client.WaitReady(readyCtx); err != nil {
		readyCancel()
		t.Fatalf("WaitReady: %v", err)
	}
	readyCancel()

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("peer connection did not connect")
	}

	// The fake SFU sends 200 ms of μ-law audio; the agent must decode it.
	mu.Lock()
	track := serverTrack
	mu.Unlock()
	go func() {
		frame := make([]byte, pcmuFrameBytes)
		for i := 0; i < 10; i++ {
			_ = track.WriteSample(media.Sample{Data: frame, Duration: 20 * time.Millisecond})
			time.Sleep(20 * time.Millisecond)
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		audioMu.Lock()
		n := heard
		audioMu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	audioMu.Lock()
	if heard == 0 {
		audioMu.Unlock()
		t.Fatal("agent did not receive meeting audio")
	}
	audioMu.Unlock()

	// The agent publishes PCMU; the fake SFU must receive RTP packets.
	speech := Speech{PCM: make([]int16, SampleRate16k/5), SampleRate: SampleRate16k}
	for i := range speech.PCM {
		speech.PCM[i] = 1000
	}
	if err := client.WritePCM(context.Background(), speech); err != nil {
		t.Fatalf("WritePCM: %v", err)
	}

	select {
	case pkt := <-received:
		if len(pkt.Payload) != pcmuFrameBytes {
			t.Errorf("payload = %d bytes, want %d", len(pkt.Payload), pcmuFrameBytes)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fake SFU did not receive agent audio")
	}
}
