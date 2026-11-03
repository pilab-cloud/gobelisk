# Local LoRA Fine-Tuning Guide (Hungarian Customer Service + English MCP Tools)

This guide provides instructions for fine-tuning small open-weights models (`Qwen2.5-1.5B-Instruct` or `Qwen2.5-3B-Instruct`) locally on a host machine equipped with an Intel Core i9 CPU and an NVIDIA Quadro T2000 (4 GB VRAM).

---

## 1. Prerequisites & Virtual Environment

Because modern host distributions like Ubuntu 26.04 ship with Python 3.14 (which lacks pre-compiled PyTorch CUDA wheels), we use `uv` to manage a hermetic Python 3.12 environment:

```bash
# 1. Install uv (if not already installed)
curl -LsSf https://astral.sh/uv/install.sh | sh

# 2. Create Python 3.12 virtual environment
uv venv training/.venv --python 3.12

# 3. Install PyTorch with CUDA 12.1 support
UV_HTTP_TIMEOUT=300 uv pip install --python training/.venv/bin/python \
  torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu121

# 4. Install fine-tuning toolchain
UV_HTTP_TIMEOUT=300 uv pip install --python training/.venv/bin/python \
  --extra-index-url https://download.pytorch.org/whl/cu121 \
  "torch==2.5.1+cu121" "transformers==4.49.0" peft bitsandbytes trl accelerate sentencepiece protobuf
```

### Verification:
```bash
training/.venv/bin/python -c "import torch, bitsandbytes; print('CUDA available:', torch.cuda.is_available(), '| GPU:', torch.cuda.get_device_name(0), '| BitsAndBytes:', bitsandbytes.__version__)"
```

---

## 2. Dataset Structure (`training/dataset/`)

The dataset uses standard JSONL format formatted for chat templates, with natural spoken Hungarian dialogues mapped to strict English function definitions:

File: [`training/dataset/sample_hungarian_customer_service.jsonl`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/training/dataset/sample_hungarian_customer_service.jsonl)

```json
{
  "messages": [
    {"role": "system", "content": "Te egy magyar nyelvű telefonos ügyfélszolgálati asszisztens vagy. Az ügyfelekkel udvariasan, tömören és érthetően beszélj magyarul. Amikor művelet végrehajtására van szükség, hívd meg a megfelelő angol nyelvű függvényt a szabványos paraméterekkel."},
    {"role": "user", "content": "Csiszolt gerendát szeretnék Budapestről Zalaegerszegre szállítani kamionnal."},
    {"role": "assistant", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "create_freight_order", "arguments": "{\"cargo_description\": \"csiszolt gerenda\", \"pickup_location\": \"Budapest\", \"delivery_location\": \"Zalaegerszeg\", \"vehicle_type\": \"truck\"}"}}]},
    {"role": "tool", "tool_call_id": "call_1", "content": "{\"status\": \"created\", \"order_id\": \"FO-2026-901\"}"},
    {"role": "assistant", "content": "Rögzítettem a fuvarozási megbízást: csiszolt gerenda szállítása Budapestről Zalaegerszegre kamionnal. A megbízás száma: FO-2026-901."}
  ]
}
```

---

## 3. Running 4-bit QLoRA Training

Script: [`training/scripts/train_lora.py`](file:///home/paalgyula/wspace/projects/pilab/gobelisk/training/scripts/train_lora.py)

### Dry-Run Verification (2 steps):
```bash
training/.venv/bin/python training/scripts/train_lora.py --model Qwen/Qwen2.5-0.5B-Instruct --dry-run
```

### Full Training Run:
```bash
training/.venv/bin/python training/scripts/train_lora.py \
  --model Qwen/Qwen2.5-1.5B-Instruct \
  --dataset training/dataset/sample_hungarian_customer_service.jsonl \
  --output-dir training/output/qwen-hu-lora \
  --max-steps 60 \
  --batch-size 1 \
  --grad-accum 4 \
  --lr 2e-4
```

### Why It Fits Within 4 GB VRAM:
1. **NF4 (Normal Float 4) Quantization:** Base model weights are held in 4-bit representation using `BitsAndBytesConfig`, consuming only ~1.0 GB VRAM for a 1.5B model.
2. **LoRA (Low-Rank Adaptation):** Only low-rank matrices (<1% of parameters) are trained.
3. **Paged AdamW 8-bit:** Prevents optimizer allocation spikes.
4. **Batch Size = 1 with Gradient Accumulation = 4:** Guarantees stable convergence without CUDA OOM errors.

---

## 4. Merging and Exporting to GGUF

Once training concludes, merge the adapter with base weights and convert to GGUF format:

```bash
# 1. Merge LoRA weights into standalone model directory
python3 -c "
from peft import PeftModel
from transformers import AutoModelForCausalLM, AutoTokenizer

base = AutoModelForCausalLM.from_pretrained('Qwen/Qwen2.5-1.5B-Instruct', torch_dtype='auto')
model = PeftModel.from_pretrained(base, 'training/output/qwen-hu-lora')
merged = model.merge_and_unload()
merged.save_pretrained('training/output/qwen-hu-final')
tokenizer = AutoTokenizer.from_pretrained('Qwen/Qwen2.5-1.5B-Instruct')
tokenizer.save_pretrained('training/output/qwen-hu-final')
"

# 2. Convert to GGUF format using llama.cpp
python3 /tmp/llama.cpp/convert_hf_to_gguf.py training/output/qwen-hu-final/ --outfile training/output/qwen-hu-final.gguf
```

### Creating an Ollama Model:
Create a local `Modelfile`:
```dockerfile
FROM ./training/output/qwen-hu-final.gguf
PARAMETER temperature 0.1
SYSTEM "Te egy magyar nyelvű telefonos ügyfélszolgálati asszisztens vagy. Az ügyféllel magyarul beszélj, a külső eszközöket angolul használd."
```

Register the model into Ollama:
```bash
ollama create pbx-hu-agent -f Modelfile
```

The model is now accessible locally via the standard OpenAI-compatible API at `http://localhost:11434`.
