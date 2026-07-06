package dialer

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/voice"
)

// Caller drives one telnet session against a target BBS, in one persona's voice.
// It logs in (or registers on first contact), reads the newest message, and
// responds — generating the real dial-in traffic a board would see.
type Caller struct {
	Persona *domain.Persona
	Voice   voice.Composer
	Prof    *Profile
	Pass       string      // fixed password (same on signup and future logins)
	Echo       string      // conference to work in (e.g. "General")
	RNG        *rand.Rand  // per-caller randomness (target choice, post-vs-reply)
	Chattiness float64     // 0..1 chance this call posts/replies vs just lurking
	Log        func(Event) // event sink
}

// Event is one thing that happened during a call, for the pool's live feed.
type Event struct {
	Handle string
	Kind   string // dial|busy|login|register|read|post|reply|logoff|error
	Detail string
}

// Outcome summarizes a completed (or failed) call.
type Outcome struct {
	Handle     string
	Busy       bool
	Registered bool
	Read       int
	Posted     int
	Replied    int
	Err        error
}

func (c *Caller) emit(kind, detail string) {
	if c.Log != nil {
		c.Log(Event{Handle: c.Persona.Handle, Kind: kind, Detail: detail})
	}
}

// Dial places one call and runs the caller's activity. A refused connection
// returns an error (host down); a full board returns Outcome{Busy:true}.
func (c *Caller) Dial(ctx context.Context, addr string) Outcome {
	out := Outcome{Handle: c.Persona.Handle}
	p := c.Prof

	c.emit("dial", addr)
	conn, err := Dial(addr, 5*time.Second)
	if err != nil {
		out.Err = err
		c.emit("error", "no carrier: "+err.Error())
		return out
	}
	defer conn.Close()
	conn.Negotiate(ctx)

	// --- reach the login prompt (or detect a busy board) ---
	i, _, err := conn.ReadUntil(ctx, p.ConnectWait, p.UserPrompt, p.Busy, p.MainMenu)
	if err != nil {
		out.Err = err
		c.emit("error", "read banner: "+err.Error())
		return out
	}
	if i == 1 { // Busy
		out.Busy = true
		c.emit("busy", "all nodes busy")
		return out
	}

	// --- login or register ---
	if i != 2 { // not already at a menu → we're at User Name:
		conn.SendLine(c.Persona.Handle)
		j, _, _ := conn.ReadUntil(ctx, p.ActWait, p.PassPrompt, p.NewUserAsk, p.Busy, p.BadLogin)
		switch j {
		case 0: // existing user → password
			conn.SendLine(c.Pass)
			c.emit("login", c.Persona.Handle)
		case 1: // new user → run signup
			if !c.register(ctx, conn) {
				out.Err = fmt.Errorf("registration did not complete")
				c.emit("error", "registration stalled")
				return out
			}
			out.Registered = true
			c.emit("register", c.Persona.Handle)
		case 2:
			out.Busy = true
			c.emit("busy", "busy at login")
			return out
		default:
			out.Err = fmt.Errorf("unexpected login response")
			c.emit("error", "login: no known prompt")
			return out
		}
	}

	if !c.toMainMenu(ctx, conn) {
		out.Err = fmt.Errorf("never reached main menu")
		c.emit("error", "no main menu (dup login? locked?)")
		return out
	}

	// --- activity: read the board, then respond ---
	c.doActivity(ctx, conn, &out)

	// --- hang up ---
	conn.Send(p.Logoff)
	conn.ReadUntil(ctx, 2*time.Second, regexp.MustCompile(`Goodbye|NO CARRIER|left`))
	c.emit("logoff", "")
	return out
}

// register walks the profile's signup script.
func (c *Caller) register(ctx context.Context, conn *Conn) bool {
	p := c.Prof
	conn.Send(p.NewUserYes)
	conn.Send("\r")
	for _, step := range p.Register {
		if i, _, _ := conn.ReadUntil(ctx, p.ActWait, step.Prompt); i < 0 {
			return false
		}
		conn.SendLine(c.regField(step.Field))
	}
	return true
}

// toMainMenu dismisses any pauses/welcome screens until the main-menu prompt.
func (c *Caller) toMainMenu(ctx context.Context, conn *Conn) bool {
	p := c.Prof
	for attempt := 0; attempt < 8; attempt++ {
		i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.MainMenu, p.Pause, p.Welcome, p.BadLogin)
		switch i {
		case 0:
			return true
		case 1, 2: // pause / welcome → press a key
			conn.Send(p.PauseKey)
		case 3:
			return false // bad login / locked
		default:
			conn.Send("\r") // nudge
		}
	}
	return false
}

