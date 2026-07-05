# carrier voice model — training walkthrough

This is the **voice tier** for carrier: a small model fine-tuned to write message
posts and mail that sound like a *real 1990s BBS caller*, not a modern LLM doing
"mood poetry." It captures **tone of voice**, not facts. It is deliberately one
narrow job (see [`docs/ENGINE-SPEC.md`](../../docs/ENGINE-SPEC.md)): the game
engine runs the board; the intent layer decides *what* a persona wants; this model
only decides *how it sounds when it talks*.

> First model Jason trained end-to-end. This doc is written so you can re-run it,
> understand every choice, and never lose the artifacts again.

---

## TL;DR

| | |
|---|---|
| **Base** | `unsloth/Qwen3-8B` (dense), 4-bit QLoRA |
| **Data** | FidoNet Voice Dataset — 57,000 real BBS replies, response-masked |
| **Hardware** | RunPod RTX 4090 (24GB), Secure Cloud, EU-RO-1 |
| **Storage** | **network volume** `carrier-voice` (50GB) — checkpoints survive pod death |
| **Recipe** | LoRA r=16, α=16, lr=2e-4, batch 4 × grad-accum 4 (eff 16) |
| **Result** | plateaued ~step 1000 → **cut early** at step 1069, kept **checkpoint-900** (train-loss floor ~2.46). See [`samples.md`](samples.md) + [`loss.csv`](loss.csv). |
| **Cost** | $0.69/hr × ~1h11m ≈ **$0.77 total** |
| **Artifacts** | adapter on the volume → pulled to `~/working/carrier-train/adapters-voice-8b/` |

**How it turned out:** 5 of 7 personas came out clearly in-voice and *distinct*
(kitkat_16 the teen girl reads nothing like CrustyRon the BOFH sysop); the model
even learned the FidoNet `On <date>, X wrote to Y` quote headers straight from the
data. Two warts — one repetition loop, one flat persona — both fixable at inference
time (repetition penalty), not a retrain. Full breakdown in [`samples.md`](samples.md).

**Prior art (why this dataset is worth having):** the raw material for old-internet
voice is common — Usenet corpora (Wesbury Lab, 20 Newsgroups), Reddit dumps, Stack
Exchange — but a *packaged, persona-conditioned BBS/FidoNet voice SFT set* did not
appear to exist on HF or Kaggle when this was built. The FidoNet messages were
preserved by hobbyist archivists (breakintochat.com / archive.org), never turned
into a fine-tuning set. That gap is the point: nobody cooks this dish because it
only tastes good to a project that needs period-authentic BBS voice.

---

## Why these choices (the lessons behind them)

**Why 8B dense, not the 35B MoE we tried first.**
The goal is *style transfer*, which is the one thing supervised fine-tuning is
genuinely good at — and style does not need a giant model. An 8B dense model fits
a cheap 24GB card, trains in ~90 minutes, and can't strand you on cost. The first
attempt reached for a 35B mixture-of-experts model; that was cool-factor scope
creep and it cost real money for nothing. **Match the model size to the job.**

**Why QLoRA (4-bit) is correct here.**
For a *dense* model, 4-bit QLoRA is the right, cheap default. (It is the *wrong*
default for an MoE — there, bitsandbytes silently falls back to a ~12× slower
per-expert loop, and the fix is bf16 + `UNSLOTH_MOE_BACKEND`. That trap is why the
35B run was slow and expensive. Not relevant to this 8B run, but worth knowing.)

**Why a network volume, non-negotiable.**
The previous run trained onto a pod's **ephemeral disk** and the adapter vanished
when the pod terminated — $10 for nothing. Everything here writes to a **network
volume** mounted at `/workspace`, which persists independently of any pod.
`pod_train_8b.py` has a hard guard that *refuses to start* if `/workspace` isn't a
real mounted volume, so that mistake cannot repeat.

**Why response-only masking.**
We train on the assistant's reply text only, not the prompt — so the model learns
to *produce* BBS voice, not to memorize personas we feed it at inference.

---

## The files here

| File | What it is |
|---|---|
| `pod_train_8b.py` | The trainer. 8B QLoRA, checkpoints every 100 steps to the volume, auto-resumes from the last checkpoint if the pod dies, saves the final adapter, runs a smoke sample. |
| `monitor_loss.py` | Read-only loss watcher. Writes `loss.csv` for the curve and raises a `PLATEAU_DETECTED` flag when the loss flattens — it never kills training (the operator makes the cut). |
| `test_personas_8b.py` | The real evaluation: does `warez_wolf` sound different from `kitkat_16`? Seven personas × varied threads. |
| `loss.csv` | The loss curve from the run (added after completion). |
| `samples.md` | Persona-battery outputs (added after completion). |

