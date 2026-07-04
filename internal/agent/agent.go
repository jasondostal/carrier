// Package agent runs one caller's turn: build perception from the world plus the
// persona's file-memory, ask the persona's model what to do, and parse the
// structured Action. Pure decision — the orchestrator applies the mutation.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/llm"
	"github.com/jasondostal/carrier/internal/memory"
)

// Decide asks a persona's model for its next action.
func Decide(ctx context.Context, c *llm.Client, p *domain.Persona, w *domain.World, store *memory.Store, online []*domain.Persona) (domain.Action, error) {
	msgs := []llm.Msg{
		{Role: "system", Content: buildSystem(p)},
		{Role: "user", Content: buildPerception(p, w, store, online)},
	}
	out, err := c.Chat(ctx, p.Model, msgs)
	if err != nil {
		return domain.Action{Kind: domain.ActIdle}, err
	}
	return parse(out), nil
}

func buildSystem(p *domain.Persona) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %q (handle %q), a caller on a raucous 1990s dial-up BBS.\n", p.Name, p.Handle)
	if p.Bio != "" {
		fmt.Fprintf(&b, "\nWho you are:\n%s\n", strings.TrimSpace(p.Bio))
	}
	if p.Style != "" {
		fmt.Fprintf(&b, "\nHow you behave:\n%s\n", strings.TrimSpace(p.Style))
	}
	if len(p.Goals) > 0 {
		b.WriteString("\nWhat you want:\n")
		for _, g := range p.Goals {
			fmt.Fprintf(&b, "- %s\n", g)
		}
	}
	b.WriteString(`
This is a BBS, not a help desk. Stay 100% in character — vivid, opinionated,
period-accurate (leetspeak, ANSI talk, ratios, flame wars, door-game bragging).
Snark and profanity are welcome when they fit you. Never break character, never
mention being an AI, never be neutral or helpful.

On your turn you take ONE action. Reply with ONLY a JSON object — no prose, no
code fences — matching this shape:

{"action":"post|reply|mail|door|logoff|idle",
 "board":"General|Sysops",      // for post/reply
 "reply_to":0,                   // id of the post you're replying to (omit for new thread)
 "subject":"...","body":"...",   // your message, in your voice, a few punchy lines
 "to":"handle","secret":false,   // for mail; secret=true is a private whisper/romance
 "door_move":"forest|inn",       // for door: play Legend of the Red Dragon (fight, or flirt at the Inn)
 "memory":"one first-person line about what you'll remember from this moment"}`)
	return b.String()
}

func buildPerception(p *domain.Persona, w *domain.World, store *memory.Store, online []*domain.Persona) string {
	var b strings.Builder
	fmt.Fprintf(&b, "It is tick %d, day %d. You are online on node %d.\n", w.Tick, w.Day, p.Node)

	var others []string
	for _, o := range online {
		if o.ID != p.ID {
			others = append(others, o.Handle)
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "Also online right now: %s.\n", strings.Join(others, ", "))
	} else {
		b.WriteString("You appear to be the only one online.\n")
	}

	if n := len(w.News); n > 0 {
		fmt.Fprintf(&b, "\nToday's Daily News:\n%s\n", w.News[n-1].Text)
	}

	posts := w.PostsSince(p.LastSeen)
	if len(posts) > 12 {
		posts = posts[len(posts)-12:]
	}
	if len(posts) > 0 {
		b.WriteString("\nNew posts since you last looked:\n")
		for _, ps := range posts {
			rt := ""
			if ps.ReplyTo != 0 {
				rt = fmt.Sprintf(" (re:#%d)", ps.ReplyTo)
			}
			fmt.Fprintf(&b, "  #%d [%s] %s%s: %s — %s\n", ps.ID, ps.Board, ps.Author, rt, ps.Subject, oneLine(ps.Body))
		}
	} else {
		b.WriteString("\nThe boards are quiet since you last looked.\n")
	}

	if mail := w.UnreadMail(p.Handle, p.LastSeen); len(mail) > 0 {
		b.WriteString("\nPrivate mail for you:\n")
		for _, m := range mail {
			tag := ""
			if m.Secret {
				tag = " (secret)"
			}
			fmt.Fprintf(&b, "  from %s%s: %s\n", m.From, tag, oneLine(m.Body))
		}
	}

	d := w.Door(p.ID)
	fmt.Fprintf(&b, "\nYour Red Dragon stats: level %d, %d gold, %d forest fights today, charm %d.\n",
		d.Level, d.Gold, d.Forest, d.Charm)

	if store != nil {
		if rel := store.Relationships(); rel != "" {
			fmt.Fprintf(&b, "\nHow you feel about people (relationships.yaml):\n%s\n", rel)
		}
		if mems := store.Recent(6); len(mems) > 0 {
			b.WriteString("\nThings you remember:\n")
			for _, m := range mems {
				fmt.Fprintf(&b, "  - %s\n", m)
			}
		}
	}

	b.WriteString("\nWhat do you do? Respond with ONLY the JSON action.")
	return b.String()
}

func parse(s string) domain.Action {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var a domain.Action
	if err := json.Unmarshal([]byte(s), &a); err != nil || a.Kind == "" {
		return domain.Action{Kind: domain.ActIdle}
	}
	return a
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return s
}