// doActivity performs the read-then-respond loop that is the point of the sim.
func (c *Caller) doActivity(ctx context.Context, conn *Conn, out *Outcome) {
	p := c.Prof

	// Enter the message subsystem.
	conn.Send(p.ToMsgArea)
	if i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.MsgMenu, p.MainMenu); i < 0 {
		return
	}

	// Read the conference: capture the listing.
	conn.Send(p.ReadKey)
	_, listing, _ := conn.ReadUntil(ctx, p.ActWait, p.ReaderPrompt)
	if os.Getenv("DIAL_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "\n---LISTING---\n%q\n---END---\n", ansiRe.ReplaceAllString(listing, ""))
	}
	out.Read++
	c.emit("read", c.Echo)

	// Most calls are a lurk: read the board and hang up without posting. Only a
	// Chattiness fraction actually writes — this is what keeps volume realistic
	// (a real board saw far more reads than posts).
	if c.RNG.Float64() >= c.Chattiness {
		conn.SendLine("0") // exit the reader
		conn.ReadUntil(ctx, p.ActWait, p.MsgMenu, p.MainMenu)
		conn.Send(p.MsgMenuToMain)
		conn.ReadUntil(ctx, p.ActWait, p.MainMenu)
		return
	}

	// Choose a message to answer: a random recent one from other callers (not
	// always the newest), so replies spread across threads instead of stacking.
	targets := c.gatherTargets(listing)
	var target msgRef
	if len(targets) > 0 {
		target = targets[c.RNG.Intn(len(targets))]
	}

	// Decide reply-vs-new-thread by the persona's intent weights. With nothing
	// to reply to, always post.
	if target.id > 0 && c.wantsReply() {
		// Drill into the target first (real "read" behavior + reply context).
		conn.SendLine(strconv.Itoa(target.id))
		_, body, _ := conn.ReadUntil(ctx, p.ActWait, p.Pause, p.MainMenu, p.MsgMenu)
		quoted := extractBody(body)
		conn.Send(p.PauseKey)
		conn.ReadUntil(ctx, p.ActWait, p.MsgMenu, p.MainMenu)

		if c.reply(ctx, conn, target, quoted) {
			out.Replied++
			c.emit("reply", fmt.Sprintf("to %s (#%d): %s", target.author, target.id, target.subject))
		}
	} else {
		// Exit the reader, then start a new thread.
		conn.SendLine("0")
		conn.ReadUntil(ctx, p.ActWait, p.MsgMenu, p.MainMenu)
		if c.post(ctx, conn) {
			out.Posted++
			c.emit("post", c.Echo)
		}
	}

	// Back to the main menu for logoff.
	conn.Send(p.MsgMenuToMain)
	conn.ReadUntil(ctx, p.ActWait, p.MainMenu)
}

// post composes and enters a new thread.
func (c *Caller) post(ctx context.Context, conn *Conn) bool {
	p := c.Prof
	body, err := c.compose(ctx, voice.Request{Kind: domain.ActPost, Echo: c.Echo})
	if err != nil || body == "" {
		return false
	}
	conn.Send(p.PostKey)
	if i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.SubjectPrompt); i < 0 {
		return false
	}
	conn.SendLine(voice.Subject(body))
	if i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.BodyPrompt); i < 0 {
		return false
	}
	sendBody(conn, body)
	conn.ReadUntil(ctx, p.ActWait, p.PostedOK, p.Pause)
	conn.Send(p.PauseKey)
	return true
}

// reply composes and enters a threaded reply to target.
func (c *Caller) reply(ctx context.Context, conn *Conn, target msgRef, quoted string) bool {
	p := c.Prof
	body, err := c.compose(ctx, voice.Request{
		Kind: domain.ActReply, Echo: c.Echo,
		To: target.author, Subject: target.subject, Quoted: quoted,
	})
	if err != nil || body == "" {
		return false
	}
	// From the message menu, open the reader and choose Reply.
	conn.Send(p.ReadKey)
	if i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.ReaderPrompt); i < 0 {
		return false
	}
	conn.SendLine(p.ReplyKey)
	if i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.ReplyIDPrompt); i < 0 {
		return false
	}
	conn.SendLine(strconv.Itoa(target.id))
	if i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.ReplySubjPrompt); i < 0 {
		return false
	}
	conn.SendLine("") // accept the default "Re: ..." subject
	if i, _, _ := conn.ReadUntil(ctx, p.ActWait, p.ReplyBodyPrompt); i < 0 {
		return false
	}
	sendBody(conn, body)
	conn.ReadUntil(ctx, p.ActWait, p.PostedOK, p.Pause)
	conn.Send(p.PauseKey)
	return true
}

