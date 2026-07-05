#!/usr/bin/env bash
# Fuse carrier-voice-8b LoRA -> Qwen3-8B, convert to GGUF, quantize Q5_K_M.
# Emits phase markers so progress is greppable. Idempotent-ish: skips finished steps.
set -euo pipefail

BASE_ID="unsloth/Qwen3-8B"
ADAPTER="$HOME/working/carrier-train/adapters-voice-8b"
WORK="$HOME/working/carrier-train/gguf"
BASE_DIR="$WORK/base"
MERGED="$WORK/merged"
OUT="$HOME/working/carrier-train/carrier-voice-8b-gguf"
mkdir -p "$WORK" "$OUT"

echo "== PHASE venv =="
if [ ! -d "$WORK/venv" ]; then
  uv venv --python 3.12 "$WORK/venv"
fi
source "$WORK/venv/bin/activate"
uv pip install -q torch transformers peft accelerate sentencepiece protobuf gguf huggingface_hub

echo "== PHASE download base ($BASE_ID) =="
if [ ! -f "$BASE_DIR/config.json" ]; then
  hf download "$BASE_ID" --local-dir "$BASE_DIR"
fi
[ -f "$BASE_DIR/config.json" ] || { echo "FATAL: base download failed (no config.json)"; exit 1; }

echo "== PHASE merge =="
if [ ! -f "$MERGED/config.json" ]; then
  python "$WORK/merge.py" "$BASE_DIR" "$ADAPTER" "$MERGED"
fi

echo "== PHASE llama.cpp =="
if [ ! -d "$WORK/llama.cpp" ]; then
  git clone --depth 1 https://github.com/ggerganov/llama.cpp "$WORK/llama.cpp"
fi
uv pip install -q -r "$WORK/llama.cpp/requirements.txt" || true

echo "== PHASE convert f16 gguf =="
F16="$OUT/carrier-voice-8b-f16.gguf"
if [ ! -f "$F16" ]; then
  python "$WORK/llama.cpp/convert_hf_to_gguf.py" "$MERGED" --outfile "$F16" --outtype f16
fi

echo "== PHASE quantize Q5_K_M =="
Q="$OUT/carrier-voice-8b-Q5_K_M.gguf"
QUANT="$(command -v llama-quantize || true)"
if [ -z "$QUANT" ]; then
  echo "installing llama.cpp (for llama-quantize)…"
  brew install llama.cpp >/dev/null 2>&1 || true
  QUANT="$(command -v llama-quantize || true)"
fi
if [ -n "$QUANT" ] && [ ! -f "$Q" ]; then
  "$QUANT" "$F16" "$Q" Q5_K_M
fi

echo "== BUILD DONE =="
ls -lah "$OUT"
