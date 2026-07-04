// Package console is the native console adapter — the first implementation of
// the host port. It streams board activity to a writer so you watch the colony
// live: CONNECT / NO CARRIER, posts, whispered mail, the Daily News, and a node
// status line. The ant farm, behind glass.
package console

import (
	"fmt"
	"io"
	"strings"

	"github.com/jasondostal/carrier/internal/domain"
)

const (
	reset  = "\033[0m"
	dim    = "\033[2m"
	bold   = "\033[1m"
	green  = "\033[32m"
	cyan   = "\033[36m"
	yellow = "\033[33m"
	red    = "\033[31m"
	mag    = "\033[35m"
)

// Host renders the board to a terminal writer.
type Host struct{ w io.Writer }

// New builds a console host over the given writer (usually os.Stdout).
func New(w io.Writer) *Host { return &Host{w: w} }

func (h *Host) line(s string) { fmt.Fprintln(h.w, s) }

func (h *Host) Connect(p *domain.Persona) {
	h.line(fmt.Sprintf("%s*** CONNECT 2400%s %s— node %d — %s %s(%s)%s",
		green, reset, cyan, p.Node, p.Handle, dim, p.Model, reset))
}

func (h *Host) Disconnect(p *domain.Persona) {
	h.line(fmt.Sprintf("%sNO CARRIER%s %s— %s hung up (node %d)%s", red, reset, dim, p.Handle, p.Node, reset))
}

func (h *Host) Post(p *domain.Post) {
	tag := "new"
	if p.ReplyTo != 0 {
		tag = fmt.Sprintf("re:#%d", p.ReplyTo)
	}
	h.line(fmt.Sprintf("%s[%s]%s %s%s%s %s%s· %s%s\n%s",
		yellow, p.Board, reset, bold, p.Author, reset, dim, tag, p.Subject, reset, wrap(p.Body)))
}

func (h *Host) Mail(m *domain.Mail) {
	kind, col := "MAIL", cyan
	if m.Secret {
		kind, col = "♥ SECRET", mag
	}
	h.line(fmt.Sprintf("%s%s %s → %s%s\n%s", col, kind, m.From, m.To, reset, wrap(m.Body)))
}

func (h *Host) News(item domain.NewsItem) {
	h.line(fmt.Sprintf("%s%s── Daily News · day %d ──%s\n%s",
		bold, yellow, item.Day, reset, wrap(item.Text)))
}

func (h *Host) Status(w *domain.World, online []*domain.Persona) {
	nodes := "idle"
	if len(online) > 0 {
		var parts []string
		for _, p := range online {
			parts = append(parts, fmt.Sprintf("n%d:%s", p.Node, p.Handle))
		}
		nodes = strings.Join(parts, " ")
	}
	posts := 0
	for _, b := range w.Boards {
		posts += len(b.Posts)
	}
	h.line(fmt.Sprintf("%s── tick %d · day %d · nodes[%d]: %s · %d posts · %d mail ──%s",
		dim, w.Tick, w.Day, w.Nodes, nodes, posts, len(w.Mail), reset))
}

func (h *Host) Close() {}

// wrap indents and soft-wraps a body to ~76 columns for readability.
func wrap(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const width = 76
	var out []string
	line := "    "
	for _, word := range strings.Fields(s) {
		if len(line)+len(word)+1 > width && strings.TrimSpace(line) != "" {
			out = append(out, line)
			line = "    "
		}
		if strings.TrimSpace(line) == "" {
			line += word
		} else {
			line += " " + word
		}
	}
	if strings.TrimSpace(line) != "" {
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
