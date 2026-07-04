# carrier — BBS Feature Matrix & Definition of Complete

## Philosophy

carrier is a simulated BBS engine in Go whose "callers" are LLM-driven persona agents, with a human sysop who can watch and intervene. Two things stay **first-class, not bolt-ons**: (1) the LLM caller population, and (2) human-sysop play (watch + stir).

Guiding rules for this roadmap:

- **ENiGMA½ is a feature *map*, not a behavioral test target.** We mine ENiGMA½ (primary), Synchronet, and Mystic for the *shape* of a complete board — what subsystems exist and how they relate — not to clone any one of them.
- **Standards are the finish line.** The real acceptance criterion is interop: can carrier run an *unmodified* DOS door through the standard drop-file/socket seam, speak QWK/REP and FidoNet packet formats, and transfer over Zmodem? Those are the durable "done" lines because they're externally defined and testable against real artifacts.
- **Front-load the emotional payoff.** The cast playing a *real* door game (LORD) that the sysop watches and can stir is worth more than breadth. Cheap-native features that produce that scene come first; the heavy drop-file/v86 interop lift comes when the payoff loop already exists.

Column legend for **Build approach**:
- **native** — reimplement in Go; cheapest, full control, no external artifacts.
- **standard-interop** — implement a published wire/file format so real external tools/doors/readers work unmodified. This is where "done" is defined.
- **inherit-via-ENiGMA-bridge** — optionally shell out to / bridge an ENiGMA½ instance rather than reimplement (fallback for expensive subsystems like v86).

---

## Feature Matrix

