// Package host defines the presentation/transport PORT. The simulation core
// drives every session through this interface and knows nothing about what's on
// the other side. The console adapter renders to your terminal; a future Bubble
// Tea TUI (the "glass") or an ENiGMA½ bridge (real doors, real telnet callers)
// would implement this SAME interface. That single seam is the whole
// ports-and-adapters spine — build one adapter now, drop in others later
// without touching domain/.
package host

import "github.com/jasondostal/carrier/internal/domain"

// Host is one realization of the board: how sessions surface and render.
type Host interface {
	Connect(p *domain.Persona)                     // a caller dialed in
	Disconnect(p *domain.Persona)                  // NO CARRIER
	Post(p *domain.Post)                           // a post hit a board
	Mail(m *domain.Mail)                           // private mail flew (sysop sees it)
	Door(line string)                              // a Legend of the Red Dragon event
	News(item domain.NewsItem)                     // Daily News bulletin
	Status(w *domain.World, online []*domain.Persona) // the "glass": node status
	Close()
}
