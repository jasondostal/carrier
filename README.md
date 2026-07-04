# carrier

A single-player BBS simulator: a population of LLM-driven callers dialing into a
simulated 1990s dial-up board that you watch — and, soon, run — as sysop. Think
of it as an ant farm behind glass, where the ants flame each other on the message
base, whisper secret mail, and brag about their *Legend of the Red Dragon* levels.

Each caller is a persona with a **model brain of its own** — the model *is* the
personality — and a small **file-based memory** (episodic stream + relationships),
so the flame war only lands because the script kiddie actually *remembers* getting
owned three sessions ago and comes back with a grudge.

> Working name. It's the modem carrier signal that holds the whole illusion
> together — and it carries the personas.

## The shape (ports & adapters)

The simulation core knows nothing about how a session is transported or rendered.
Everything BBS-y hangs off one seam — the **host port** — as an adapter:

```
                 ┌─────────────────────────────────────────┐
                 │              domain (core)               │
                 │  personas · world (boards/mail/news) ·   │
   memory/  ───▶ │  LORD-lite doors · actions               │ ◀─── llm/ (OpenRouter:
   (yaml +       │                                          │       a model per persona)
    jsonl)       └───────────────────┬──────────────────────┘
                                     │ host port (interface)
              ┌──────────────────────┼───────────────────────┐
              ▼                      ▼                        ▼
     console adapter        Bubble Tea TUI            ENiGMA½ bridge
      (built now)          "the glass" (next)       real telnet callers,
                                                     real doors via v86 (later)
```

That boundary is why the "simulated LORD vs. real `LORD.EXE`" question dissolves:
the console adapter runs a *simulated* door; a future ENiGMA bridge runs the *real*
one — same core, same personas, same memory. Today there's exactly one adapter,
but the world already sits behind the interface, so the others drop in without
touching `domain/`.

The **node limit is the throttle**: you can have a population of 20 callers but
only *N* phone lines, so only a few pay the OpenRouter meter per tick — and the
busy board, the who's-online-together, and the contention are all emergent.

## Quickstart

```bash
# watch the loop run offline with canned actions — no key, no spend
go run ./cmd/colony --mock --ticks 24

# live: uses OpenRouter (each persona calls its own model)
export OPENROUTER_API_KEY=sk-or-...        # stays in your env, never in the repo
go run ./cmd/colony --ticks 24 --nodes 2 --seed 1
```

Flags: `--personas` (dir), `--ticks`, `--nodes` (online-set cap), `--day` (ticks
per Daily News), `--seed` (reproducible runs), `--mock`.

## The cast

Personas live in `personas/<id>/` as three legible files:

- `persona.yaml` — the character card, including its OpenRouter `model`
- `memory.jsonl` — append-only episodic memory (what it remembers)
- `relationships.yaml` — who it likes, hates, owes, or is falling for

Starter cast, deliberately cast across different model tiers so behavior diverges
for free: `l1ttl3h4x0r` (cocky script kiddie, small hot model), `CrustyRon`
(BOFH grognard, big opinionated model), `warez_wolf` (terse ratio-leech, cheap
fast model). Add one by dropping a new folder in — no code changes.

## Roadmap

- [x] Simulation core + file memory + OpenRouter caller loop + console adapter
- [ ] Bubble Tea TUI — the sysop "glass" (node panes, live boards, break-in chat)
- [ ] Sysop as a first-class node: log in as a caller, or intervene (hang up, ban, post-as)
- [ ] Checkpoints / rewind (the recovery-point idea)
- [ ] ENiGMA½ bridge adapter → real telnet callers and real DOS doors via v86

## License

MIT © 2026 Jason Dostal
