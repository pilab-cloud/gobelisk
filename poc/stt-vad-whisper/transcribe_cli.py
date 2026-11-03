#!/usr/bin/env python3
"""
CLI wrapper a recorder és külső eszközök számára, amely közvetlenül meghívja
a Faster-Whisper in-memory HTTP ASR szervert (vagy fallbackként helyben futtatja).
"""
import sys
import os
import subprocess
import urllib.request
import urllib.error

ASR_URL = os.getenv("WHISPER_ASR_URL", "http://127.0.0.1:8000/transcribe")

def main():
    if len(sys.argv) < 2:
        print("Használat: transcribe_cli.py <audio_file_path>", file=sys.stderr)
        sys.exit(1)

    audio_path = sys.argv[1]
    if not os.path.exists(audio_path):
        print(f"Hiba: A fájl nem található: {audio_path}", file=sys.stderr)
        sys.exit(1)

    # Audio konvertálása 16kHz s16le PCM streamre ffmpeg segítségével
    cmd = [
        "ffmpeg", "-nostdin", "-threads", "1", "-i", audio_path,
        "-f", "s16le", "-ac", "1", "-ar", "16000", "-"
    ]
    try:
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        pcm_bytes, _ = proc.communicate()
    except Exception as e:
        print(f"ffmpeg hiba: {e}", file=sys.stderr)
        sys.exit(1)

    if not pcm_bytes:
        print("", end="")
        sys.exit(0)

    # HTTP POST küldése a memóriában futó Faster-Whisper mikroszerviznek
    req = urllib.request.Request(
        ASR_URL,
        data=pcm_bytes,
        headers={"Content-Type": "application/octet-stream"},
        method="POST"
    )

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            import json
            data = json.loads(resp.read().decode("utf-8"))
            print(data.get("text", ""))
    except urllib.error.URLError as e:
        print(f"ASR HTTP hiba ({ASR_URL}): {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
