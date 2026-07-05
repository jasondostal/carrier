// Package intent is carrier's decision layer: given a persona and the world, it
// CHOOSES one action from persona-weighted utility — no LLM, no reasoning call,
// no chance of an illegal move. This is the engine half of the engine/voice
// split: the model never decides what happens, only writes the words once the
// engine has decided a caller speaks.
//
// It sets everything about an Action EXCEPT the prose (Subject/Body), which the
// voice layer fills. Selection is seeded-RNG deterministic (fixed candidate
// order, no map iteration) so runs replay.
package intent

import (
	"math/rand"
	"strings"

	"github.com/jasondostal/carrier/internal/domain"
)

// Choose picks one action for p this turn. online is the current online-set
// (including p); rng is the orchestrator's seeded source (call under its lock).
func Choose(p *domain.Persona, w *domain.World, online []*domain.Persona, rng *rand.Rand) domain.Action {
	iw := withDefaults(p.Intent)

	// context: who else is around, what's new, what's mine.
	var others []string
	for _, o := range online {
		if o.ID != p.ID {
			others = append(others, o.Handle)
		}
	}
	posts := allPosts(w)
	mine := map[int]bool{}
	for _, ps := range posts {
		if ps.Author == p.Handle {
			mine[ps.ID] = true
		}
	}
	var targets []*domain.Post
	for _, ps := range posts {
		if ps.Author != p.Handle && ps.Tick > p.LastSeen {
			targets = append(targets, ps)
		}
	}

	// logoff urge climbs as a caller burns through its session budget.
	pressure := 0.0
	if p.SessionLen > 0 {
		if frac := float64(w.Tick-p.SessionStart) / float64(p.SessionLen); frac > 0.6 {
			pressure = frac
		}
	}

	// Candidate actions in a FIXED order (reproducibility), each with a utility.
	type cand struct {
		kind actionKind
		w    float64
	}
	cands := []cand{
		{kReply, cond(len(targets) > 0, iw.Reply)},
		{kPost, iw.Post},
		{kMail, cond(len(others) > 0, iw.Mail)},
		{kDoor, iw.Door},
		{kLogoff, iw.Logoff + pressure*1.5},
		{kIdle, 0.15}, // a small floor so quiet callers sometimes just lurk
	}
	total := 0.0
	for _, c := range cands {
		total += c.w
	}
	if total <= 0 {
		return domain.Action{Kind: domain.ActIdle}
	}
	roll := rng.Float64() * total
	pick := kIdle
	for _, c := range cands {
		roll -= c.w
		if roll <= 0 {
			pick = c.kind
			break
		}
	}

	switch pick {
	case kReply:
		t := pickTarget(targets, mine, p.Handle, rng)
		return domain.Action{
			Kind: domain.ActReply, Board: t.Board, ReplyTo: t.ID,
			To: t.Author, Subject: reSubject(t.Subject),
		}
	case kMail:
		return domain.Action{
			Kind: domain.ActMail, To: others[rng.Intn(len(others))],
			Secret: rng.Float64() < iw.Romance,
		}
	case kDoor:
		return doorAction(iw, others, rng)
	case kLogoff:
		return domain.Action{Kind: domain.ActLogoff}
	case kPost:
		board := "General"
		if rng.Float64() < 0.15 {
			board = "Sysops"
		}
		return domain.Action{Kind: domain.ActPost, Board: board}
	default:
		return domain.Action{Kind: domain.ActIdle}
	}
}

type actionKind int

const (
	kReply actionKind = iota
	kPost
	kMail
	kDoor
	kLogoff
	kIdle
)

func withDefaults(iw domain.Intent) domain.Intent {
	// A persona.yaml with no intent block gets a reasonable everyman profile;
	// individually-authored zeros are respected (that persona doesn't do it).
	if iw.Post == 0 && iw.Reply == 0 && iw.Mail == 0 && iw.Door == 0 {
		iw.Post, iw.Reply, iw.Door = 1.0, 1.5, 0.8
	}
	if iw.Logoff == 0 {
		iw.Logoff = 0.2
	}
	return iw
}

// pickTarget weights reply targets by salience: replies to ME, mentions of my
// handle, live humans, and the SYSOP pull the most.
func pickTarget(targets []*domain.Post, mine map[int]bool, handle string, rng *rand.Rand) *domain.Post {
	scores := make([]float64, len(targets))
	total := 0.0
	for i, t := range targets {
		s := 1.0
		if mine[t.ReplyTo] {
			s += 3
		}
		if strings.Contains(strings.ToLower(t.Body), strings.ToLower(handle)) {
			s += 2
		}
		if t.Human {
			s += 2.5
		}
		if t.Author == "SYSOP" {
			s += 1.5
		}
		scores[i] = s
		total += s
	}
	roll := rng.Float64() * total
	for i, s := range scores {
		roll -= s
		if roll <= 0 {
			return targets[i]
		}
	}
	return targets[len(targets)-1]
}

func doorAction(iw domain.Intent, others []string, rng *rand.Rand) domain.Action {
	a := domain.Action{Kind: domain.ActDoor, DoorMove: "forest"}
	switch {
	case len(others) > 0 && rng.Float64() < iw.Aggression*0.5:
		a.DoorMove, a.DoorTarget = "attack", others[rng.Intn(len(others))]
	case rng.Float64() < iw.Romance*0.4:
		a.DoorMove = "inn"
	case rng.Float64() < 0.2:
		a.DoorMove = "shop"
	}
	return a
}

func allPosts(w *domain.World) []*domain.Post {
	var out []*domain.Post
	for _, b := range w.Boards {
		out = append(out, b.Posts...)
	}
	return out
}

func reSubject(s string) string {
	s = strings.TrimSpace(s)
	for {
		if len(s) >= 3 && strings.EqualFold(s[:3], "re:") {
			s = strings.TrimSpace(s[3:])
			continue
		}
		break
	}
	if s == "" {
		s = "your post"
	}
	return "Re: " + s
}

func cond(ok bool, v float64) float64 {
	if ok {
		return v
	}
	return 0
}
