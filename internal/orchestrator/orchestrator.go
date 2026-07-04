// Package orchestrator is the tick engine: a discrete-event sim where LLM-driven
// callers contend for a small number of phone lines (the online-set cap), take
// turns, and feed each other's drama. The node limit is the throttle that keeps
// a population of 20 affordable — only a few pay the meter per tick.
package orchestrator

import (
	"context"
	"math/rand"
	"strings"

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
}

// Run advances the sim by the given number of ticks.
func (o *Orchestrator) Run(ctx context.Context, ticks int) {
	for t := 0; t < ticks; t++ {
		o.World.Tick++
		o.admit()
		o.turns(ctx)
		o.dayBoundary()
	}
	o.Host.Status(o.World, o.online())
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
		o.Host.Connect(p)
	}
}

// turns gives each online caller one action, in shuffled order.
func (o *Orchestrator) turns(ctx context.Context) {
	on := o.online()
	o.RNG.Shuffle(len(on), func(i, j int) { on[i], on[j] = on[j], on[i] })
	for _, p := range on {
		// line-cycling: once a caller's session budget is up, the line drops so
		// someone else can dial in. Keeps the whole cast rotating through.
		if o.World.Tick-p.SessionStart >= p.SessionLen {
			o.Host.Disconnect(p)
			p.Online, p.Node = false, 0
			continue
		}
		store := o.Bank[p.ID]
		act, err := agent.Decide(ctx, o.LLM, p, o.World, store, on)
		if err != nil {
			continue // a caller's decision failing is non-fatal; skip their turn
		}
		o.apply(p, act, store)
		p.LastSeen = o.World.Tick
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

// dayBoundary flushes the Daily News and resets per-day door counters.
func (o *Orchestrator) dayBoundary() {
	if o.TicksPerDay <= 0 || o.World.Tick%o.TicksPerDay != 0 {
		return
	}
	o.World.Day++
	if len(o.pendingNews) > 0 {
		item := domain.NewsItem{Day: o.World.Day, Text: strings.Join(o.pendingNews, "\n")}
		o.World.News = append(o.World.News, item)
		o.Host.News(item)
		o.pendingNews = nil
	}
	for _, lp := range o.World.Lords {
		lp.NewDay()
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