---

## How to reproduce (RunPod, from scratch)

```bash
# 0. one network volume in a storage-capable secure DC (EU-RO-1 has 4090 stock)
#    -> RunPod console → Storage → New Network Volume (50GB, EU-RO-1)

# 1. a 4090 pod in the SAME datacenter, with that volume attached (mounts /workspace)
#    image: runpod/pytorch:2.4.0-py3.11-cuda12.4.1-devel-ubuntu22.04
#    expose 22/tcp

# 2. push data + scripts to the volume (from your Mac)
scp -P <port> pod_train_8b.py monitor_loss.py root@<ip>:/workspace/
scp -P <port> train.jsonl root@<ip>:/workspace/data/train.jsonl   # 57k FidoNet voice set

# 3. install unsloth (a clean runpod pytorch image = matched CUDA, no breakage)
ssh ... 'pip install --no-cache-dir unsloth unsloth_zoo'

# 4. train (detached, logs + checkpoints on the volume)
ssh ... 'cd /workspace && setsid bash -c "MAX_STEPS=2000 SAVE_STEPS=100 python -u pod_train_8b.py" \
          </dev/null >/workspace/train.log 2>&1 &'
ssh ... 'cd /workspace && setsid python -u monitor_loss.py </dev/null >/workspace/monitor.log 2>&1 &'

# 5. when done: pull the adapter down, then DELETE THE POD so it stops billing
scp -r -P <port> root@<ip>:/workspace/carrier-voice-8b-adapter ./adapters-voice-8b
```

The **57k voice `train.jsonl`** is built from the FidoNet scrape by the harness in
`~/working/corpora/fidonet` and published as Kaggle dataset
`jdostal/bbs-voice-fidonet-train`. Each row is
`system = persona+echo / user = thread context / assistant = the real reply body`.

## Reading the loss curve

Healthy shape: a fast drop in the first ~50–100 steps (the model finding the
register), then a long gentle decline that flattens. When the last-200-step
average stops improving by more than ~0.5%, more steps mostly waste money — that's
the cut point. `monitor_loss.py` flags it; grab the latest checkpoint.

## Using the adapter

```python
from unsloth import FastLanguageModel
from peft import PeftModel
model, tok = FastLanguageModel.from_pretrained("unsloth/Qwen3-8B", load_in_4bit=True)
model = PeftModel.from_pretrained(model, "path/to/carrier-voice-8b-adapter")
FastLanguageModel.for_inference(model)
# system = persona card, user = thread context → assistant = in-voice reply
```

## If the pod dies mid-run

Just relaunch step 4. `pod_train_8b.py` calls `get_last_checkpoint()` on the volume
and resumes. Nothing before the last 100-step checkpoint is lost.

---

## Serving the voice model (LM Studio, GGUF)

`serving/build.sh` fuses the LoRA into full-precision Qwen3-8B and produces a
GGUF LM Studio can serve:

```bash
serving/build.sh          # merge → GGUF f16 → quantize Q5_K_M (~5.4GB)
# output: carrier-train/carrier-voice-8b-gguf/carrier-voice-8b-Q5_K_M.gguf
```

Then in LM Studio: drop the `.gguf` under `~/.lmstudio/models/carrier/carrier-voice-8b/`,
**load** it, and make sure the **local server is running** (`:1234`). Point carrier at it:

```bash
go run ./cmd/colony --intent engine --voice-model lmstudio:carrier-voice-8b
# other host/box? CARRIER_LMSTUDIO_BASE_URL=http://192.168.1.57:1234/v1 go run …
```

## Engine integration (the model's job in carrier)

carrier uses this model as its **content layer** only (see the engine/voice split
in `internal/intent` + `internal/voice`): the game engine decides *what* each
caller does from persona utility weights, and this model writes the *words* in
the exact prompt shape it was trained on. It also voices in-character brags for
notable Legend of the Red Dragon events. Run offline with a canned mock voice by
omitting `--voice-model`.

## The four traps that cost us money (so you don't repeat them)

1. **Ephemeral disk** — the pod's container disk is wiped on termination. Durable
   work goes on a **network volume**. (Cost of learning this: $10 + one lost model.)
2. **QLoRA on an MoE** — silently ~12× slower; MoE needs bf16 + a big card.
3. **Kaggle free tier** — hands you an ancient GPU (P100, sm_60) the modern
   PyTorch build doesn't support; Unsloth dies at load. Fine for nothing serious.
4. **Wall-clock ≠ compute** — you're billed from pod boot, including the ~5–15 min
   of model download + install + kernel compile, not just the training minutes.
   Budget accordingly.
