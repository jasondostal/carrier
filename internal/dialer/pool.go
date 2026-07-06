package dialer

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/jasondostal/carrier/internal/domain"
	"github.com/jasondostal/carrier/internal/voice"
)

// Pool is the traffic simulator: a population of persona callers that dial the
// target board on their own cadence (weighted by call-urge), redial when they
// hit a busy signal, and generate real read/post/reply traffic. It's the mock
// "load of callers" a sysop would use to exercise a board.
type Pool struct {
	Addr          string
	Personas      []*domain.Persona
	Voice         voice.Composer
	Prof          *Profile
	Password      string
	Echo          string
	MaxConcurrent int           // client-side cap on simultaneous calls
	MinGap, MaxGap time.Duration // per-persona wait between call attempts
	BusyBackoff   time.Duration  // base wait before redialing a busy line
	Seed          int64
	Log           func(Event)

	sem  chan struct{}
	mu   sync.Mutex
	stat Stats
}

// Stats accumulates what the run produced.
type Stats struct {
	Calls, Busies, Retries         int
	Logins, Registrations          int
	Reads, Posts, Replies, Errors  int
}

// Run launches one goroutine per persona and blocks until duration elapses (0 =
// until ctx is cancelled). Each persona loops: wait its cadence, dial, and on a
// busy signal redial after a short backoff — the retry-on-busy behavior at the
// heart of the simulator.
func (p *Pool) Run(ctx context.Context, duration time.Duration) {
	if p.MaxConcurrent <= 0 {
		p.MaxConcurrent = len(p.Personas)
	}
	if p.BusyBackoff == 0 {
		p.BusyBackoff = 3 * time.Second
	}
	p.sem = make(chan struct{}, p.MaxConcurrent)

	runCtx := ctx
	if duration > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}

	var wg sync.WaitGroup
	for idx, persona := range p.Personas {
		wg.Add(1)
		go func(idx int, persona *domain.Persona) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(p.Seed + int64(idx)*7919))
			// stagger startups so they don't all dial on tick zero
			sleep(runCtx, time.Duration(rng.Int63n(int64(p.MaxGap)+1)))
			for runCtx.Err() == nil {
				p.attempt(runCtx, persona, rng)
				if !sleep(runCtx, p.gap(persona, rng)) {
					return
				}
			}
		}(idx, persona)
	}
	wg.Wait()
}

// attempt makes one "call intent": dial, and if the line is busy, redial a few
// times with backoff before giving up until the next cadence tick.
func (p *Pool) attempt(ctx context.Context, persona *domain.Persona, rng *rand.Rand) {
	c := &Caller{
		Persona: persona, Voice: p.Voice, Prof: p.Prof,
		Pass: p.Password, Echo: p.Echo, RNG: rng, Log: p.Log,
	}
	const maxRedial = 5
	for redial := 0; redial <= maxRedial; redial++ {
		select {
		case p.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		out := c.Dial(ctx, p.Addr)
		<-p.sem

		p.record(out, redial > 0)
		if !out.Busy {
			return // connected (or hard error) — done with this intent
		}
		// Busy: wait a jittered backoff and redial, like a real caller hitting
		// a busy signal and hitting redial.
		back := p.BusyBackoff + time.Duration(rng.Int63n(int64(p.BusyBackoff)))
		if !sleep(ctx, back) {
			return
		}
	}
}

// gap returns the wait until this persona's next call attempt, shorter for
// higher call-urge personas, with jitter.
func (p *Pool) gap(persona *domain.Persona, rng *rand.Rand) time.Duration {
	urge := persona.CallUrge
	if urge <= 0 {
		urge = 0.4
	}
	if urge > 1 {
		urge = 1
	}
	// lerp from MaxGap (low urge) toward MinGap (high urge)
	span := float64(p.MaxGap - p.MinGap)
	base := float64(p.MaxGap) - span*urge
	jitter := rng.Float64()*span*0.4 - span*0.2
	d := time.Duration(base + jitter)
	if d < p.MinGap {
		d = p.MinGap
	}
	return d
}

func (p *Pool) record(out Outcome, wasRedial bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stat.Calls++
	if wasRedial {
		p.stat.Retries++
	}
	switch {
	case out.Busy:
		p.stat.Busies++
	case out.Err != nil:
		p.stat.Errors++
	}
	if out.Registered {
		p.stat.Registrations++
	} else if out.Err == nil && !out.Busy {
		p.stat.Logins++
	}
	p.stat.Reads += out.Read
	p.stat.Posts += out.Posted
	p.stat.Replies += out.Replied
}

// Summary renders the accumulated stats.
func (p *Pool) Summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stat
	return fmt.Sprintf(
		"── run summary ──\n"+
			"calls:        %d  (busy: %d, retries: %d, errors: %d)\n"+
			"logins:       %d   registrations: %d\n"+
			"reads:        %d   posts: %d   replies: %d",
		s.Calls, s.Busies, s.Retries, s.Errors,
		s.Logins, s.Registrations, s.Reads, s.Posts, s.Replies)
}

// sleep waits d or until ctx is done; returns false if the context ended.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
