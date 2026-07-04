// Command colony runs a carrier board: a population of LLM-driven callers
// dialing into a simulated BBS you watch (and, later, run) as sysop.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"

	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/host/console"
	"github.com/jasondostal/carrier/internal/host/tui"
	"github.com/jasondostal/carrier/internal/llm"
	"github.com/jasondostal/carrier/internal/memory"
	"github.com/jasondostal/carrier/internal/orchestrator"
)

func main() {
	personasDir := flag.String("personas", "personas", "personas directory")
	ticks := flag.Int("ticks", 20, "number of ticks to run")
	nodes := flag.Int("nodes", 2, "phone lines — the online-set cap")
	perDay := flag.Int("day", 8, "ticks per day (Daily News cadence)")
	seed := flag.Int64("seed", 1, "RNG seed for reproducible runs")
	mock := flag.Bool("mock", false, "run offline with canned actions (no OpenRouter spend)")
	useTUI := flag.Bool("tui", false, "render in the full-screen Bubble Tea sysop console")
	persist := flag.Bool("persist", false, "write new memories back to disk (living world across runs); default is ephemeral")
	flag.Parse()

	personas, bank, err := memory.Load(*personasDir, *persist)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load personas:", err)
		os.Exit(1)
	}
	if len(personas) == 0 {
		fmt.Fprintln(os.Stderr, "no personas found in", *personasDir)
		os.Exit(1)
	}

	w := domain.NewWorld(*nodes, "General", "Sysops")
	c := llm.New(*mock)

	o := &orchestrator.Orchestrator{
		World: w, LLM: c, Bank: bank, Personas: personas,
		RNG: rand.New(rand.NewSource(*seed)), TicksPerDay: *perDay,
	}

	mode := "LIVE (OpenRouter)"
	if *mock {
		mode = "MOCK (offline, no spend)"
	}
	if *persist {
		mode += " · persistent"
	} else {
		mode += " · ephemeral"
	}

	if *useTUI {
		// The TUI owns the main loop: the orchestrator runs in a goroutine that
		// the model's Init kicks once the program is live (so no early CONNECT is
		// dropped), while Run blocks the main thread until the sysop quits.
		h := tui.New(func() { o.Run(context.Background(), *ticks) })
		o.Host = h
		defer h.Close()
		if err := h.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "tui:", err)
			os.Exit(1)
		}
		return
	}

	h := console.New(os.Stdout)
	o.Host = h
	defer h.Close()

	fmt.Printf("carrier — %d callers, %d nodes, %s\n\n", len(personas), *nodes, mode)
	o.Run(context.Background(), *ticks)
}
