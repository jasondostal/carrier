// Package dialer is carrier's OUTBOUND side: instead of simulating callers in
// carrier's own world, it drives real telnet sessions against an EXTERNAL BBS
// (tresbbs, or any host) so carrier becomes a host-agnostic traffic generator —
// a mock caller pool for load- and behavior-testing a real board.
//
// transport.go is the telnet CLIENT: TCP dial, IAC option negotiation as a
// well-behaved character-at-a-time client, and a screen reader that strips
// telnet commands + ANSI so callers can match on prompts. It is the mirror image
// of a BBS's server-side line reader.
package dialer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// Telnet command bytes (RFC 854/857/858).
const (
	iac  = 255
	se   = 240
	sb   = 250
	will = 251
	wont = 252
	do   = 253
	dont = 254
	optEcho = 1
	optSGA  = 3
)

// ansiRe strips CSI / charset escape sequences so prompt matching sees plain text.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][AB0]|\x1b[78]`)

// ErrBusy means the far end refused the call — the host is up but has no free
// node (TCP accepted then dropped, or an explicit "all nodes busy" banner). The
// pool treats this as a retry-later signal, distinct from a hard dial failure.
var ErrBusy = errors.New("line busy: no free node")

// Conn is a live telnet client session to a BBS.
type Conn struct {
	raw    net.Conn
	r      *bufio.Reader
	screen strings.Builder // rolling decoded transcript (IAC/ANSI stripped)
	trace  func(string)    // optional per-byte-batch tracer for debugging
}

// Dial opens a telnet connection. A refused/timed-out connect returns a wrapped
// error (host down); the caller distinguishes that from ErrBusy (host up, full).
func Dial(addr string, timeout time.Duration) (*Conn, error) {
	raw, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return &Conn{raw: raw, r: bufio.NewReader(raw)}, nil
}

// SetTrace installs an optional tracer that receives each decoded screen chunk.
func (c *Conn) SetTrace(f func(string)) { c.trace = f }

// Screen returns the full decoded transcript so far.
func (c *Conn) Screen() string { return c.screen.String() }

// Close hangs up.
func (c *Conn) Close() error {
	if c.raw == nil {
		return nil
	}
	return c.raw.Close()
}

// Send writes raw bytes (used for single menu keystrokes — no line ending).
func (c *Conn) Send(s string) error {
	_, err := c.raw.Write([]byte(s))
	return err
}

// SendLine writes a line the way a telnet client transmits Enter (CR LF).
func (c *Conn) SendLine(s string) error {
	_, err := c.raw.Write([]byte(s + "\r\n"))
	return err
}

// ReadUntil reads until one of the regexes matches the tail of the decoded
// screen, the context is cancelled, or an idle gap elapses with no match. It
// returns the index of the matched pattern (or -1) and the newly-read text.
// Telnet negotiation is answered inline so the far end flips us into char mode.
func (c *Conn) ReadUntil(ctx context.Context, idle time.Duration, pats ...*regexp.Regexp) (int, string, error) {
	var got strings.Builder
	deadline := time.Now().Add(idle)
	for {
		if ctx.Err() != nil {
			c.flush(got.String())
			return -1, got.String(), ctx.Err()
		}
		c.raw.SetReadDeadline(time.Now().Add(120 * time.Millisecond))
		b, err := c.r.ReadByte()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				// idle tick — check for a match on what we have, then keep waiting
				if i := matchTail(ansiRe.ReplaceAllString(got.String(), ""), pats); i >= 0 {
					c.flush(got.String())
					return i, got.String(), nil
				}
				if time.Now().After(deadline) {
					c.flush(got.String())
					return -1, got.String(), nil // idle timeout, no match (not an error)
				}
				continue
			}
			// real read error (EOF = far end hung up)
			c.flush(got.String())
			i := matchTail(ansiRe.ReplaceAllString(got.String(), ""), pats)
			if i >= 0 {
				return i, got.String(), nil
			}
			return -1, got.String(), err
		}
		if b == iac {
			c.answerCommand()
			continue
		}
		got.WriteByte(b)
		if i := matchTail(ansiRe.ReplaceAllString(got.String(), ""), pats); i >= 0 {
			c.flush(got.String())
			return i, got.String(), nil
		}
		deadline = time.Now().Add(idle) // saw data — extend the idle window
	}
}

// flush appends decoded text to the rolling transcript and traces it.
func (c *Conn) flush(s string) {
	clean := ansiRe.ReplaceAllString(s, "")
	c.screen.WriteString(clean)
	if c.trace != nil && clean != "" {
		c.trace(clean)
	}
}

// matchTail reports the index of the first pattern that matches s (searched over
// the whole accumulated text, which is fine for prompt detection).
func matchTail(s string, pats []*regexp.Regexp) int {
	for i, p := range pats {
		if p.MatchString(s) {
			return i
		}
	}
	return -1
}

// Negotiate performs the opening handshake: answer the server's option offers so
// it puts us in character-at-a-time mode with server echo (what a BBS wants),
// and proactively decline everything else. Call once right after Dial.
func (c *Conn) Negotiate(ctx context.Context) {
	// Give the server a beat to send its WILL/DO offers, answering inline.
	c.raw.SetReadDeadline(time.Now().Add(600 * time.Millisecond))
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			break // no more immediate negotiation traffic
		}
		if b == iac {
			c.answerCommand()
			continue
		}
		// First real content byte — put it back so the next ReadUntil sees the
		// full banner (including a leading "All nodes busy" that has no prefix).
		c.r.UnreadByte()
		break
	}
}

// answerCommand consumes one IAC sequence (the leading 255 already read) and
// replies like a cooperating char-mode client: accept ECHO + SGA, refuse the
// rest, and skip subnegotiations.
func (c *Conn) answerCommand() {
	cmd, err := c.r.ReadByte()
	if err != nil {
		return
	}
	switch cmd {
	case will, do, wont, dont:
		opt, err := c.r.ReadByte()
		if err != nil {
			return
		}
		c.reply(cmd, opt)
	case sb:
		for { // skip until IAC SE
			b, err := c.r.ReadByte()
			if err != nil {
				return
			}
			if b == iac {
				if n, err := c.r.ReadByte(); err != nil || n == se {
					return
				}
			}
		}
	}
}

// reply sends the client's answer to one option offer.
func (c *Conn) reply(cmd, opt byte) {
	var out []byte
	switch cmd {
	case will: // server offers to do opt
		if opt == optEcho || opt == optSGA {
			out = []byte{iac, do, opt} // yes, please echo / suppress-GA
		} else {
			out = []byte{iac, dont, opt}
		}
	case do: // server asks us to do opt
		if opt == optSGA {
			out = []byte{iac, will, optSGA}
		} else {
			out = []byte{iac, wont, opt} // we won't do terminal-type, NAWS, etc.
		}
	case wont:
		out = []byte{iac, dont, opt}
	case dont:
		out = []byte{iac, wont, opt}
	}
	if out != nil {
		c.raw.Write(out)
	}
}
