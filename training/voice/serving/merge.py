"""Merge the carrier voice LoRA into full-precision Qwen3-8B for GGUF export.

The adapter was trained against the 4-bit base, but its LoRA deltas apply to the
same layers in the bf16/fp16 base — so we merge onto that and get a clean, full
model llama.cpp can convert.
"""
import sys
import torch
from transformers import AutoModelForCausalLM, AutoTokenizer
from peft import PeftModel

base, adapter, out = sys.argv[1], sys.argv[2], sys.argv[3]
print(f">> loading base {base}", flush=True)
tok = AutoTokenizer.from_pretrained(base)
model = AutoModelForCausalLM.from_pretrained(base, torch_dtype=torch.float16, device_map="cpu")
print(f">> applying adapter {adapter}", flush=True)
model = PeftModel.from_pretrained(model, adapter)
print(">> merge_and_unload", flush=True)
model = model.merge_and_unload()
print(f">> saving merged -> {out}", flush=True)
model.save_pretrained(out, safe_serialization=True)
tok.save_pretrained(out)
print(">> MERGE DONE", flush=True)
