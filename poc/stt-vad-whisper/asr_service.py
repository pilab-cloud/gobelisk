"""
FastAPI + Faster-Whisper in-memory ASR Service
Optimalizálva SIP/RTP és WebRTC VAD endpointed audio chunkokhoz.
"""
import io
import os
import time
import numpy as np
from fastapi import FastAPI, Request, Response
from faster_whisper import WhisperModel
import uvicorn

app = FastAPI(title="Faster-Whisper In-Memory ASR Service")

# Modell inicializálás memóriában (large-v3-turbo vagy small/medium igény szerint)
# CPU-n int8, GPU-n float16
MODEL_SIZE = os.getenv("WHISPER_MODEL", "small") # pl: tiny, base, small, medium, large-v3-turbo
DEVICE = os.getenv("WHISPER_DEVICE", "cpu")      # "cpu" vagy "cuda"
COMPUTE_TYPE = os.getenv("WHISPER_COMPUTE", "int8" if DEVICE == "cpu" else "float16")

print(f"Loading Whisper model: {MODEL_SIZE} on {DEVICE} ({COMPUTE_TYPE})...")
model = WhisperModel(MODEL_SIZE, device=DEVICE, compute_type=COMPUTE_TYPE)
print("Model loaded successfully into memory.")

@app.post("/transcribe")
async def transcribe(request: Request):
    # Nyers 16kHz 16-bit mono PCM érkezik a Go VAD endpoint-tól
    pcm_bytes = await request.body()
    if not pcm_bytes:
        return {"text": "", "duration": 0.0, "latency_ms": 0}

    t0 = time.perf_counter()
    
    # PCM s16le konvertálása float32-be a Whisper számára (-1.0 .. 1.0)
    audio_data = np.frombuffer(pcm_bytes, dtype=np.int16).astype(np.float32) / 32768.0
    duration_sec = len(audio_data) / 16000.0

    # Whisper transzkripció magyar nyelvre
    segments, info = model.transcribe(
        audio_data,
        language="hu",
        beam_size=3,
        vad_filter=False, # Mivel a Go réteg már VAD-olt szakaszt küld
        temperature=0.0
    )

    full_text = " ".join([seg.text.strip() for seg in segments]).strip()
    latency_ms = int((time.perf_counter() - t0) * 1000)

    print(f"[{time.strftime('%X')}] Transcribed {duration_sec:.2f}s audio in {latency_ms}ms: \"{full_text}\"")

    return {
        "text": full_text,
        "language": info.language,
        "duration_sec": round(duration_sec, 2),
        "latency_ms": latency_ms
    }

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8000)
