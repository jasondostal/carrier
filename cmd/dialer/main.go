// Command dialer is carrier's outbound BBS traffic simulator: a pool of persona
// callers that dial a real BBS over telnet, retry on busy, register/login, read
// the board, and respond — generating realistic dial-in traffic for testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jasondostal/carrier/internal/dialer"
	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/llm"
	"github.com/jasondostal/carrier/internal/memory"
	"github.com/jasondostal/carrier/internal/voice"
)

func main() {
	addr := flag.String("host", "localhost:2323", "target BBS telnet address")
	profileName := flag.String("profile", "tresbbs", "BBS adapter to drive (see internal/dialer profile registry)")
	personasDir := flag.String("personas", "personas", "personas directory")
	voiceModel := flag.String("voice-model", "lmstudio:carrier-voice-moe@q8_0", "voice model id (empty/mock = canned)")
	pass := flag.String("password", "carrier1", "password sim callers register/login with")
	echo := flag.String("echo", "General", "conference to work in")
	one := flag.String("one", "", "dial exactly one persona by handle and exit (debug; traces the session)")
	callers := flag.Int("callers", 4, "max concurrent callers (the board's node count is the real cap)")
	minGap := flag.Duration("min-gap", 3*time.Second, "min wait between a persona's calls")
	maxGap := flag.Duration("max-gap", 20*time.Second, "max wait between a persona's calls")
	duration := flag.Duration("duration", 2*time.Minute, "how long to run the pool (0 = until Ctrl-C)")
	seed := flag.Int64("seed", 1, "RNG seed")
	flag.Parse()

	personas, _, err := memory.Load(*personasDir, false)
	if err != nil || len(personas) == 0 {
		fmt.Fprintln(os.Stderr, "load personas:", err)
		os.Exit(1)
	}

	client := llm.New(*voiceModel == "" || *voiceModel == "mock")
	var composer voice.Composer = voice.Mock{}
	if !client.Mock() {
		composer = voice.LLM{Client: client, Model: *voiceModel}
	}
	prof, err := dialer.Get(*profileName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Debug: dial a single persona with a live trace of the session.
	if *one != "" {
		pz := pick(personas, *one)
		if pz == nil {
			fmt.Fprintln(os.Stderr, "no such persona:", *one)
			os.Exit(1)
		}
		c := &dialer.Caller{Persona: pz, Voice: composer, Prof: prof, Pass: *pass, Echo: *echo,
			RNG: rand.New(rand.NewSource(*seed)),
			Log: func(e dialer.Event) { fmt.Printf("  · %-8s %s %s\n", e.Kind, e.Handle, e.Detail) }}
		fmt.Printf("dialing %s @ %s ...\n", pz.Handle, *addr)
		out := c.Dial(ctx, *addr)
		fmt.Printf("\noutcome: %+v\n", out)
		return
	}

	pool := &dialer.Pool{
		Addr: *addr, Personas: personas, Voice: composer, Prof: prof,
		Password: *pass, Echo: *echo, MaxConcurrent: *callers,
		MinGap: *minGap, MaxGap: *maxGap, Seed: *seed,
		Log: consoleFeed(),
	}
	fmt.Printf("carrier dialer → %s · %d personas · up to %d concurrent · voice=%s\n\n",
		*addr, len(personas), *callers, modelLabel(*voiceModel, client.Mock()))
	pool.Run(ctx, *duration)
	fmt.Println("\n" + pool.Summary())
}

func pick(ps []*domain.Persona, handle string) *domain.Persona {
	for _, p := range ps {
		if strings.EqualFold(p.Handle, handle) {
			return p
		}
	}
	return nil
}

// consoleFeed renders the live event stream with simple colored tags.
func consoleFeed() func(dialer.Event) {
	color := map[string]string{
		"dial": "36", "busy": "33", "login": "32", "register": "32",
		"read": "34", "post": "35", "reply": "35", "logoff": "90", "error": "31",
	}
	return func(e dialer.Event) {
		col := color[e.Kind]
		if col == "" {
			col = "37"
		}
		fmt.Printf("\x1b[%sm%-9s\x1b[0m \x1b[1m%-14s\x1b[0m %s\n", col, e.Kind, e.Handle, e.Detail)
	}
}

func modelLabel(m string, mock bool) string {
	if mock {
		return "mock (canned)"
	}
	return m
}
