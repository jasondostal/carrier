package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── event messages ──────────────────────────────────────────────────────────
//
// One message per host-port call. Each carries value copies (see tui.go) so the
// model can render without ever touching the live, mutating world. They double
// as feed entries: Update appends them to the log and re-renders.

type connectMsg struct {
	node        int
	handle      string
	model       string
	tick        int
}

type disconnectMsg struct {
	node   int
	handle string
}

type postMsg struct {
	board   string
	author  string
	subject string
	body    string
	replyTo int
	tick    int
}

type mailMsg struct {
	from   string
	to     string
	body   string
	secret bool
	tick   int
}

type doorMsg struct {
	text string
}

type newsMsg struct {
	day  int
	text string
}

// nodeStat is one online caller as seen by the sidebar.
type nodeStat struct {
	node   int
	handle string
	model  string
}

// statusMsg is the authoritative "glass" snapshot the orchestrator emits once at
// the end of a run; it reconciles the sidebar totals the model tracks live.
type statusMsg struct {
	tick   int
	day    int
	nodes  int
	online []nodeStat
	posts  int
	mail   int
}

// ── palette ─────────────────────────────────────────────────────────────────

var (
	colGreen  = lipgloss.Color("#5faf5f") // CONNECT
	colRed    = lipgloss.Color("#d75f5f") // NO CARRIER
	colYellow = lipgloss.Color("#d7af5f") // boards / news
	colCyan   = lipgloss.Color("#5fafaf") // mail
	colMag    = lipgloss.Color("#af87d7") // secret mail
	colDragon = lipgloss.Color("#e0803c") // Red Dragon / door events
	colDim    = lipgloss.Color("#6c6c6c") // metadata
	colText   = lipgloss.Color("#c6c6c6") // bodies
	colAccent = lipgloss.Color("#87afd7") // headers / handles
	colBar    = lipgloss.Color("#3a3a3a") // rules / borders
)

const sidebarWidth = 30 // total columns reserved for the status sidebar

// ── model ───────────────────────────────────────────────────────────────────

type model struct {
	run    func()       // the simulation entry point, kicked from Init
	inject func(string) // the sysop "stir": drop a SYSOP broadcast into the live board

	ready         bool
	width, height int
	vp            viewport.Model

	input     textinput.Model // sysop stir prompt
	inputMode bool

	entries []feedEntry // the running event log, re-rendered on resize

	// live sidebar state, tracked from events (Status is only emitted once).
	online   map[int]nodeStat
	tick     int
	day      int
	nodes    int
	posts    int
	mail     int
}

func newModel(run func(), inject func(string)) model {
	ti := textinput.New()
	ti.Prompt = "SYSOP> "
	ti.CharLimit = 240
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colDragon).Bold(true)
	return model{run: run, inject: inject, input: ti, online: map[int]nodeStat{}}
}

