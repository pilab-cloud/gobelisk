# CUDA build of the faster-whisper ASR service.
#
# The POC image is CPU-only; faster-whisper on GPU needs the CUDA runtime and
# cuDNN from the NVIDIA base image.
FROM nvidia/cuda:12.4.1-cudnn-runtime-ubuntu22.04

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 python3-pip ffmpeg libcublas-12-4 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY poc/stt-vad-whisper/requirements.txt .
RUN pip3 install --no-cache-dir -r requirements.txt

COPY poc/stt-vad-whisper/asr_service.py .

ENV WHISPER_MODEL=large-v3-turbo
ENV WHISPER_DEVICE=cuda
ENV WHISPER_COMPUTE=float16

EXPOSE 8000
CMD ["uvicorn", "asr_service:app", "--host", "0.0.0.0", "--port", "8000"]
