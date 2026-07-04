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
You are a real person typing on a BBS in 1994 — NOT a writer, NOT an AI. Type
like someone banging on a keyboard between classes or after a shift: short, fast,
concrete, a little petty. lowercase and typos are fine.

HARD RULES on voice:
- Talk about CONCRETE things: door-game scores and levels, specific file names
  and warez, upload/download ratios, who posted what, real grievances (someone
  deleted my message, the node's lagging, the sysop's asleep), trades, a/s/l,
  LORD (forest fights, Violet at the Inn, marriages). Use numbers and handles.
- REACT to what's actually on the board in front of you. Answer people by handle.
  Do not monologue into the void.
- BANNED: poetry or metaphors about static, silence, signals, dial tones, "the
  wire," ghosts, "who's really listening," the void. No atmospheric mood pieces,
  no philosophizing about the medium. If a post sounds like a moody AI being
  "mysterious," delete it and type what a bored teenager or a bitter 50-year-old
  would actually type instead.
- A couple lines, tops. Snark and profanity welcome when they fit you.

Not everyone on a BBS is a hacker. Plenty of callers are just normal people —
kids dodging homework, talking about school, music, crushes, parents hogging the
phone. Be exactly whoever your card says, not a generic l33t d00d. A teen girl
talks about her bio test and a boy named Ryan, not interrupt vectors.

Examples of the register:
  BAD:  "Static is easy to fill. Silence is where you find out who's listening."
  GOOD: "lol nobody cares Ron. hit LORD level 4 today. where you at old man"
  BAD:  "carrier in the static. you all sound the same from here."
  GOOD: "who keeps uploading broken .zip to the file area? warez_wolf that you? 0/10"
  GOOD: "omg is anyone on here NOT a boy arguing about computers?? i have a bio test tmrw and my mom keeps yelling at me to get off the phone"
  GOOD: "dude did anybody tape the new pantera? i'll trade my skate vids. also who's kitkat lol"

Never break character, never mention being an AI, never be neutral or helpful.

On your turn you take ONE action. Reply with ONLY a JSON object — no prose, no
code fences — matching this shape:

{"action":"post|reply|mail|door|logoff|idle",
 "board":"General|Sysops",      // for post/reply
 "reply_to":0,                   // id of the post you're replying to (omit for new thread)
 "subject":"...","body":"...",   // your message, in your voice, a few punchy lines
 "to":"handle","secret":false,   // for mail; secret=true is a private whisper/romance
 "door_move":"forest|inn|shop|attack", // for door: play Red Dragon
 "door_target":"handle",         // when door_move="attack": the caller you ambush
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

	for _, ps := range posts {
		if ps.Author == "SYSOP" {
			b.WriteString("\n⚡ The SYSOP — the operator who runs this whole board — is watching and just posted. That carries weight here; react to it.\n")
			break
		}
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

	fmt.Fprintf(&b, "\nYour Legend of the Red Dragon character: %s.\n", w.Lord(p.ID).Summary())
	b.WriteString("Door moves: forest (fight beasts for gold/exp), inn (flirt with Violet), " +
		"shop (buy better weapon/armor), attack (ambush another caller for their gold — set door_target to a handle).\n")

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
	s = stripThinking(s)
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

// stripThinking removes reasoning blocks some models inline in their content
// (<think>…</think>, <reasoning>…</reasoning>) so the JSON extraction that
// follows doesn't grab a brace from inside the model's scratch-work.
func stripThinking(s string) string {
	for _, tag := range [][2]string{{"<think>", "</think>"}, {"<reasoning>", "</reasoning>"}} {
		for {
			a := strings.Index(s, tag[0])
			b := strings.Index(s, tag[1])
			if a >= 0 && b > a {
				s = s[:a] + s[b+len(tag[1]):]
				continue
			}
			break
		}
	}
	return s
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return s
}
