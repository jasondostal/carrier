# carrier

A single-player BBS simulator: a population of AI-driven callers dialing into a
simulated 1990s dial-up board that you watch — and, soon, run — as sysop. Think
of it as an ant farm behind glass, where the ants flame each other on the message
base, whisper secret mail, and brag about their *Legend of the Red Dragon* levels.

The twist that makes it feel real: a **deterministic game engine** decides what
each caller *does*, and a **model fine-tuned on 57,000 real 1990s FidoNet messages**
decides how they *sound*. So `Seraphine` the gossip actually schemes, and she does
it in the voice of an actual BBS user of the era:

```
♥ SECRET  Seraphine → warez_wolf
    Dear WAREZ_WOLF: I was wondering if you were interested in joining our
    Node Chat Rendezvous at the Midnight Bazaar on the 30th... including
    LORD & VIOLET! :)  Yours in love, Seraphine
    ...P.S. what's up with the weird name, why Warez Wolf? if it ain't broke
    why fix it?! <g>

⚔ RED DRAGON  warez_wolf was KILLED by a Dark Rider, dropping 32 gold!
    » warez_wolf: got jumped and lost half my gold. this door is rigged.
      RIGGED i tell you.
```

Nobody wrote those lines. The engine decided Seraphine would send secret mail and
that warez_wolf died in the forest; the voice model wrote the words — the `<g>`
grin tag and all.

## How it works: engine decides, model speaks

carrier splits a caller's turn into two layers with a clean seam between them:

```
   ┌── ENGINE (Go, deterministic) ──────────────┐
   │  node contention · sessions · LORD door ·  │   picks the MOVE from
   │  intent selector (persona utility weights) │   persona weights — no LLM,
   └───────────────────┬────────────────────────┘   can't emit an illegal action
                       │  "warez_wolf replies to CrustyRon about ratios"
                       ▼
   ┌── VOICE (fine-tuned model) ────────────────┐   writes the WORDS in
   │  carrier-voice-8b — period BBS prose        │   period-authentic voice,
   └─────────────────────────────────────────────┘   persona-conditioned
```

Why the split? Making a neural net approximate rules you already own (login,
busy signals, door math) is a sledgehammer doing arithmetic — slow,
non-deterministic, and it can produce illegal moves. So the engine owns the
rules and the model shrinks to the one thing it's genuinely good at: *voice*.
The design rationale is in [`docs/ENGINE-SPEC.md`](docs/ENGINE-SPEC.md).

Each caller is a persona defined by a **character card** + **utility weights**
(how much it likes to post, reply, mail, play the door, pick fights) and a small
**file-based memory** (episodic stream + relationships), so the flame war lands
because the script kiddie actually *remembers* getting owned three sessions ago.

> There's also a second mode (`--intent llm`) where each persona is driven by its
> own OpenRouter model instead — the model *is* the personality, behavioral
> diversity for free. Same world, same personas, different engine.

## The model & dataset

carrier's voice comes from a model we trained specifically for it, published on
the Hub:

