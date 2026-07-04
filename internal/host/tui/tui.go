// Package tui is the Bubble Tea host adapter — the "sysop console glass." It is
// a second implementation of the host port (alongside package console): instead
// of streaming lines to a writer, it drives a live full-screen terminal UI with
// a scrolling event feed and a node-status sidebar. Bubble Tea owns the main
// loop; the simulation runs in a goroutine and forwards events to the running
// program via (*tea.Program).Send. The domain never learns any of this exists.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasondostal/carrier/internal/domain"
)

// Host renders the board as a Bubble Tea program. It satisfies host.Host by
// translating each port call into a message posted to the running program; the
// model (model.go) does the actual rendering on the UI goroutine.
type Host struct {
	prog *tea.Program
}

// New builds a TUI host. run is the simulation's entry point (the orchestrator's
// Run, already closed over its context and tick count); it is kicked from the
// model's Init once the program is live — that ordering keeps the very first
// CONNECT/Post events from being dropped. inject is the sysop "stir" hook
// (orchestrator.InjectSysop): pressing `s` composes a broadcast and injects it.
func New(run func(), inject func(string)) *Host {
	m := newModel(run, inject)
	p := tea.NewProgram(m, tea.WithAltScreen())
	return &Host{prog: p}
}

// Run starts the program and blocks until the sysop quits (q / ctrl+c). It is
// meant to hold the main goroutine while the simulation drives from behind.
func (h *Host) Run() error {
	_, err := h.prog.Run()
	return err
}

// The host-port methods below run on the *orchestrator's* goroutine. Each snaps
// the values it needs into a message (never sharing a mutable domain pointer
// across the boundary) and hands it to the program; Send is a no-op once the
// program has quit, so a still-running sim can't panic on shutdown.

func (h *Host) Connect(p *domain.Persona) {
	h.prog.Send(connectMsg{node: p.Node, handle: p.Handle, model: p.Model, tick: p.SessionStart})
}

func (h *Host) Disconnect(p *domain.Persona) {
	h.prog.Send(disconnectMsg{node: p.Node, handle: p.Handle})
}

func (h *Host) Post(p *domain.Post) {
	h.prog.Send(postMsg{
		board: p.Board, author: p.Author, subject: p.Subject,
		body: p.Body, replyTo: p.ReplyTo, tick: p.Tick,
	})
}

func (h *Host) Mail(m *domain.Mail) {
	h.prog.Send(mailMsg{from: m.From, to: m.To, body: m.Body, secret: m.Secret, tick: m.Tick})
}

func (h *Host) Door(line string) {
	h.prog.Send(doorMsg{text: line})
}

func (h *Host) News(item domain.NewsItem) {
	h.prog.Send(newsMsg{day: item.Day, text: item.Text})
}

func (h *Host) Status(w *domain.World, online []*domain.Persona) {
	nodes := make([]nodeStat, 0, len(online))
	for _, p := range online {
		nodes = append(nodes, nodeStat{node: p.Node, handle: p.Handle, model: p.Model})
	}
	posts := 0
	for _, b := range w.Boards {
		posts += len(b.Posts)
	}
	h.prog.Send(statusMsg{
		tick: w.Tick, day: w.Day, nodes: w.Nodes,
		online: nodes, posts: posts, mail: len(w.Mail),
	})
}

// Close is a no-op: the program owns its own teardown via Run.
func (h *Host) Close() {}
