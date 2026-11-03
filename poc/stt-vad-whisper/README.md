# SIP/WebRTC VAD-alapú Whisper ASR POC (Proof-of-Concept)

Ez a POC az **1. Megközelítést (VAD-alapú Endpointing)** valósítja meg valós idejű telefonos (SIP/RTP) és WebRTC híváskezeléshez Go és Python (faster-whisper) komponensekkel.

---

## 🏗️ Architektúra működése

```
RTP / WebRTC (Opus/G.711 / PCM)
             │
             ▼
   [ Go Audio Buffer & VAD ]
             │
             ├── Csend: Eldobás / nem terheli a GPU-t
             │
             ├── Beszéd észlelve: 20 ms PCM szeletek gyűjtése memóriában
             │
             └── 350-500 ms csend (Beszéd vége / Utterance vége):
                         │
                         ▼ HTTP/gRPC (In-Memory PCM Flush)
          [ Faster-Whisper ASR Microservice ]
                         │
                         ▼ (80 - 250 ms)
               Kész szöveg az IVR-nek / Dialplannak
```

---

## 📁 A POC tartalma

* `main.go`: 
  - **RTP/WebRTC stream szimulátor**: Képes bármilyen audio fájlt (`.wav`, `.mp3`) valós idejű 20 ms-os PCM szeletekben streamelni egy belső VAD pufferbe.
  - **WebSocket / RTP szerver mód**: Éles környezethez WebRTC/WebSocket kliensek audio streamjének fogadására (`ws://host:8088/ws/audio`).
  - **VAD Endpointing motor**: Figyeli a beszéd- és csendidőszakokat, és automatikusan lezárja a mondatot, majd elküldi a Whisper szervernek.
* `asr_service.py`: 
  - FastAPI mikroszerviz `faster-whisper` motorral. A modell folyamatosan a RAM-ban (vagy VRAM-ban) van tartva, így nincs betöltési késleltetés.
* `Dockerfile` & `docker-compose.yml`:
  - Konténerizált Whisper kiszolgáló (CPU vagy GPU / CUDA támogatással).

---

## 🚀 Gyors kipróbálás és tesztelés

### 1. Whisper ASR Backend indítása

#### Opció A: Docker segítségével (ajánlott)
```bash
cd poc/stt-vad-whisper
docker compose up -d
```

#### Opció B: Helyi Python környezetben
```bash
cd poc/stt-vad-whisper
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
python asr_service.py
```

### 2. Valós idejű RTP stream szimulálása és tesztelése

Futtasd le a Go tesztprogramot egy meglévő teszthanggal (akár WAV, MP3 vagy PBX rögzítés):

```bash
cd poc/stt-vad-whisper

# Szimuláció futtatása (a Go program 20ms-os szeletekben adagolja, mintha telefonvonalról jönne):
go run main.go -mode simulate -input /path/to/test_magyar_beszed.wav -silence-ms 400
```

### 3. Valós idejű kimenet példa

```text
--- RTP / WebRTC Stream Szimuláció indul ---
Fájl: test_magyar_beszed.wav | Endpointing csend: 400 ms | VAD küszöb: 500.0

=== [IVR DISPATCH] ===
Beszéd hossza: 2.40 sec
Whisper leirat: "Jó napot kívánok, a számlázási osztályt szeretném kérni."
Feldolgozási idő (ASR Latency): 165 ms
=====================

=== [IVR DISPATCH] ===
Beszéd hossza: 1.80 sec
Whisper leirat: "Az ügyfélszámom 45 82 10."
Feldolgozási idő (ASR Latency): 112 ms
=====================
```

---

## 🎯 Gobelisk PBX Integráció

A `pkg/ivr` vagy `pkg/pbx` rétegben az RTP csomagok fogadásakor a 20 ms-os G.711 / Opus payload dekódolható 16 kHz PCM-re, és átadható ennek az `AudioBuffer`-nek. Amikor a VAD csendet detektál, az IVR state machine azonnal megkapja az értelmezett szöveget a dialplanban meghatározott lépések végrehajtásához.

---

## 🔗 Integráció a `../pir2` rendszerrel

A `pir2` monorepóban a `recorder` szolgáltatás végzi a WebRTC sávok (audio.ogg / video.ivf) rögzítését és a leiratozást:

1. **Beépített HTTP ASR Engine**:
   A `pir2/recorder/internal/transcriber` bővítve lett a `HTTPEngine` támogatással, amely közvetlenül eléri a Faster-Whisper in-memory szervert.
2. **Környezeti változók konfigurálása a recorderben**:
   A `pir2/recorder` indításakor add meg az ASR URL-t:
   ```bash
   export RECORDER_ASR_URL="http://127.0.0.1:8000/transcribe"
   # Vagy docker-compose-ban:
   # - RECORDER_ASR_URL=http://whisper-asr:8000/transcribe
   ```
3. **Működése**:
   Amikor egy meeting lezárul, vagy a `POST /v1/transcriptions` végponton leiratozás kérés érkezik, a `pir2/recorder` háttérmunkása automatikusan konvertálja a rögzített audio sávot és átadja a memóriában ülő Whisper modellnek, elmenti az elkészült feliratot és frissíti a Mongo adatbázisban a státuszt `completed`-re.
