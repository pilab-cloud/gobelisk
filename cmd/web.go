package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/devopshaven/gobelisk/gen/go/gobelisk/v1"
	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/spf13/cobra"
)

var webPort int

// WebCallSession tracks a live mock/PBX call in the web tester.
type WebCallSession struct {
	CallID     string    `json:"call_id"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	StartTime  time.Time `json:"start_time"`
	DTMFBuffer string    `json:"dtmf_buffer"`
	Transcript []string  `json:"transcript"`
	State      string    `json:"state"` // idle, connected, ended
}

var (
	callSessionsMu sync.Mutex
	activeCall     *WebCallSession
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "start web dial pad tester",
	Run: func(cmd *cobra.Command, args []string) {
		if _, err := config.Load(cfgFile); err != nil {
			fmt.Printf("⚠️ Config betöltési hiba: %v (alapértelmezett beállítások használata)\n", err)
		}

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
			activeCall = &WebCallSession{
				CallID:     fmt.Sprintf("call-%d", time.Now().UnixNano()),
				From:       req.From,
				To:         req.To,
				StartTime:  time.Now(),
				DTMFBuffer: "",
				Transcript: []string{"Hívás csatlakoztatva: " + req.To},
				State:      "connected",
			}
			call := *activeCall
			callSessionsMu.Unlock()

			// Optionally notify gRPC if PBX is listening
			go func() {
				client, conn, err := dialRPC()
				if err == nil {
					defer conn.Close()
					_, _ = client.OriginateCall(withAuth(context.Background()), &gobeliskv1.OriginateCallRequest{From: req.From, To: req.To})
				}
			}()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"message": "Hívás felépült",
				"call_id": call.CallID,
				"to":      req.To,
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

			// DTMF IVR menu logic
			replyText := ""
			switch req.Digit {
			case "1":
				replyText = "Ön az 1-es gombot választotta: Számlázási és pénzügyi osztály."
			case "2":
				replyText = "Ön a 2-es gombot választotta: Műszaki segítségnyújtás és hibabejelentés."
			case "3":
				replyText = "Ön a 3-as gombot választotta: Új szerződéskötés és értékesítés."
			case "#":
				replyText = fmt.Sprintf("PIN vagy azonosító megerősítve: %s. Köszönjük!", currBuf)
			case "*":
				replyText = "Visszalépés a főmenübe."
			default:
				replyText = fmt.Sprintf("A megadott opció [%s] rögzítve.", req.Digit)
			}

			callSessionsMu.Lock()
			if activeCall != nil {
				activeCall.Transcript = append(activeCall.Transcript, "IVR: "+replyText)
			}
			callSessionsMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"digit":  req.Digit,
				"buffer": currBuf,
				"reply":  replyText,
			})
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
		fmt.Printf("🚀 Gobelisk Web Dial Pad elindult!\n")
		fmt.Printf("👉 Elérhető a böngészőből: http://localhost%s\n", addr)
		fmt.Printf("=======================================================\n\n")
		http.ListenAndServe(addr, nil)
	},
}

const dialPadHTML = `<!DOCTYPE html>
<html lang="hu">
<head>
  <meta charset="UTF-8">
  <title>Gobelisk PBX - DTMF Tesztelő</title>
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg: #0f172a;
      --card-bg: #1e293b;
      --accent: #38bdf8;
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
      grid-template-columns: 360px 480px;
      gap: 24px;
      max-width: 900px;
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
    .input-group { display: flex; gap: 8px; margin-bottom: 16px; }
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
    .grid-keypad { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-bottom: 16px; }
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
    .key-btn span { font-size: 0.65rem; font-weight: 400; color: var(--text-muted); margin-top: 2px; }
    .key-btn:active { transform: scale(0.95); background: var(--accent); color: #000; }
    .action-btns { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
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
    .chat-box { display: flex; flex-direction: column; height: 500px; }
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
    .msg { padding: 10px 14px; border-radius: 10px; line-height: 1.4; max-width: 88%; word-wrap: break-word; }
    .msg-system { align-self: center; background: rgba(255,255,255,0.06); color: var(--text-muted); font-size: 0.8rem; }
    .msg-user { align-self: flex-end; background: #2563eb; color: #fff; }
    .msg-ivr { align-self: flex-start; background: #334155; color: #38bdf8; border-left: 3px solid var(--accent); }
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
    .status-item { display: flex; align-items: center; gap: 8px; }
    .status-dot { width: 10px; height: 10px; border-radius: 50%; background: #64748b; }
    .status-dot.active { background: var(--success); }
    @media (max-width: 820px) { .container { grid-template-columns: 1fr; } }
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

  <!-- DTMF Call Log -->
  <div class="card chat-box">
    <h2>☎️ Hívásnapló</h2>

    <div class="transcript-log" id="transcript-log">
      <div class="msg msg-system">Kattints a "Hívás Indítása" gombra, majd használd a billentyűzetet a DTMF tesztekhez.</div>
    </div>

    <div class="status-bar">
      <div class="status-item">
        <div class="status-dot" id="status-indicator"></div>
        <span id="audio-status">Vonal bontva</span>
      </div>
    </div>
  </div>
</div>

<script>
  let isCalling = false;

  function logMsg(role, text) {
    const log = document.getElementById('transcript-log');
    const msg = document.createElement('div');
    msg.className = 'msg msg-' + role;
    msg.textContent = text;
    log.appendChild(msg);
    log.scrollTop = log.scrollHeight;
  }

  async function startCall() {
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
      document.getElementById('audio-status').textContent = 'Vonal él';
      logMsg('system', 'Hívás felépült: ' + to);
    } catch (err) {
      document.getElementById('call-status').textContent = 'Hiba';
      logMsg('system', 'Csatlakozási hiba: ' + err);
    }
  }

  function hangup() {
    fetch('/api/hangup', {method: 'POST'})
      .then(r => r.json())
      .then(() => {
        isCalling = false;
        document.getElementById('call-status').textContent = 'Bontva';
        document.getElementById('dtmf-display').textContent = '---';
        document.getElementById('status-indicator').classList.remove('active');
        document.getElementById('audio-status').textContent = 'Vonal bontva';
        logMsg('system', 'Hívás befejezve.');
      });
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
        logMsg('ivr', data.reply);
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
