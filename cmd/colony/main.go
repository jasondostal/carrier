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
	flag.Parse()

	personas, bank, err := memory.Load(*personasDir)
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
	h := console.New(os.Stdout)
	defer h.Close()

	o := &orchestrator.Orchestrator{
		World: w, LLM: c, Host: h, Bank: bank, Personas: personas,
		RNG: rand.New(rand.NewSource(*seed)), TicksPerDay: *perDay,
	}

	mode := "LIVE (OpenRouter)"
	if *mock {
		mode = "MOCK (offline, no spend)"
	}
	fmt.Printf("carrier — %d callers, %d nodes, %s\n\n", len(personas), *nodes, mode)
	o.Run(context.Background(), *ticks)
}