func (c *Caller) compose(ctx context.Context, r voice.Request) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return c.Voice.Compose(cctx, c.Persona, r)
}

// --- helpers ---

type msgRef struct {
	id      int
	author  string
	subject string
}

// gatherTargets parses the reader listing into every message NOT authored by
// this caller — the pool of things it could reply to.
func (c *Caller) gatherTargets(listing string) []msgRef {
	listing = ansiRe.ReplaceAllString(listing, "")
	var out []msgRef
	for _, m := range c.Prof.MsgLine.FindAllStringSubmatch(listing, -1) {
		id, _ := strconv.Atoi(m[1])
		author := strings.TrimSpace(m[2])
		if id == 0 || strings.EqualFold(author, c.Persona.Handle) {
			continue
		}
		out = append(out, msgRef{id: id, author: author, subject: strings.TrimSpace(m[3])})
	}
	return out
}

// wantsReply rolls reply-vs-new-thread from the persona's intent weights.
func (c *Caller) wantsReply() bool {
	post, reply := c.Persona.Intent.Post, c.Persona.Intent.Reply
	if post <= 0 && reply <= 0 {
		post, reply = 0.5, 0.5
	}
	return c.RNG.Float64() < reply/(post+reply)
}

// sendBody transmits a message body as the editor expects: content line(s) then
// an empty line to finish. Newlines in the body are flattened to keep one blank
// line from terminating the post early.
func sendBody(conn *Conn, body string) {
	oneLine := strings.Join(strings.Fields(strings.ReplaceAll(body, "\n", " ")), " ")
	conn.SendLine(oneLine)
	conn.SendLine("") // empty line finishes the message
}

// extractBody pulls the message text out of a read-message screen (everything
// after the separator rule), best-effort.
func extractBody(screen string) string {
	screen = ansiRe.ReplaceAllString(screen, "")
	if i := strings.LastIndex(screen, "════"); i >= 0 {
		if nl := strings.IndexByte(screen[i:], '\n'); nl >= 0 {
			body := screen[i+nl+1:]
			if j := strings.Index(body, "Press Enter"); j >= 0 {
				body = body[:j]
			}
			return strings.TrimSpace(body)
		}
	}
	return ""
}

var stateTable = []string{"OR", "OH", "CA", "OK", "AZ", "TX", "NY", "WA", "IL", "MI", "FL", "WI"}
var cityTable = []string{"Portland", "Akron", "Sacramento", "Tulsa", "Mesa", "Austin", "Buffalo", "Tacoma", "Peoria", "Flint", "Tampa", "Madison"}

// regField supplies a value for one signup prompt, synthesizing plausible period
// PII deterministically from the handle so a persona re-registers identically.
func (c *Caller) regField(field string) string {
	h := fnv.New32a()
	h.Write([]byte(c.Persona.Handle))
	seed := h.Sum32()
	switch field {
	case "alias":
		return c.Persona.Handle
	case "name":
		if c.Persona.Name != "" {
			return c.Persona.Name
		}
		return c.Persona.Handle
	case "password":
		return c.Pass
	case "city":
		return cityTable[seed%uint32(len(cityTable))]
	case "state":
		return stateTable[seed%uint32(len(stateTable))]
	case "phone":
		return fmt.Sprintf("%03d-555-%04d", 200+seed%700, seed%10000)
	case "zip":
		return fmt.Sprintf("%05d", 10000+seed%89999)
	case "street":
		return fmt.Sprintf("%d Modem Ln", 100+seed%8900)
	case "email":
		return strings.ToLower(c.Persona.Handle) + "@fidonet.org"
	default: // free-form questionnaire answer
		ans := []string{"a Commodore 64", "an IBM PC XT", "a Tandy 1000", "my dad's 386"}
		return ans[seed%uint32(len(ans))]
	}
}
