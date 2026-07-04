// Package domain is the simulation core: personas, the world, and the actions
// callers take. It has NO knowledge of how a session is transported or
// rendered (that lives behind the host port) and no I/O of its own. Everything
// BBS-y — the TUI, a future ENiGMA bridge — hangs off the edges, never here.
package domain

// Persona is a caller: an LLM-driven character whose *model is its
// personality*. The brain (an OpenRouter model id) is chosen per persona, so
// behavioral diversity comes from model selection rather than prompt-wrangling.
type Persona struct {
	ID       string   // stable id, also the personas/<id> dir name
	Handle   string   // the handle shown on the boards
	Name     string   // "real" name — the sysop can see it
	Model    string   // OpenRouter model id: the brain
	Bio      string   // short backstory
	Style    string   // voice / behavior guidance
	Goals    []string // what they're here to do or stir up
	CallUrge float64  // 0..1 baseline propensity to dial in on a given tick

	// runtime state (not persisted)
	Online   bool
	Node     int // node number while online; 0 = offline
	LastSeen int // last tick this persona perceived the boards
}

// Post is a message-base post. ReplyTo == 0 means a new thread.
type Post struct {
	ID      int
	Board   string
	Tick    int
	Author  string // handle
	ReplyTo int
	Subject string
	Body    string
}

// Mail is private inter-caller mail. Secret flags the budding-romance path:
// the sysop can still see it (that's the fun), but other callers cannot.
type Mail struct {
	ID     int
	Tick   int
	From   string // handle
	To     string // handle
	Body   string
	Secret bool
}

// NewsItem is one line of the LORD-style Daily News bulletin — a drama seed the
// whole board wakes up to.
type NewsItem struct {
	Day  int
	Text string
}

// DoorState is LORD-lite, per persona: just enough to generate news and
// rivalries. Real LORD.EXE via a v86 host adapter is a later authenticity flex.
type DoorState struct {
	Level   int
	Gold    int
	Forest  int    // forest fights taken today
	Charm   int    // Violet flirts at the Inn
	Married string // handle of spouse, if any
	Alive   bool
}

// Board is a single message base.
type Board struct {
	Name  string
	Posts []*Post
}

// World is the entire simulated board at a moment in time. It is the single
// source of truth; host adapters only read it and stream events.
type World struct {
	Tick   int
	Day    int
	Nodes  int // number of phone lines (the online-set cap)
	Boards map[string]*Board
	Mail   []*Mail
	News   []NewsItem
	Doors  map[string]*DoorState // keyed by persona id

	nextPost int
	nextMail int
}

// NewWorld builds an empty board with the given boards and node count.
func NewWorld(nodes int, boards ...string) *World {
	w := &World{
		Nodes:  nodes,
		Boards: map[string]*Board{},
		Doors:  map[string]*DoorState{},
	}
	for _, b := range boards {
		w.Boards[b] = &Board{Name: b}
	}
	return w
}

// AddPost records a post and returns it (with an assigned ID). Unknown boards
// are created on the fly so a persona can't crash the world with a typo.
func (w *World) AddPost(p *Post) *Post {
	w.nextPost++
	p.ID = w.nextPost
	b, ok := w.Boards[p.Board]
	if !ok {
		b = &Board{Name: p.Board}
		w.Boards[p.Board] = b
	}
	b.Posts = append(b.Posts, p)
	return p
}

// AddMail records private mail and returns it (with an assigned ID).
func (w *World) AddMail(m *Mail) *Mail {
	w.nextMail++
	m.ID = w.nextMail
	w.Mail = append(w.Mail, m)
	return m
}

// PostsSince returns every post across all boards created after the given tick,
// oldest first — a caller's "what did I miss" feed.
func (w *World) PostsSince(tick int) []*Post {
	var out []*Post
	for _, b := range w.Boards {
		for _, p := range b.Posts {
			if p.Tick > tick {
				out = append(out, p)
			}
		}
	}
	return out
}

// UnreadMail returns mail addressed to a handle after the given tick.
func (w *World) UnreadMail(handle string, tick int) []*Mail {
	var out []*Mail
	for _, m := range w.Mail {
		if m.To == handle && m.Tick > tick {
			out = append(out, m)
		}
	}
	return out
}

// Door returns (creating if needed) the LORD state for a persona.
func (w *World) Door(id string) *DoorState {
	d, ok := w.Doors[id]
	if !ok {
		d = &DoorState{Level: 1, Gold: 20, Alive: true}
		w.Doors[id] = d
	}
	return d
}

// ActionKind enumerates what a caller can do on their turn.
type ActionKind string

const (
	ActPost   ActionKind = "post"
	ActReply  ActionKind = "reply"
	ActMail   ActionKind = "mail"
	ActDoor   ActionKind = "door"
	ActLogoff ActionKind = "logoff"
	ActIdle   ActionKind = "idle"
)

// Action is the structured decision an agent's model emits each turn. The model
// is instructed to reply with exactly this JSON shape.
type Action struct {
	Kind     ActionKind `json:"action"`
	Board    string     `json:"board,omitempty"`
	ReplyTo  int        `json:"reply_to,omitempty"`
	Subject  string     `json:"subject,omitempty"`
	Body     string     `json:"body,omitempty"`
	To       string     `json:"to,omitempty"`        // mail target handle
	Secret   bool       `json:"secret,omitempty"`    // secret (romance) mail
	DoorMove string     `json:"door_move,omitempty"` // "forest" | "inn"
	Memory   string     `json:"memory,omitempty"`    // what the persona chooses to remember
}
