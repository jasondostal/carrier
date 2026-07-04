// Package orchestrator is the tick engine: a discrete-event sim where LLM-driven
// callers contend for a small number of phone lines (the online-set cap), take
// turns, and feed each other's drama. The node limit is the throttle that keeps
// a population of 20 affordable — only a few pay the meter per tick.
package orchestrator

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"github.com/jasondostal/carrier/internal/agent"
	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/host"
	"github.com/jasondostal/carrier/internal/llm"
	"github.com/jasondostal/carrier/internal/memory"
)

// Orchestrator wires the world, the model client, the host adapter, and the
// persona minds together and runs the loop.
type Orchestrator struct {
	World       *domain.World
	LLM         *llm.Client
	Host        host.Host
	Bank        memory.Bank
	Personas    []*domain.Persona
	RNG         *rand.Rand
	TicksPerDay int

	pendingNews []string

	mu           sync.Mutex // guards pendingSysop (written from the UI goroutine)
	pendingSysop []string
}

// InjectSysop queues a SYSOP broadcast to appear on the board at the next tick.
// This is the sysop "stir": it's safe to call from another goroutine (e.g. the
// TUI), so the human can drop a message into the live board and watch the cast
// react to the operator.
func (o *Orchestrator) InjectSysop(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	o.mu.Lock()
	o.pendingSysop = append(o.pendingSysop, text)
	o.mu.Unlock()
}

// drainSysop posts any queued SYSOP broadcasts to the board so callers perceive
// (and react to) the operator on their turn this tick.
func (o *Orchestrator) drainSysop() {
	o.mu.Lock()
	pending := o.pendingSysop
	o.pendingSysop = nil
	posts := make([]*domain.Post, 0, len(pending))
	for _, text := range pending {
		posts = append(posts, o.World.AddPost(&domain.Post{
			Board: "General", Tick: o.World.Tick, Author: "SYSOP", Subject: "** SYSOP **", Body: text,
		}))
	}
	o.mu.Unlock()
	for _, post := range posts {
		o.Host.Post(post)
	}
}

// Run advances the sim by the given number of ticks. The world mutex (o.mu)
// serializes every touch of the world between this loop and any live telnet
// callers; it is held only around quick reads/writes, never across an LLM call.
func (o *Orchestrator) Run(ctx context.Context, ticks int) {
	for t := 0; t < ticks; t++ {
		o.mu.Lock()
		o.World.Tick++
		o.mu.Unlock()
		o.drainSysop()
		o.admit()
		o.turns(ctx)
		o.dayBoundary()
	}
	o.mu.Lock()
	o.Host.Status(o.World, o.online())
	o.mu.Unlock()
}

func (o *Orchestrator) online() []*domain.Persona {
	var on []*domain.Persona
	for _, p := range o.Personas {
		if p.Online {
			on = append(on, p)
		}
	}
	return on
}

func (o *Orchestrator) freeNode() int {
	used := map[int]bool{}
	for _, p := range o.Personas {
		if p.Online {
			used[p.Node] = true
		}
	}
	for n := 1; n <= o.World.Nodes; n++ {
		if !used[n] {
			return n
		}
	}
	return 0
}

// admit fills free lines from offline callers, weighted by call urge. This is
// where "the board is busy" and "who's online together" emerge.
func (o *Orchestrator) admit() {
	o.mu.Lock()
	var connected []*domain.Persona
	for {
		n := o.freeNode()
		if n == 0 {
			break
		}
		var cand []*domain.Persona
		for _, p := range o.Personas {
			if !p.Online && o.RNG.Float64() < p.CallUrge {
				cand = append(cand, p)
			}
		}
		if len(cand) == 0 {
			break
		}
		p := cand[o.RNG.Intn(len(cand))]
		p.Online, p.Node = true, n
		p.SessionStart, p.SessionLen = o.World.Tick, 2+o.RNG.Intn(4) // stay 2–5 ticks
		connected = append(connected, p)
	}
	o.mu.Unlock()
	for _, p := range connected {
		o.Host.Connect(p)
	}
}

