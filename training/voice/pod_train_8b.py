"""carrier voice tier v2 — 8B DENSE QLoRA on FidoNet, RunPod.

Goal: capture the TONE OF VOICE of 1990s BBS callers (style, not facts).
8B dense + 4-bit QLoRA is the right-sized tool; it fits a 24GB card.

STORAGE CONTRACT (the thing that burned us last time):
  Everything durable lives under $VOL — a RunPod NETWORK VOLUME, which
  survives pod termination. Container/ephemeral disk holds NOTHING we care
  about. Checkpoints every SAVE_STEPS -> volume. Auto-resume from the last
  checkpoint if the pod dies. Final adapter -> volume. And a hard guard that
  REFUSES TO RUN if the volume isn't actually mounted, so we cannot repeat
  the ephemeral mistake.

Env:
  BASE_MODEL  default unsloth/Qwen3-8B   (dense 8B; 4-bit QLoRA is correct here)
  VOL         default /workspace          (network-volume mount point)
  DATA        default $VOL/data/train.jsonl
  MAX_STEPS   default 2000
  SAVE_STEPS  default 100                  (checkpoint cadence -> volume)
  BATCH / GA  default 4 / 4                (drop BATCH to 2 if you OOM on 24GB)
  FORCE=1     bypass the volume-mount guard (don't, unless you mean it)
"""
import json, os
import torch
from unsloth import FastLanguageModel
from datasets import Dataset
from trl import SFTTrainer, SFTConfig
from transformers.trainer_utils import get_last_checkpoint

VOL   = os.environ.get("VOL", "/workspace")
MODEL = os.environ.get("BASE_MODEL", "unsloth/Qwen3-8B")
DATA  = os.environ.get("DATA", f"{VOL}/data/train.jsonl")
OUTDIR = os.environ.get("OUTDIR", f"{VOL}/carrier-voice-8b")            # resumable checkpoints
FINAL  = os.environ.get("FINAL",  f"{VOL}/carrier-voice-8b-adapter")    # clean final adapter
MAX_STEPS  = int(os.environ.get("MAX_STEPS", "2000"))
SAVE_STEPS = int(os.environ.get("SAVE_STEPS", "100"))
BATCH = int(os.environ.get("BATCH", "4"))
GA    = int(os.environ.get("GA", "4"))
MAX_SEQ = 1024

# --- storage guard: never silently train onto ephemeral disk again -----------
if not os.environ.get("FORCE"):
    assert os.path.ismount(VOL), (
        f"REFUSING TO RUN: {VOL} is not a mounted network volume. "
        f"Attach a volume (or set FORCE=1 if you truly want ephemeral).")
os.makedirs(OUTDIR, exist_ok=True)
os.makedirs(FINAL, exist_ok=True)
# prove we can actually write to the volume before we spend GPU time
with open(f"{OUTDIR}/.writetest", "w") as f:
    f.write("ok")
print(f">> storage OK: durable dir = {OUTDIR}", flush=True)

print(f">> model={MODEL}  ckpt_every={SAVE_STEPS}  batch={BATCH}x{GA}", flush=True)
model, tok = FastLanguageModel.from_pretrained(
    model_name=MODEL, max_seq_length=MAX_SEQ, load_in_4bit=True, dtype=None)
model = FastLanguageModel.get_peft_model(
    model, r=16, lora_alpha=16, lora_dropout=0.0, bias="none",
    target_modules=["q_proj", "k_proj", "v_proj", "o_proj",
                    "gate_proj", "up_proj", "down_proj"],
    use_gradient_checkpointing="unsloth", random_state=7)

rows = [json.loads(l) for l in open(DATA, encoding="utf-8")]
print(f">> {len(rows)} examples", flush=True)


def fmt(ex):
    try:
        txt = tok.apply_chat_template(ex["messages"], tokenize=False,
                                      add_generation_prompt=False, enable_thinking=False)
    except TypeError:
        txt = tok.apply_chat_template(ex["messages"], tokenize=False,
                                      add_generation_prompt=False)
    return {"text": txt}


ds = Dataset.from_list(rows).map(fmt, remove_columns=["messages"])

cfg = SFTConfig(
    per_device_train_batch_size=BATCH, gradient_accumulation_steps=GA,
    warmup_steps=20, max_steps=MAX_STEPS, learning_rate=2e-4,
    fp16=not torch.cuda.is_bf16_supported(), bf16=torch.cuda.is_bf16_supported(),
    logging_steps=10, optim="adamw_8bit", weight_decay=0.01,
    lr_scheduler_type="linear", seed=7,
    output_dir=OUTDIR,                    # <-- checkpoints land on the VOLUME
    save_steps=SAVE_STEPS, save_total_limit=3,
    report_to="none", dataset_text_field="text",
    max_seq_length=MAX_SEQ, dataset_num_proc=8)
trainer = SFTTrainer(model=model, tokenizer=tok, train_dataset=ds, args=cfg)

# train only on the assistant's reply — better voice, no wasted signal on the prompt
try:
    from unsloth.chat_templates import train_on_responses_only
    trainer = train_on_responses_only(
        trainer, instruction_part="<|im_start|>user\n",
        response_part="<|im_start|>assistant\n")
    print(">> response-only masking on", flush=True)
except Exception as e:
    print(">> masking skipped:", e, flush=True)

# if the pod died mid-run, relaunching this script picks up where it left off
resume = get_last_checkpoint(OUTDIR) if os.path.isdir(OUTDIR) else None
print((">> RESUMING from " + resume) if resume else ">> fresh start", flush=True)
trainer.train(resume_from_checkpoint=resume)

trainer.save_model(FINAL)
tok.save_pretrained(FINAL)
print(">> FINAL adapter saved ->", FINAL, flush=True)

# smoke test so the log shows the voice immediately
FastLanguageModel.for_inference(model)
msgs = [
    {"role": "system", "content": "You are Duke, a caller on a 1990s BBS posting in the Star Wars message echo. Write like a real BBS user of the era."},
    {"role": "user", "content": "You are replying to Ace about \"Re: best lightsaber\".\n\nThey wrote:\nAC> Vader's is clearly the best, red is just cooler than blue\n\nWrite your reply."}]
try:
    ids = tok.apply_chat_template(msgs, add_generation_prompt=True, enable_thinking=False, return_tensors="pt").to("cuda")
except TypeError:
    ids = tok.apply_chat_template(msgs, add_generation_prompt=True, return_tensors="pt").to("cuda")
out = model.generate(input_ids=ids, max_new_tokens=120, temperature=0.8, do_sample=True)
print(">> SAMPLE:\n" + tok.decode(out[0][ids.shape[1]:], skip_special_tokens=True), flush=True)
print(">> DONE", flush=True)
