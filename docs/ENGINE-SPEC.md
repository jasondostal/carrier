# carrier — Simulation Engine Spec

> Status: design, not built. Captured 2026-07-05 from the "is an LLM even the
> right tool for BBS ops?" conversation. This is the plan for the work *after*
> the voice-model fine-tune lands. Read `README.md` first for the world premise.

## The insight that drives this

carrier currently uses an LLM for **two different jobs in one call**: deciding
*what a caller does* this turn (intent) **and** writing *the words* they say
(content). Those are not the same kind of problem.

- **Content** (a flame post, secret mail, a LORD taunt) is a language task. An
  LLM — specifically our fine-tuned BBS-voice model — is exactly right.
- **Ops** (who dials in when, busy signals, time budgets, menu navigation, door
  mechanics, whose turn it is) is **not a language task at all.** It's a
  *simulation*. And for a world **we own**, we already have the state machine —
  so making a neural net approximate rules we know exactly is a sledgehammer
  doing arithmetic: slower, non-deterministic, costs tokens, and it can
  hallucinate an illegal move a game engine simply cannot represent.

So: **BBS ops in carrier's own world is a game engine. The LLM shrinks to voice.**

### The one distinction that keeps this from being a mistake

This applies to **carrier-the-simulation** (the owned ant farm). It does **not**
apply to **carrier-the-dialer** — the endgame where carrier drives a *foreign*
board (a real ENiGMA½, someone's TriBBS) that we did **not** write. There we
can't hardcode a menu tree we've never seen, so reading unknown screens and
emitting keystrokes is a genuine perception→action problem — and *that* is where
the fine-tuned **keystroke/action model** (`carrier-caller-4b`) belongs. It was
never meant to orchestrate our own world; it's the brain for boards we don't own.

| | owned world (this sim) | foreign board (the dialer) |
|---|---|---|
| ops / navigation | **game engine** (deterministic) | keystroke action model (LLM) |
| content / voice | fine-tuned voice MoE | fine-tuned voice MoE |

Neither trained model is wasted. This reframe just sorts them into the right jobs.

## What already exists (carrier is ~60% a game engine)

Grounding in `internal/orchestrator/orchestrator.go`:

- **World clock** — `Run()` ticks the world; `dayBoundary()` handles the Daily News.
- **Login scheduler + contention** — `admit()` fills free phone lines from offline
  callers weighted by `CallUrge`; `freeNode()` enforces the node cap. **Busy
  signals and "who's online together" already emerge here** — this is the engine,
  already built.
- **Session lifecycle** — `admit()` assigns `SessionStart`/`SessionLen` (2–5
  ticks); `turns()` drops a caller when the budget is spent. Line-cycling works.
- **Action application** — `apply()` mutates world state for post/reply/mail/
  door/logoff and notifies the host adapter.
- **Door mechanics** — `playLORD()` + `internal/domain/lord.go` already resolve a
  LORD turn (forest/inn/shop/attack) as *rules*, not LLM freetext.

The gap is exactly **one seam**: `turns()` calls the LLM (`agent.Prompt` → `LLM.Chat`
→ `agent.Parse`) to get a full `Action` — mixing the *intent decision* with the
*content*. That's the thing to split.

## Target architecture — three layers

```
┌─────────────────────────────────────────────────────────────┐
│  ENGINE (deterministic Go)                                   │
│  world clock · login scheduler (daily desire + time budget) ·│
│  line contention/busy signals · session lifecycle ·          │
│  door games (LORD…) as rules · message base · relationships  │
└───────────────┬─────────────────────────────────────────────┘
                │ "caller P is online, it's their turn, here's world state"
                ▼
┌─────────────────────────────────────────────────────────────┐
│  INTENT (light, persona-weighted — NOT a reasoning LLM call) │
│  utility function over action types, weighted by persona     │
│  traits + context + mood. Picks: post / reply / read / door /│
│  mail / lurk / logoff. Cheap, deterministic-ish, debuggable. │
└───────────────┬─────────────────────────────────────────────┘
                │ only when intent needs words:
                ▼
┌─────────────────────────────────────────────────────────────┐
│  VOICE (the fine-tuned BBS MoE)                              │
│  generate the actual message body / mail / taunt in-persona. │
│  The ONLY place the big model fires.                         │
└─────────────────────────────────────────────────────────────┘
```

**The load-bearing principle:** the ant farm's emergent drama comes from *simple
rules colliding with generated content* — **not** from LLM-driven navigation. No
viewer perceives *how* a caller chose the file menu; they perceive the savage
ratio callout he posts. So moving ops to the engine loses nothing visible, as
long as **content stays generative and you never script the flame wars.** Move
ops to the engine; keep drama in the model. Emergence intact.

## Engine subsystems — spec

### 1. World clock / tick
Exists. Keep. A tick is one scheduling quantum. Ticks/day drives the news cycle
and the daily budget reset (below).

### 2. Login scheduler (daily desire + time budget)
Formalize what `admit()` gestures at. Each persona gets, per simulated day:
- **login desire** — probability/eagerness to dial at all today (persona trait;
  `CallUrge` is the seed of this). NightOwl spikes at night; kitkat_16 dodges
  homework after school.
- **time budget** — allotted minutes/ticks for the day (a caller with budget left
  who *wants* on competes for a line; budget exhausted → done until tomorrow).
- **call schedule** — optional per-persona active windows (a "prime time").

Contention against the node cap turns "everyone wants on at 9pm" into **busy
signals** for free. Reset budgets at `dayBoundary()`.

### 3. Line contention / busy signals
Exists via `freeNode()` + node cap. Formalize the *failure* case: a caller who
wants on but finds no free line gets a **busy signal** (a first-class event the
host/TUI can show — "NO CARRIER / line busy"), maybe retries later, maybe gives
up. This is texture the viewer sees and it's pure engine.

### 4. Session lifecycle
Exists (`SessionLen`). Extend: session length should derive from *remaining time
budget* and persona (a leech pops in for one file and leaves; a lonely caller
lingers). Logoff can be engine-driven (budget spent) or intent-driven ("nothing
happening, bail").

### 5. Intent selection — the crux (replaces the per-turn reasoning LLM call)
Per online caller per turn, pick a high-level action **without** an LLM, via a
**utility function**:

```
score(action) = base_weight[persona][action]      // warez_wolf→files, CrustyRon→drama,
                                                   // Seraphine→mail/romance, l1ttl3→door+drama
              + context_bonus(action, world)       // unread mail → boost read-mail;
                                                   // I got flamed → boost reply;
                                                   // forest fights left → boost door;
                                                   // board has fresh drama → boost read/reply
              - repetition_penalty(action, me)     // already posted 3x → suppress post
              + mood(me)                            // grudge, crush, win-streak, boredom
pick = weighted_sample(softmax(scores))
```

Outputs one of: `post · reply(target) · read · door(move) · mail(target) · lurk ·
logoff`. This gives **characterful sequences of choices** (this one lurks in
files, that one starts fights) that are debuggable, tunable, free, and can't emit
an illegal move. Persona weights live in `persona.yaml` (extend the schema).

Optional flavor: a **cheap, occasional** LLM "session mood" call ("warez_wolf is
on the warpath about ratios today") that biases the weights for a session — but
the mechanical selection stays a utility function. Do NOT put a reasoning call in
the per-turn hot path.

### 6. Door engine (LORD and friends)
Exists for LORD (`lord.go` + `playLORD`). Generalize: a door is a **rules engine**
with state per player. The persona's *choices inside a door* (attack vs. flee, who
to PvP-ambush) are the intent utility again — weighted by traits — **not** an LLM.
The LLM only writes the *trash talk* around a PvP hit, if we want flavor. Spec a
small `Door` interface so LORD, and later others, plug in the same way.

### 7. Message base · mail · relationships · memory
Exists (`World`, `memory.Store`, `relationships.yaml`). Unchanged in role. These
are engine state the intent layer reads (unread mail, who flamed me, who I owe)
and the voice layer conditions on.

## Where the models plug in (after this refactor)

- **Voice MoE** (fine-tuning now): called *only* by the voice layer, only when
  intent = post/reply/mail/taunt. Prompt = persona card + thread context +
  chosen intent → the words. This is where *all* LLM value concentrates — which
  is why this refactor **vindicates** the voice fine-tune rather than threatening
  it.
- **Keystroke action model** (`carrier-caller-4b`): NOT used in the sim. It moves
  to the **dialer** adapter for foreign boards, where reading unknown screens is a
  real problem. Keep it; it just has a different home.

## Migration path (incremental, reversible — per house rules)

Never big-bang. Each step independently shippable, old path stays until the new
one proves out:

1. **Extend `persona.yaml`** with intent weights (backward-compatible; default to
   current behavior). No code path change yet.
2. **Add an `Intent` selector** alongside `agent.Prompt`. Behind a flag, `turns()`
   calls the utility selector for the *action*, and only calls the LLM for the
   *content* of post/reply/mail. Keep the old single-call path as the default
   until parity is shown.
3. **Formalize the scheduler**: daily desire + time budget + busy-signal event.
   Reset at `dayBoundary()`.
4. **Generalize the door layer** behind a `Door` interface; port LORD onto it.
5. **Flip the default** to engine-intent + voice-content once it reads as good as
   (or better than) the LLM-per-turn version. Archive the old path, don't delete.
6. **Later, separate track:** the dialer adapter using the keystroke model on a
   real foreign board. That's a different milestone (the "NPCs for any BBS" north
   star), not part of this refactor.

## Open questions to settle when we start

- How much does the **cheap session-mood LLM nudge** actually buy vs. pure utility
  weights? Try pure-utility first; add the nudge only if sequences feel flat.
- Does intent need **cross-turn memory of its own plan** (a session goal), or is
  per-turn utility + world context enough? (Start stateless; add a session goal if
  callers feel goldfish-brained.)
- **Tuning surface:** where do the base weights live and how do we tune them
  without recompiling? (persona.yaml + hot-reload, probably.)
- Keep the **abstract sim** (structs) and the **real-telnet** path (tresbbs) both
  driven by this same engine? The host-port seam already allows it; confirm the
  intent/voice split sits above the seam so both adapters share it.

## One-line summary

carrier already has the engine skeleton; the work is to **stop asking an LLM to
navigate a world we own** — replace the per-turn reasoning call with a
persona-weighted utility selector, and let the fine-tuned voice model do the one
thing only it can: *talk*.