// turns gives each online caller one action, in shuffled order. The world lock
// is held only while reading perception and applying the result — never during
// the LLM call — so a live telnet caller can post between turns without racing.
func (o *Orchestrator) turns(ctx context.Context) {
	o.mu.Lock()
	on := o.online()
	o.RNG.Shuffle(len(on), func(i, j int) { on[i], on[j] = on[j], on[i] })
	o.mu.Unlock()

	// Build every caller's perception under the lock (a consistent pre-tick
	// snapshot) and drop expired sessions, then fire all the model calls
	// CONCURRENTLY — a tick advances at the speed of one reasoning call, not the
	// sum of them. Results apply back in order under the lock.
	type job struct {
		p     *domain.Persona
		store *memory.Store
		msgs  []llm.Msg
	}
	o.mu.Lock()
	var jobs []job
	var expired []*domain.Persona
	for _, p := range on {
		// line-cycling: once a caller's session budget is up, the line drops.
		if o.World.Tick-p.SessionStart >= p.SessionLen {
			p.Online, p.Node = false, 0
			expired = append(expired, p)
			continue
		}
		store := o.Bank[p.ID]
		jobs = append(jobs, job{p: p, store: store, msgs: agent.Prompt(p, o.World, store, on)})
	}
	o.mu.Unlock()

	for _, p := range expired {
		o.Host.Disconnect(p)
	}

	type result struct {
		p     *domain.Persona
		store *memory.Store
		act   domain.Action
		ok    bool
	}
	results := make([]result, len(jobs))
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j := jobs[i]
			out, err := o.LLM.Chat(ctx, j.p.Model, j.msgs)
			if err != nil {
				return // leaves results[i].ok == false; a failed turn is non-fatal
			}
			results[i] = result{p: j.p, store: j.store, act: agent.Parse(out), ok: true}
		}(i)
	}
	wg.Wait()

	for _, r := range results {
		if !r.ok {
			continue
		}
		o.mu.Lock()
		o.apply(r.p, r.act, r.store)
		r.p.LastSeen = o.World.Tick
		o.mu.Unlock()
	}
}

func (o *Orchestrator) apply(p *domain.Persona, a domain.Action, store *memory.Store) {
	switch a.Kind {
	case domain.ActPost, domain.ActReply:
		board := a.Board
		if board == "" {
			board = "General"
		}
		post := o.World.AddPost(&domain.Post{
			Board: board, Tick: o.World.Tick, Author: p.Handle,
			ReplyTo: a.ReplyTo, Subject: a.Subject, Body: a.Body,
		})
		o.Host.Post(post)
	case domain.ActMail:
		if a.To != "" && strings.TrimSpace(a.Body) != "" {
			m := o.World.AddMail(&domain.Mail{
				Tick: o.World.Tick, From: p.Handle, To: a.To, Body: a.Body, Secret: a.Secret,
			})
			o.Host.Mail(m)
		}
	case domain.ActDoor:
		o.playLORD(p, a)
	case domain.ActLogoff:
		o.Host.Disconnect(p)
		p.Online, p.Node = false, 0
	}

	mem := a.Memory
	if mem == "" {
		mem = summarize(a)
	}
	if store != nil {
		_ = store.Append(o.World.Tick, mem)
	}
}

// playLORD resolves one Legend of the Red Dragon action, streams the narrated
// outcome to the sysop's feed, and queues notable events (level-ups, deaths,
// marriages) for the Daily News — the drama seed the whole board reacts to.
func (o *Orchestrator) playLORD(p *domain.Persona, a domain.Action) {
	lp := o.World.Lord(p.ID)
	var line string
	var notable bool
	switch a.DoorMove {
	case "inn":
		line, notable = o.World.Inn(p.Handle, lp, o.RNG)
	case "shop":
		line, notable = o.World.Shop(p.Handle, lp)
	case "attack":
		var def *domain.LordPlayer
		if t := o.personaByHandle(a.DoorTarget); t != nil {
			def = o.World.Lord(t.ID)
		}
		line, notable = o.World.Attack(p.Handle, lp, a.DoorTarget, def, o.RNG)
	default: // "forest" or anything unrecognized
		line, notable = o.World.Forest(p.Handle, lp, o.RNG)
	}
	o.Host.Door(line)
	if notable {
		o.pendingNews = append(o.pendingNews, line)
	}
}

