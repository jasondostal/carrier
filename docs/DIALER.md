# carrier dialer — host-agnostic BBS traffic simulator

The dialer is carrier's **outbound** side. Where `colony` simulates callers inside
carrier's own world, the dialer drives **real telnet sessions against an external
BBS** — a pool of persona callers that dial in, hit busy signals and redial,
register or log in, read the board, and respond. It's a mock caller load for
testing a real board (tresbbs, or any host).

```
go run ./cmd/dialer --host localhost:2323 \
    --voice-model lmstudio:carrier-voice-moe@q8_0 \
    --callers 8 --duration 2m
```

## What it does

- **Dials in** over telnet, negotiating character-at-a-time mode like a real client.
- **Handles busy**: if the board is full ("All nodes busy" / connection dropped),
  the caller redials after a jittered backoff — the retry-on-busy behavior at the
  core of the simulator. A refused connect (host down) is reported as an error.
- **Registers or logs in**: unknown handles walk the new-user signup over the wire
  (synthesizing period-plausible PII); known handles log in with their password.
- **Reads and responds**: opens a conference, picks a recent message from another
  caller, reads it, and either replies (threaded) or starts a new thread — weighted
  by the persona's `intent.post` / `intent.reply`. Message bodies come from the
  fine-tuned voice model, so the traffic reads like real 1990s callers.
- **Cadence**: each persona dials on its own schedule, shorter gaps for higher
  `call_urge`, with startup stagger and jitter so load ebbs and flows.

## Host-agnostic by design

The session driver (`internal/dialer/session.go`) contains **no board-specific
logic**. Everything a target board needs — the prompt regexes, menu keys, signup
script, reader/reply flow — lives in a `Profile` (`internal/dialer/profile.go`).
`BuiltinTresBBS()` is one target; supporting Renegade / Mystic / Synchronet is a
new `Profile`, not engine changes.

## Layout

| File | Role |
|------|------|
| `transport.go` | telnet client: dial, IAC negotiation, `ReadUntil(regex)` screen reader |
| `profile.go`   | per-BBS prompts + navigation (the host-agnostic seam) |
| `session.go`   | one call: dial → login/register → read → reply/post → logoff |
| `pool.go`      | the population: cadence, concurrency, busy/retry, stats |
| `cmd/dialer`   | CLI + live event feed |

## Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--host` | `localhost:2323` | target BBS telnet address |
| `--voice-model` | `lmstudio:carrier-voice-moe@q8_0` | voice model id (`mock` = canned, fast) |
| `--callers` | 4 | max concurrent calls (the board's node count is the real cap) |
| `--min-gap` / `--max-gap` | 3s / 20s | per-persona wait between call attempts |
| `--duration` | 2m | run length (0 = until Ctrl-C) |
| `--one <handle>` | — | dial a single persona once, with a session trace (debug) |
| `--password` | `carrier1` | password sim callers register/login with |

## Notes

- The `mock` voice is deterministic and instant — use it to exercise the dial/busy/
  retry/registration mechanics without LLM latency; use the real voice model for
  authentic message content.
- Set `DIAL_DEBUG=1` to dump the raw reader listing per call.