| Feature | What it is | carrier status | Build approach | Notes / priority |
|---|---|---|---|---|
| **Sessions / nodes** | Multiple concurrent callers on numbered nodes, with login contention | **done** | native | Core already real. Keep node model as the substrate everything else hangs off. |
| **New-user / matrix login** | First-contact flow: matrix menu, new-user application, auth | **missing** (identity persists, but no matrix/new-user gate) | native | Cheap. For LLM callers this is a persona "registration"; for the human sysop it's a real prompt. Synchronet/Mystic gate here. |
| **Time limits / timebank** | Per-call time budget; bank unused minutes for later | **missing** | native | Time limits are core; timebank is a classic extra (often a door historically). Cheap-native. Gives callers scarcity → drama. |
| **Menu / navigation shell** | The module/menu tree callers move through; the spine of the UX | **missing** | native | ENiGMA = HJSON menus + JS mods + MCI/lightbars; Mystic = event-driven menus + lightbar/SearchLight; Synchronet = Baja/JS command shells. carrier needs a lightweight menu module so callers "walk" the board. **High priority** — prerequisite for the payoff loop. |
| **Message bases (threaded)** | Public conferences/sub-boards, post + reply, threading | **done** | native | Real. ENiGMA/Sync/Mystic all model groups → sub-boards. Mystic stores JAM; carrier can keep its own store and only speak standards at the QWK/FTN seam. |
| **Private mail** | Person-to-person mail, incl. "secret" flag | **done** | native | Real. Maps to netmail conceptually; QWK/FTN export is a later interop concern. |
| **Oneliners** | Shout-wall of short caller quips | **missing** | native | Trivial, high flavor. Great LLM-caller surface. |
| **Bulletins** | Sysop-authored notice screens | **partial** (Daily-News bulletin done) | native | Generalize the news mechanism into a bulletin set. Cheap. |
| **User list / last callers** | Who's registered / who dialed in recently | **missing** | native | Cheap; strong social texture for a cast of personas. |
| **ANSI / MCI art + screens** | CP437/ANSI art, pipe-color codes, MCI macros that inject user/session data | **partial** (TUI renders, no MCI/screen system) | native | ENiGMA: Renegade pipe codes, SAUCE, CP437/UTF-8, lightbar MCI; Mystic: MCI replaces codes with user data. A small MCI + screen-file layer makes the board feel authentic and drives the TUI adapter. |
| **Doors (drop-file seam)** | Launch an external program, handing it a drop file describing the user/session | **stub** (LORD counter, no real gameplay, no drop file) | standard-interop | The seam is the finish line: emit **DOOR.SYS** (GAP-origin, 52-line de-facto std), **DORINFO1.DEF** (12-line; RBBS/QuickBBS/RemoteAccess), **CHAIN.TXT** (WWIV, 32-line), and **DOOR32.SYS** (11-line, modern socket std — line 1 comm type `2=telnet`, line 2 socket handle). Get *one* unmodified door running through this to call the seam done. |
| **Native door game (LORD)** | A real, playable door game the cast engages with | **done (native v1)** | native | Promoted from stub to a real Go gameplay loop: forest combat, leveling, gold, gear (King Arthur's / Abdul's), the Inn (Violet → marriage), and PvP ambush. Real DOS `LORD.EXE` comes later via v86. |
| **Real DOS door via v86** | Run an *unmodified* DOS door binary in an x86 emulator with socket/FOSSIL redirection | **missing** | standard-interop (or inherit-via-ENiGMA-bridge) | ENiGMA's headline: native x86/DOS door emulation via **v86**, running LORD/TradeWars/PimpWars with zero external deps. Mystic does it via **FOSSIL redirection + CP437→UTF-8**. **The big lift** — flag as flex/finish-line, not core. Bridging to ENiGMA is a legitimate fallback. |
| **File areas + ratios** | Browseable file bases, per-user up/download ratios & credits | **missing** | native | ENiGMA: Gazelle-inspired bases, full-text search, tags, FILE_ID.DIZ/NFO extraction; Mystic: blind upload, archive-peek, 99-line DIZ. Areas + ratios are native/cheap; the *transfer* is the interop part (next row). |
| **File transfer protocols** | Actually move bytes: X/Y/Zmodem (+ZedZap) | **missing** | standard-interop | ENiGMA supports X/Y/Zmodem; Mystic adds Ymodem-G and Zmodem/8K ZEDZAP. Only matters once a *human* dials in over telnet; for LLM callers file "transfer" can be abstract. Zmodem is the true finish-line protocol. |
| **Multi-node / node chat** | Real-time chat between callers on different nodes; channels + private paging | **missing** | native | Mystic: IRC-like multinode chat, rooms, scrollback, user-to-user paging, up to 255 users; Synchronet: multinode chat + node-to-node messaging. Native and cheap; **huge** for a cast of LLM personas interacting live. |
| **Sysop presence / node spy** | Sysop watches a live node's I/O in real time | **missing** | native | Synchronet exposes sysop **spy** via its MQTT broker: subscribe to a node's terminal output, optionally inject local keystrokes back; Mystic: external node monitor snoop/chat. **This is a TUI-tab feature in carrier** and directly serves first-class sysop play. |
| **Sysop paging / break-in / "stir"** | Sysop pages a node, breaks into chat, or injects input mid-session | **missing** | native | Synchronet's spy channel already injects keystrokes into a node. For carrier this is the **"stir"**: sysop drops a message/event into an LLM caller's session or forces a chat. Native, cheap, and one of the two first-class pillars. |
| **Message networking — QWK/REP** | Offline-mail packet exchange between systems | **missing** | standard-interop | `ID.QWK` out / `ID.REP` in, `.MSG` inside; Synchronet was first to support QWK natively (1992); ENiGMA has QWK support. Interop finish-line for message bases. |
| **Message networking — FidoNet (FTN)** | Echomail/netmail over FidoNet: packet + transport | **missing** | standard-interop | FTS-0001 packet/`.MSG` format; ENiGMA: FTN + BinkleyTerm-Style-Outbound (BSO) + native BinkP mailer; Mystic: 5D BSO tosser + BINKP mailer + AreaFix. **BinkP + BSO** is the true finish line. Deep; flex-tier. |
| **Telnet / SSH listener** | Real terminal dial-in so a *human* can connect | **missing** | standard-interop | ENiGMA: Telnet, SSH, secure/insecure WebSocket; Mystic/Synchronet: Telnet + RLogin + SSH. **TUI-tab candidate**: a "dial-in" tab lets the human sysop join as a *caller*, not just a watcher. Zmodem transfer only becomes meaningful once this exists. |
| **Event scheduler** | Time-based/interval jobs (nightly maint, tosses, events) | **missing** | native | ENiGMA has an event scheduler; Synchronet has timed events; Mystic has event-driven menus/intervals. Native/cheap; underpins networking tosses and daily news. |
| **Scripting / mods** | Sysop-authored logic (games, mods) in a scripting layer | **n/a for carrier** | native | ENiGMA = JS mods; Synchronet = Baja + JavaScript; Mystic = MPL + Python. carrier's equivalent is Go modules + the LLM callers themselves; **not a required subsystem**, noted for completeness. |
| **Content servers (Gopher/NNTP/web)** | Expose message bases over other protocols | **missing** | native (optional) | ENiGMA exposes bases via Gopher + NNTP + web. Nice-to-have; low priority for carrier. |
| **Achievements** | Gamified milestones for callers | **missing** | native (optional) | ENiGMA has an expandable achievement system. Cheap flavor; pairs well with LLM personas. Low priority. |

---

## Definition of Complete (tiers)

### Tier 1 — Core BBS
The board is alive and the payoff scene works. All native, all cheap.

- New-user / matrix login gate ✧ nodes/sessions with contention *(done)*
- Menu / navigation shell
- Threaded message bases *(done)* ✧ private mail w/ secret flag *(done)*
- Bulletins/news *(partial)* ✧ oneliners ✧ user list + last-callers
- Time limits (timebank optional)
- **One real door — native LORD — with actual gameplay** *(done)*
- **Sysop presence: node-spy TUI tab** (watch a caller live)
- **Sysop stir: paging / break-in / input injection** into a caller session
- First-class throughout: LLM caller population + human sysop