// Init kicks the simulation *after* the program is running so no early event is
// dropped, and asks for nothing else — Bubble Tea sends the first WindowSizeMsg.
func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		if m.run != nil {
			go m.run()
		}
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.inputMode {
			// While composing a sysop broadcast, keys feed the prompt.
			switch msg.String() {
			case "enter":
				if m.inject != nil {
					m.inject(m.input.Value())
				}
				m.input.Reset()
				m.input.Blur()
				m.inputMode = false
				return m, nil
			case "esc":
				m.input.Reset()
				m.input.Blur()
				m.inputMode = false
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "s":
			// Enter "stir" mode: type a message the whole cast will react to.
			m.inputMode = true
			m.input.Focus()
			return m, textinput.Blink
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		bodyH := m.height - 2 // header + footer, one line each
		if bodyH < 1 {
			bodyH = 1
		}
		vpW := m.width - sidebarWidth - 1 // sidebar + a one-column gutter
		if vpW < 10 {
			vpW = 10
		}
		if !m.ready {
			m.vp = viewport.New(vpW, bodyH)
			m.ready = true
		} else {
			m.vp.Width, m.vp.Height = vpW, bodyH
		}
		m.refresh()
		return m, nil

	case connectMsg:
		m.online[msg.node] = nodeStat{node: msg.node, handle: msg.handle, model: msg.model}
		m.bumpTick(msg.tick)
		m.append(msg)
		return m, nil

	case disconnectMsg:
		delete(m.online, msg.node)
		m.append(msg)
		return m, nil

	case postMsg:
		m.posts++
		m.bumpTick(msg.tick)
		m.append(msg)
		return m, nil

	case mailMsg:
		m.mail++
		m.bumpTick(msg.tick)
		m.append(msg)
		return m, nil

	case doorMsg:
		m.append(msg)
		return m, nil

	case newsMsg:
		if msg.day > m.day {
			m.day = msg.day
		}
		m.append(msg)
		return m, nil

	case statusMsg:
		// Reconcile with the authoritative snapshot.
		m.tick, m.day, m.nodes = msg.tick, msg.day, msg.nodes
		m.posts, m.mail = msg.posts, msg.mail
		m.online = map[int]nodeStat{}
		for _, n := range msg.online {
			m.online[n.node] = n
		}
		return m, nil
	}

	// Everything else (↑/↓, pgup/pgdn, wheel) drives the feed viewport.
	if m.ready {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) bumpTick(t int) {
	if t > m.tick {
		m.tick = t
	}
}

// append records a feed entry and re-renders the viewport, pinned to the bottom
// so the newest activity is always in view — the live-tail sysop feel.
func (m *model) append(e feedEntry) {
	m.entries = append(m.entries, e)
	m.refresh()
}

// refresh rebuilds the viewport content from the entry log at the current width
// (re-wrapping on resize) and scrolls to the newest line.
func (m *model) refresh() {
	if !m.ready {
		return
	}
	w := m.vp.Width
	blocks := make([]string, len(m.entries))
	for i, e := range m.entries {
		blocks[i] = e.render(w)
	}
	m.vp.SetContent(strings.Join(blocks, "\n"))
	m.vp.GotoBottom()
}

func (m model) View() string {
	if !m.ready {
		return "carrier — waking the modem…"
	}
	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.vp.View(),
		" ",
		m.sidebar(m.vp.Height),
	)
	return lipgloss.JoinVertical(lipgloss.Left, m.header(), body, m.footer())
}

// ── chrome ──────────────────────────────────────────────────────────────────

func (m model) header() string {
	return lipgloss.NewStyle().
		Width(m.width).
		Bold(true).
		Foreground(colAccent).
		Background(colBar).
		Padding(0, 1).
		Render("carrier — sysop console")
}

func (m model) footer() string {
	if m.inputMode {
		return lipgloss.NewStyle().
			Width(m.width).
			Padding(0, 1).
			Render(m.input.View() + lipgloss.NewStyle().Foreground(colDim).Render("   (enter to send · esc to cancel)"))
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Foreground(colDim).
		Padding(0, 1).
		Render("↑/↓ scroll · s stir (sysop broadcast) · q quit")
}

func (m model) sidebar(height int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colAccent).Render("NODES")

	var lines []string
	lines = append(lines, title)
	if len(m.online) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colDim).Render("idle"))
	} else {
		nodes := make([]nodeStat, 0, len(m.online))
		for _, n := range m.online {
			nodes = append(nodes, n)
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].node < nodes[j].node })
		for _, n := range nodes {
			head := lipgloss.NewStyle().Foreground(colGreen).
				Render(fmt.Sprintf("node %d ", n.node)) +
				lipgloss.NewStyle().Bold(true).Foreground(colText).Render(n.handle)
			sub := lipgloss.NewStyle().Foreground(colDim).Render("  " + shortModel(n.model))
			lines = append(lines, head, sub)
		}
	}

	rule := lipgloss.NewStyle().Foreground(colBar).Render(strings.Repeat("─", sidebarWidth-4))
	meta := lipgloss.NewStyle().Foreground(colText)
	lines = append(lines,
		"",
		rule,
		meta.Render(fmt.Sprintf("tick %d · day %d", m.tick, m.day)),
		meta.Render(fmt.Sprintf("posts %d · mail %d", m.posts, m.mail)),
	)

	return lipgloss.NewStyle().
		Width(sidebarWidth - 2). // 2 columns eaten by the border
		Height(height - 2).      // 2 rows eaten by the border
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBar).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

// shortModel trims the vendor prefix off an OpenRouter model id for the sidebar.
func shortModel(m string) string {
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return m
}
