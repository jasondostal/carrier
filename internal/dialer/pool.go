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
	MaxConcurrent int // client-side cap on simultaneous calls

	// Time model — two orthogonal, wide knobs:
	//   DayLength   = HOW FAST: wall-clock for one simulated day (24h = real time,
	//                 10m = a day every ten minutes, 30s = fast-forward).
	//   CallsPerDay = HOW MUCH: average calls per caller per simulated day, scaled
	//                 by each persona's call-urge (a real heavy user ~a handful/day).
	//   Chattiness  = of those calls, the fraction that actually post/reply rather
	//                 than just read and hang up (real callers mostly lurk).
	DayLength   time.Duration
	CallsPerDay float64
	Chattiness  float64

	Seed int64
	Log  func(Event)

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
	if p.DayLength <= 0 {
		p.DayLength = 10 * time.Minute
	}
	if p.CallsPerDay <= 0 {
		p.CallsPerDay = 4
	}
	if p.Chattiness <= 0 {
		p.Chattiness = 0.6
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
			// stagger startups across a typical gap so they don't all dial at once
			sleep(runCtx, time.Duration(rng.Float64()*float64(p.gap(persona, rng))))
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
		Pass: p.Password, Echo: p.Echo, RNG: rng, Chattiness: p.Chattiness, Log: p.Log,
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
		// Busy: wait a jittered backoff and redial, like a real caller hitting a
		// busy signal and hitting redial. Backoff scales with the pace: a short
		// slice of a simulated day (~1/500th), clamped to something sane.
		back := time.Duration(float64(p.DayLength) / 500.0)
		back = clampDur(back, 500*time.Millisecond, 20*time.Second)
		back += time.Duration(rng.Int63n(int64(back) + 1))
		if !sleep(ctx, back) {
			return
		}
	}
}

// gap returns the wait until this persona's next call attempt, derived from the
// time model: mean gap = DayLength / (CallsPerDay scaled by call-urge), then
// jittered 0.5×–1.5×. Higher-urge personas dial more often.
func (p *Pool) gap(persona *domain.Persona, rng *rand.Rand) time.Duration {
	urge := persona.CallUrge
	if urge <= 0 {
		urge = 0.4
	}
	if urge > 1 {
		urge = 1
	}
	// A heavy (urge≈1) caller dials ~1.5× the base rate; a lurker ~0.5×.
	calls := p.CallsPerDay * (0.5 + urge)
	if calls < 0.1 {
		calls = 0.1
	}
	mean := float64(p.DayLength) / calls
	d := time.Duration(mean * (0.5 + rng.Float64())) // 0.5×–1.5× jitter
	return clampDur(d, 300*time.Millisecond, 24*time.Hour)
}

// clampDur bounds d to [lo, hi].
func clampDur(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
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
