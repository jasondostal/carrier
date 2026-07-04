// Package telnet is a minimal dial-in listener: it gives a human a real BBS
// session on the *live* board — the same world the LLM cast is running in — over
// plain telnet. By design the session lives in the caller's own terminal window
// (you `telnet host port` from another window); it is deliberately NOT nested
// inside the sysop TUI, which would be the bad-UX terminal-in-a-terminal.
package telnet

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
)

// Backend is the slice of the board a dialed-in human can touch. The
// orchestrator implements it thread-safely, so sessions run concurrently with
// the tick loop.
type Backend interface {
	WhoOnline() []string
	RecentPosts(n int) []string
	Post(handle, board, subject, body string)
	LordSheet(handle string) string
	LordMove(handle, move, target string) string
}

// Server accepts telnet connections and runs a session per caller.
type Server struct {
	addr string
	be   Backend
}

// NewServer builds a dial-in server bound to addr (e.g. ":2323").
func NewServer(addr string, be Backend) *Server { return &Server{addr: addr, be: be} }

// ListenAndServe blocks serving dial-ins; run it in a goroutine.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.session(conn)
	}
}

func (s *Server) session(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)

	write(conn, banner)
	write(conn, "\r\nPick a handle: ")
	handle := readLine(r)
	if strings.TrimSpace(handle) == "" {
		handle = "guest"
	}
	write(conn, fmt.Sprintf("\r\nWelcome, %s. You're dialed into a LIVE board — the regulars are AI, and they can see you.\r\n", handle))

	for {
		write(conn, menu)
		switch strings.ToUpper(strings.TrimSpace(readLine(r))) {
		case "W":
			write(conn, "\r\n── Who's Online ──\r\n")
			lines := s.be.WhoOnline()
			if len(lines) == 0 {
				write(conn, "  (nobody on a node right now)\r\n")
			}
			for _, l := range lines {
				write(conn, "  "+l+"\r\n")
			}
		case "R":
			write(conn, "\r\n── Recent Posts ──\r\n")
			lines := s.be.RecentPosts(15)
			if len(lines) == 0 {
				write(conn, "  (board's quiet)\r\n")
			}
			for _, l := range lines {
				write(conn, "  "+l+"\r\n\r\n")
			}
		case "P":
			write(conn, "Subject: ")
			subj := readLine(r)
			write(conn, "Message (one line): ")
			body := readLine(r)
			if strings.TrimSpace(body) == "" {
				write(conn, "\r\nEmpty — skipped.\r\n")
				break
			}
			s.be.Post(handle, "General", subj, body)
			write(conn, "\r\nPosted to General. The room can see it now.\r\n")
		case "L":
			s.lord(r, conn, handle)
		case "Q", "":
			write(conn, "\r\nNO CARRIER\r\n")
			return
		default:
			write(conn, "\r\n?REDO\r\n")
		}
	}
}

func (s *Server) lord(r *bufio.Reader, conn io.Writer, handle string) {
	for {
		write(conn, "\r\n══ Legend of the Red Dragon ══\r\n  "+s.be.LordSheet(handle)+"\r\n")
		write(conn, "  (F)orest  (I)nn  (S)hop  (A)ttack  (B)ack: ")
		switch strings.ToUpper(strings.TrimSpace(readLine(r))) {
		case "F":
			write(conn, "\r\n"+s.be.LordMove(handle, "forest", "")+"\r\n")
		case "I":
			write(conn, "\r\n"+s.be.LordMove(handle, "inn", "")+"\r\n")
		case "S":
			write(conn, "\r\n"+s.be.LordMove(handle, "shop", "")+"\r\n")
		case "A":
			write(conn, "Ambush whom (handle)? ")
			target := strings.TrimSpace(readLine(r))
			write(conn, "\r\n"+s.be.LordMove(handle, "attack", target)+"\r\n")
		case "B", "":
			return
		default:
			write(conn, "\r\n?\r\n")
		}
	}
}

func write(w io.Writer, s string) { _, _ = io.WriteString(w, s) }

// readLine reads one line, translating CRLF and quietly consuming telnet IAC
// negotiation sequences so option bytes never leak into user input.
func readLine(r *bufio.Reader) string {
	var b strings.Builder
	for {
		c, err := r.ReadByte()
		if err != nil {
			return b.String()
		}
		switch {
		case c == '\n':
			return b.String()
		case c == '\r':
			// ignore; clients send CRLF or CR NUL
		case c == 0xFF: // IAC
			cmd, err := r.ReadByte()
			if err != nil {
				return b.String()
			}
			switch {
			case cmd == 250: // SB ... IAC SE
				for {
					x, err := r.ReadByte()
					if err != nil {
						return b.String()
					}
					if x == 0xFF {
						if y, err := r.ReadByte(); err != nil || y == 240 {
							break
						}
					}
				}
			case cmd >= 251 && cmd <= 254: // WILL/WONT/DO/DONT + one option byte
				_, _ = r.ReadByte()
			case cmd == 255: // escaped literal 0xFF
				b.WriteByte(0xFF)
			}
		default:
			b.WriteByte(c)
		}
	}
}

const banner = "\r\n" +
	"  +======================================+\r\n" +
	"  |   C A R R I E R   -   dial-in node    |\r\n" +
	"  |   a live board full of... characters  |\r\n" +
	"  +======================================+\r\n"

const menu = "\r\n" +
	"  +- MAIN ------------------------------+\r\n" +
	"  | (W)ho's online    (R)ead the board  |\r\n" +
	"  | (P)ost a message  (L)egend of Dragon|\r\n" +
	"  | (Q)uit                              |\r\n" +
	"  +-------------------------------------+\r\n" +
	"  > "
