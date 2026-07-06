package dialer

import (
	"bufio"
	"context"
	"math/rand"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/voice"
)

// fakeBBS is a tiny scripted board used to drive the caller without tresbbs. It
// implements just enough of the tresbbs prompt protocol for one call: telnet
// negotiation, new-user signup, a one-message reader, and a threaded reply. It
// records what the caller did so the test can assert on real behavior.
type fakeBBS struct {
	ln         net.Listener
	registered chan string
	replied    chan string
}

func newFakeBBS(t *testing.T) *fakeBBS {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeBBS{ln: ln, registered: make(chan string, 1), replied: make(chan string, 1)}
	go f.serve()
	return f
}

func (f *fakeBBS) addr() string { return f.ln.Addr().String() }
func (f *fakeBBS) close()       { f.ln.Close() }

func (f *fakeBBS) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(s string) { conn.Write([]byte(s)) }
	readLine := func() string {
		var b strings.Builder
		for {
			c, err := r.ReadByte()
			if err != nil {
				return b.String()
			}
			if c == 255 { // skip IAC 3-byte commands from the client
				r.ReadByte()
				r.ReadByte()
				continue
			}
			if c == '\r' {
				// block for the paired \n/0 (it may arrive in a later packet);
				// unread a real char so we don't eat the next line.
				if nb, err := r.ReadByte(); err == nil && nb != '\n' && nb != 0 {
					r.UnreadByte()
				}
				return b.String()
			}
			if c == '\n' {
				return b.String()
			}
			b.WriteByte(c)
		}
	}
	readKey := func() byte {
		for {
			c, err := r.ReadByte()
			if err != nil {
				return 0
			}
			if c == 255 {
				r.ReadByte()
				r.ReadByte()
				continue
			}
			if c == '\r' || c == '\n' {
				continue
			}
			return c
		}
	}

	// Opening negotiation: offer char-mode + echo like a real BBS.
	w(string([]byte{255, 251, 1, 255, 251, 3, 255, 253, 3}))
	w("Welcome to FakeBBS\r\nUser Name: ")

	handle := readLine()
	// Unknown user → new-user flow.
	w("New user? (y/N)? ")
	readKey() // 'Y'
	for _, prompt := range []string{
		"Choose an alias: ", "Your real name: ", "Your city: ", "Phone number: ",
		"Street address (optional): ", "State (optional): ", "ZIP code (optional): ",
		"Email address (optional): ", "Choose a password: ",
		"What was your first computer? ", "How did you hear about this BBS? ",
	} {
		w(prompt)
		readLine()
	}
	f.registered <- handle
	w("Welcome to FakeBBS, " + handle + "!\r\nPress Enter to continue...")
	readLine()

	// Main menu.
	mainMenu := func() { w("\r\nEnter Selection - [B M F G ?]? ") }
	mainMenu()
	for {
		switch readKey() {
		case 'M', 'm':
			f.messageMenu(w, readLine, readKey)
			mainMenu()
		case 'G', 'g':
			w("Goodbye!\r\n")
			return
		default:
			mainMenu()
		}
	}
}

func (f *fakeBBS) messageMenu(w func(string), readLine func() string, readKey func() byte) {
	menu := func() { w("\r\nEnter Selection - [C E R N Y S Q M X P G ?]? ") }
	menu()
	for {
		switch readKey() {
		case 'R', 'r':
			// listing with one message from someone else, then reply flow. The
			// reader prompt takes a LINE (as tresbbs does), not a single key.
			w("=== General ===\r\n")
			w("   7 - 01/02/94 12:00 SysGuru        anybody know good ANSI editors?\r\n")
			w("\r\nRead#  <R>eply  <D>elete  <M>ail  0=exit: ")
			switch strings.ToLower(strings.TrimSpace(readLine())) {
			case "r":
				w("Message # to reply to: ")
				readLine()
				w("Subject [Re: anybody know good ANSI editors?]: ")
				readLine()
				w("Enter reply (empty line to finish):\r\n")
				var body []string
				for {
					ln := readLine()
					if ln == "" {
						break
					}
					body = append(body, ln)
				}
				f.replied <- strings.Join(body, " ")
				w("Reply posted!\r\nPress Enter to continue...")
				readLine()
			default:
				// drilling a message id: show the body, then pause.
				w("Number  : 7\r\nFrom    : SysGuru\r\n")
				w("════════════════════════════════════════\r\n")
				w("i've tried theDraw but want something lighter.\r\nPress Enter to continue...")
				readLine()
			}
			menu()
		case 'M', 'm':
			return
		default:
			menu()
		}
	}
}

func TestCallerRegistersAndReplies(t *testing.T) {
	f := newFakeBBS(t)
	defer f.close()

	persona := &domain.Persona{
		Handle: "TestCaller", Name: "Test User",
		Intent: domain.Intent{Reply: 1.0, Post: 0.0}, // force a reply
	}
	c := &Caller{
		Persona: persona,
		Voice:   voice.Mock{},
		Prof:    BuiltinTresBBS(),
		Pass:    "pw",
		Echo:    "General",
		RNG:     rand.New(rand.NewSource(1)),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out := c.Dial(ctx, f.addr())

	if out.Err != nil {
		t.Fatalf("dial error: %v", out.Err)
	}
	if !out.Registered {
		t.Error("expected the caller to register as a new user")
	}
	if out.Replied != 1 {
		t.Errorf("expected 1 reply, got %d (posted %d)", out.Replied, out.Posted)
	}

	select {
	case h := <-f.registered:
		if h != "TestCaller" {
			t.Errorf("registered handle = %q, want TestCaller", h)
		}
	default:
		t.Error("server never saw a registration")
	}
	select {
	case body := <-f.replied:
		if strings.TrimSpace(body) == "" {
			t.Error("reply body was empty")
		}
	default:
		t.Error("server never received a reply")
	}
}

func TestGatherTargetsSkipsSelfAndParses(t *testing.T) {
	c := &Caller{Persona: &domain.Persona{Handle: "warez_wolf"}, Prof: BuiltinTresBBS()}
	listing := "=== General ===\r\n" +
		"  14 - 01/01/01 00:00 NightOwl        got fresh warez here\r\n" +
		"  13 - 01/01/01 00:00 warez_wolf      my own post, skip me\r\n" +
		"   1 - 01/01/01 00:00 Jason           First post!\r\n" +
		"\r\nRead#  <R>eply  0=exit: "
	got := c.gatherTargets(listing)
	if len(got) != 2 {
		t.Fatalf("got %d targets, want 2 (self must be skipped): %+v", len(got), got)
	}
	if got[0].id != 14 || got[0].author != "NightOwl" {
		t.Errorf("target[0] = %+v, want id 14 NightOwl", got[0])
	}
	if got[0].subject != "got fresh warez here" {
		t.Errorf("subject = %q", got[0].subject)
	}
}