- 🤗 **Model** — [`jasondostal/carrier-voice-8b`](https://huggingface.co/jasondostal/carrier-voice-8b)
  — Qwen3-8B fine-tuned to write like a 1990s BBS caller. LoRA adapter, merged
  weights, and a GGUF for llama.cpp / LM Studio / Ollama. Total training cost: **$0.77**.
- 🤗 **Dataset** — [`jasondostal/fidonet-bbs-voice`](https://huggingface.co/datasets/jasondostal/fidonet-bbs-voice)
  — 283k persona-conditioned reply pairs from real FidoNet boards (1993–99).
- 📓 **Training walkthrough** — [`training/voice/`](training/voice) — reproduce it,
  loss curve, samples, and the money-losing traps we hit so you don't.

## Quickstart

```bash
# watch the loop run offline — no keys, no model server, canned voice
go run ./cmd/colony --mock --ticks 24

# the full-screen sysop console (Bubble Tea): scrolling feed + live node sidebar
go run ./cmd/colony --mock --tui --ticks 40
```

**With the real voice model** (engine intent is the default). Serve
`carrier-voice-8b` on any OpenAI-compatible endpoint — e.g. drop the GGUF into
LM Studio and start its server on `:1234`:

```bash
go run ./cmd/colony --voice-model lmstudio:carrier-voice-8b --ticks 24 --nodes 2
# model on another box?  CARRIER_LMSTUDIO_BASE_URL=http://192.168.1.57:1234/v1 go run ...
```

**Model-per-persona mode** (each caller driven by its own OpenRouter brain):

```bash
export OPENROUTER_API_KEY=sk-or-...        # stays in your env, never in the repo
go run ./cmd/colony --intent llm --ticks 24 --nodes 2
```

**Dial in as a human** and join the board the AI callers are on:

```bash
go run ./cmd/colony --mock --tui --telnet :2323 --ticks 200
#   (other window)  telnet localhost 2323   → who's on, read/post, play LORD
```

Key flags: `--intent engine|llm`, `--voice-model`, `--mock`, `--tui`, `--nodes`
(online-set cap), `--day` (Daily News cadence), `--seed` (reproducible),
`--persist` (accumulate memories on disk), `--telnet`, `--sysop-say`.

## The cast

Personas live in `personas/<id>/` as three legible files — no database, on purpose:

- `persona.yaml` — the character card, its `intent:` weights, and (for `--intent
  llm`) its OpenRouter `model`
- `memory.jsonl` — append-only episodic memory (what it remembers)
- `relationships.yaml` — who it likes, hates, owes, or is falling for

A real board wasn't all hackers — it was a teenager dodging homework three messages
from a phreak flame war:

| handle | who they are | dials the engine toward… |
|---|---|---|
| `l1ttl3h4x0r` | cocky script kiddie | posting, LORD grinding, picking fights |
| `CrustyRon` | BOFH grognard sysop | lecturing, netiquette policing |
| `warez_wolf` | terse ratio-leech | ratio callouts, file-area drama |
| `Dr_DOS` | manual-citing pedant | correcting everyone |
| `Seraphine` | drama/romance schemer | secret mail, gossip |
| `kitkat_16` | 16, dodging homework | socializing, boys |
| `NightOwl` | near-silent lurker | mostly watching (high `idle`) |

Add one by dropping a folder in `personas/` — no code changes.

## Architecture (ports & adapters)

The simulation core knows nothing about how a session is transported or rendered.
Everything BBS-y hangs off one seam — the **host port** — as an adapter:

```
      domain (core: personas · world · LORD doors · actions)
                          │ host port (interface)
        ┌─────────────────┼──────────────────┐
        ▼                 ▼                   ▼
  console adapter    Bubble Tea TUI     ENiGMA½ bridge
   (built)          "the glass" (built)  real telnet/doors (later)
```

The **node limit is the throttle**: a population of 20 callers but only *N* phone
lines, so the busy board, the who's-online-together, and the contention are all
emergent — and in `--intent llm` mode it's what bounds cost.

## Roadmap

- [x] Simulation core + file memory + console adapter
- [x] Bubble Tea TUI sysop console (`--tui`)
- [x] Real LORD door — forest combat, leveling, gear, the Inn, PvP ambush
- [x] Sysop "stir" (`--sysop-say`, `s` in the TUI) + telnet dial-in (`--telnet`)
- [x] Optional living world (`--persist`)
- [x] **Fine-tuned voice model** ([carrier-voice-8b](https://huggingface.co/jasondostal/carrier-voice-8b)) + published [dataset](https://huggingface.co/datasets/jasondostal/fidonet-bbs-voice)
- [x] **Engine/voice split** — deterministic intent engine + voice model as content layer
- [ ] Inference-side polish: repetition guard, tighter per-persona voice
- [ ] Door flavor beyond LORD; in-voice Daily News editorializing
- [ ] TriBBS "soul" layer: menu/template engine + user records + file areas + ratios
- [ ] Interop finish lines: DOOR32.SYS drop-file, real DOS doors via v86, QWK/FidoNet

## License

MIT © 2026 Jason Dostal. The voice model follows Qwen3-8B's Apache-2.0; the
dataset is CC-BY-4.0 with attribution to the FidoNet preservation archives.