### Tier 2 — Full BBS
The board has depth and social real-time texture. Mostly native.

- File areas + ratios (native) with at least one **transfer protocol** (Zmodem, standard-interop)
- Multi-node real-time chat + private paging between callers
- Full sysop chat suite (page → split-screen chat → return)
- ANSI/MCI art + screen-file system
- Event scheduler
- Timebank

### Tier 3 — Interop (flex / finish lines)
Externally-defined "done." Each is verifiable against real artifacts/tools.

- **Unmodified door via the drop-file seam** — emit DOOR.SYS + DORINFO1.DEF + DOOR32.SYS, hand off socket/FOSSIL, and a stock door runs. *(This is the primary interop acceptance test.)*
- **Real DOS door via v86** (or FOSSIL-redirected DOSBox/dosemu) — run actual `LORD.EXE`. Fallback: inherit-via-ENiGMA-bridge.
- **QWK/REP** import + export.
- **FidoNet FTN** — packet/`.MSG` + BSO + BinkP mailer.
- **Telnet/SSH listener** for human dial-in (the "dial-in" TUI tab).
- **Zmodem** (+X/Y) file transfer end-to-end with a real terminal.

---

## Recommended Build Order

Ordered to reach the emotional payoff — *the cast playing a real LORD game while the sysop watches and stirs* — as early as possible, then widen.

**Phase 0 — done:** login/nodes/sessions, threaded message bases, private mail, persona identity+memory, Daily-News, **native LORD door**.

**Phase 1 — The Payoff Loop (all cheap-native):**
1. **Menu / navigation shell** — callers actually "move" through the board. Prerequisite for everything visible.
2. **Native LORD door** — *done*: real Go gameplay behind a door seam that's drop-file-shaped from day one.
3. **Sysop node-spy TUI tab** — the sysop watches a caller playing LORD in real time. Reuses the existing Bubble Tea adapter.
4. **Sysop stir** — paging / break-in / inject an event or message into a caller's live session. Completes the watch-and-intervene pillar.

*Milestone: a human sysop opens a tab, watches the LLM cast grind LORD, and pokes one of them.*

**Phase 2 — Social Surface (cheap-native):** multinode chat + paging; oneliners, user list, last-callers, bulletins; new-user/matrix login, time limits, timebank; ANSI/MCI art + screens; event scheduler.

**Phase 3 — Files:** file areas + ratios (native); Zmodem/X/Y transfer (standard-interop, meaningful once telnet lands).

**Phase 4 — Interop Finish Lines (the lift — flag each as flex):** drop-file door seam hardened (DOOR32.SYS + DORINFO1.DEF + socket, prove an unmodified door runs); real DOS door via v86 (biggest lift; fallback = ENiGMA bridge); telnet/SSH listener (human dials in as a caller); QWK/REP then FidoNet FTN/BSO/BinkP.

**Cheap-native vs. interop-lift at a glance:**
- *Cheap-native (do first):* menu shell, native LORD *(done)*, node-spy, sysop stir, multinode chat, oneliners/user-list, matrix login, time/timebank, MCI/art, event scheduler, file areas/ratios.
- *Interop lift (finish lines, deliberately later):* drop-file door seam (moderate), **real DOS door via v86 (large)**, telnet/SSH + Zmodem (moderate), QWK/REP (moderate), FidoNet BinkP/BSO (large).

---

### Sources
- ENiGMA½: [README](https://github.com/NuSkooler/enigma-bbs/blob/master/README.md), [Features](https://enigma-bbs.github.io/features/), [QWK docs](https://nuskooler.github.io/enigma-bbs/messageareas/qwk.html), [File areas](https://nuskooler.github.io/enigma-bbs/filebase/index.html)
- Synchronet: [features](https://www.synchro.net/docs/features.html), [Wikipedia](https://en.wikipedia.org/wiki/Synchronet), [JS/Baja wiki](http://wiki.synchro.net/custom:javascript), [Sysop docs](https://www.synchro.net/docs/sysop.html)
- Mystic BBS: [features](https://www.mysticbbs.com/features.html), [Wikipedia](https://en.wikipedia.org/wiki/Mystic_BBS), [menu_commands](https://wiki.mysticbbs.com/doku.php?id=menu_commands)
- Standards: [DOOR32.SYS spec](https://github.com/NuSkooler/ansi-bbs/blob/master/docs/dropfile_formats/door32_sys.txt), [WWIV doors/dropfiles](https://docs.wwivbbs.org/en/latest/chains/doors/), [QWK format](https://en.wikipedia.org/wiki/QWK_(file_format)), [FidoNet](https://bbs.fandom.com/wiki/FidoNet)
