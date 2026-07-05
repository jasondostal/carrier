// Package voice is carrier's content layer: given an action the engine already
// decided on, it writes the in-persona message BODY. It is the only place the
// LLM is used in the engine-intent path — the model shrinks to one job, voice.
//
// The prompt is built in the exact shape the carrier voice model was fine-tuned
// on (system = handle + echo + style, user = thread context), so it performs the
// way its persona battery did. The Composer interface keeps the backend swappable
// — LM Studio today, any OpenAI-compatible provider (or a mock) tomorrow.
package voice

import (
	"context"
	"fmt"
	"strings"

	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/llm"
)

// Request is the focused context the voice model needs to write ONE message.
type Request struct {
	Kind    domain.ActionKind // post | reply | mail | door
	Echo    string            // board / echo name
	To      string            // handle being replied to / mailed
	Subject string            // subject being replied under
	Quoted  string            // the body being replied to (empty for a new thread)
	Event   string            // for door: the mechanical outcome to brag/complain about
}

// Composer writes an in-persona message body. Swap the implementation to change
// inference backend without touching the engine.
type Composer interface {
	Compose(ctx context.Context, p *domain.Persona, r Request) (string, error)
}

// chatter is the minimal inference dependency the LLM composer needs.
type chatter interface {
	ChatWith(ctx context.Context, model string, msgs []llm.Msg, o llm.Opts) (string, error)
}

// LLM composes over a fine-tuned voice model on any OpenAI-compatible provider.
// Model is a provider-routed id, e.g. "lmstudio:carrier-voice-8b".
type LLM struct {
	Client chatter
	Model  string
}

// Compose builds the trained-format prompt and returns the cleaned body. Short
// max-tokens + a frequency penalty keep the 8B from the repetition loops the
// persona battery exposed.
func (l LLM) Compose(ctx context.Context, p *domain.Persona, r Request) (string, error) {
	msgs := []llm.Msg{
		{Role: "system", Content: systemPrompt(p, r.Echo)},
		{Role: "user", Content: userPrompt(r)},
	}
	out, err := l.Client.ChatWith(ctx, l.Model, msgs, llm.Opts{Temperature: 0.85, MaxTokens: 220, FrequencyPenalty: 0.6})
	if err != nil {
		return "", err
	}
	return clean(out), nil
}

func systemPrompt(p *domain.Persona, echo string) string {
	if echo == "" {
		echo = "General"
	}
	style := strings.TrimSpace(p.Style)
	if style == "" {
		style = strings.TrimSpace(p.Bio)
	}
	return fmt.Sprintf(
		"You are %s, a caller on a 1990s BBS posting in the %s message echo. "+
			"Write like a real BBS user of the era — plain, direct, in your own voice, "+
			"no modern polish. You are: %s.", p.Handle, echo, style)
}

func userPrompt(r Request) string {
	switch r.Kind {
	case domain.ActDoor:
		return fmt.Sprintf("This just happened to you in the door game Legend of the Red Dragon: %s\n\n"+
			"Post a short brag or complaint about it to the board, in your voice — a line or two.", r.Event)
	case domain.ActMail:
		return fmt.Sprintf("You are writing a private message to %s.\n\nWrite your message.", r.To)
	case domain.ActReply:
		b := fmt.Sprintf("You are replying to %s about %q.", r.To, strings.TrimPrefix(r.Subject, "Re: "))
		if q := strings.TrimSpace(r.Quoted); q != "" {
			b += "\n\nThey wrote:\n" + q
		}
		return b + "\n\nWrite your reply."
	default: // new thread
		return fmt.Sprintf("You are starting a new thread in the %s echo.\n\nWrite your post.", r.Echo)
	}
}

// clean strips artifacts the model sometimes emits: a leading FidoNet quote
// header ("On <date>, X wrote to Y") and stray surrounding quotes/fences.
func clean(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`")
	lines := strings.Split(s, "\n")
	for len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		low := strings.ToLower(first)
		isHeader := (strings.HasPrefix(low, "on ") || strings.HasPrefix(low, "in a message")) &&
			(strings.Contains(low, "wrote") || strings.Contains(low, "said"))
		if isHeader {
			lines = lines[1:]
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Subject derives a short thread subject from a composed body (first line),
// since the model writes bodies, not headers. Used for new posts.
func Subject(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		ln = strings.Join(strings.Fields(ln), " ")
		if len(ln) > 48 {
			ln = strings.TrimSpace(ln[:48]) + "…"
		}
		return ln
	}
	return "(no subject)"
}