// personaByHandle finds a persona by its BBS handle (for PvP targeting).
func (o *Orchestrator) personaByHandle(handle string) *domain.Persona {
	if handle == "" {
		return nil
	}
	for _, p := range o.Personas {
		if p.Handle == handle {
			return p
		}
	}
	return nil
}

// ── telnet Backend ───────────────────────────────────────────────────────────
//
// These methods let a live human caller (dialed in over telnet) touch the same
// world the LLM cast is running in. Every one takes the world lock, so they're
// safe to call concurrently with the tick loop. Human posts and door plays flow
// to the sysop's Host feed just like a persona's, so the operator sees the guest.

// WhoOnline returns one line per persona currently on a node.
func (o *Orchestrator) WhoOnline() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []string
	for _, p := range o.Personas {
		if p.Online {
			out = append(out, fmt.Sprintf("node %d  %s (%s)", p.Node, p.Handle, p.Model))
		}
	}
	return out
}

// RecentPosts returns up to n most-recent board posts, oldest first, preformatted.
func (o *Orchestrator) RecentPosts(n int) []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	posts := o.World.PostsSince(0)
	sort.Slice(posts, func(i, j int) bool { return posts[i].ID < posts[j].ID })
	if len(posts) > n {
		posts = posts[len(posts)-n:]
	}
	out := make([]string, 0, len(posts))
	for _, p := range posts {
		out = append(out, fmt.Sprintf("[%s] %s: %s\n    %s", p.Board, p.Author, p.Subject, firstLine(p.Body, 140)))
	}
	return out
}

// Post adds a human caller's message to the board and surfaces it to the sysop.
func (o *Orchestrator) Post(handle, board, subject, body string) {
	if strings.TrimSpace(board) == "" {
		board = "General"
	}
	o.mu.Lock()
	post := o.World.AddPost(&domain.Post{
		Board: board, Tick: o.World.Tick, Author: handle, Subject: subject, Body: body, Human: true,
	})
	o.mu.Unlock()
	o.Host.Post(post)
}

// LordSheet returns a human caller's Red Dragon character summary.
func (o *Orchestrator) LordSheet(handle string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.World.Lord(handle).Summary()
}

// LordMove resolves one Red Dragon action for a human caller and returns the
// narrated outcome (also streamed to the sysop feed).
func (o *Orchestrator) LordMove(handle, move, target string) string {
	o.mu.Lock()
	lp := o.World.Lord(handle)
	var line string
	switch move {
	case "inn":
		line, _ = o.World.Inn(handle, lp, o.RNG)
	case "shop":
		line, _ = o.World.Shop(handle, lp)
	case "attack":
		var def *domain.LordPlayer
		if t := o.personaByHandle(target); t != nil {
			def = o.World.Lord(t.ID)
		}
		line, _ = o.World.Attack(handle, lp, target, def, o.RNG)
	default:
		line, _ = o.World.Forest(handle, lp, o.RNG)
	}
	o.mu.Unlock()
	o.Host.Door(line)
	return line
}

// firstLine collapses whitespace and truncates for a compact preview.
func firstLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

// dayBoundary flushes the Daily News and resets per-day door counters.
func (o *Orchestrator) dayBoundary() {
	o.mu.Lock()
	if o.TicksPerDay <= 0 || o.World.Tick%o.TicksPerDay != 0 {
		o.mu.Unlock()
		return
	}
	o.World.Day++
	var item *domain.NewsItem
	if len(o.pendingNews) > 0 {
		it := domain.NewsItem{Day: o.World.Day, Text: strings.Join(o.pendingNews, "\n")}
		o.World.News = append(o.World.News, it)
		o.pendingNews = nil
		item = &it
	}
	for _, lp := range o.World.Lords {
		lp.NewDay()
	}
	o.mu.Unlock()
	if item != nil {
		o.Host.News(*item)
	}
}

func summarize(a domain.Action) string {
	switch a.Kind {
	case domain.ActPost, domain.ActReply:
		return "I posted on the board: " + a.Subject
	case domain.ActMail:
		return "I sent " + a.To + " private mail."
	case domain.ActDoor:
		return "I played Legend of the Red Dragon."
	case domain.ActLogoff:
		return "I logged off for the night."
	}
	return ""
}
