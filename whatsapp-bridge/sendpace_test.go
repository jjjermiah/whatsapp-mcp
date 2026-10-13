package main

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a sendPacer clock whose sleep advances time instantly and
// records what it was asked to wait.
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newFakePacer(interval time.Duration) (*sendPacer, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	p := newSendPacer(interval)
	p.now, p.sleep = clock.now, clock.sleep
	return p, clock
}

func TestSendPacer_DisabledNeverWaits(t *testing.T) {
	p, clock := newFakePacer(0)
	for i := 0; i < 3; i++ {
		p.Wait()
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("pacing disabled but slept %v", clock.sleeps)
	}
}

func TestSendPacer_FirstSendIsImmediateThenSpaced(t *testing.T) {
	p, clock := newFakePacer(10 * time.Second)
	p.Wait()
	if len(clock.sleeps) != 0 {
		t.Fatalf("first send should not wait, slept %v", clock.sleeps)
	}
	p.Wait()
	p.Wait()
	want := []time.Duration{10 * time.Second, 10 * time.Second}
	if len(clock.sleeps) != len(want) || clock.sleeps[0] != want[0] || clock.sleeps[1] != want[1] {
		t.Fatalf("sleeps = %v, want %v", clock.sleeps, want)
	}
}

func TestSendPacer_OnlyWaitsForTheRemainder(t *testing.T) {
	p, clock := newFakePacer(10 * time.Second)
	p.Wait()
	clock.advance(4 * time.Second)
	p.Wait()
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 6*time.Second {
		t.Fatalf("sleeps = %v, want [6s]", clock.sleeps)
	}
	clock.advance(11 * time.Second)
	p.Wait()
	if len(clock.sleeps) != 1 {
		t.Fatalf("an already-elapsed interval must not wait, sleeps = %v", clock.sleeps)
	}
}

// Concurrent callers queue behind each other: with N callers the pacer sleeps
// N-1 times, never rejecting anyone.
func TestSendPacer_ConcurrentCallersAreSerialized(t *testing.T) {
	p, clock := newFakePacer(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Wait()
		}()
	}
	wg.Wait()
	if len(clock.sleeps) != 4 {
		t.Fatalf("sleeps = %v, want 4 sleeps for 5 callers", clock.sleeps)
	}
	if got := clock.now().Sub(time.Unix(1_700_000_000, 0)); got != 4*time.Second {
		t.Fatalf("total elapsed = %v, want 4s", got)
	}
}

func TestSendPacer_RealClockEnforcesInterval(t *testing.T) {
	p := newSendPacer(40 * time.Millisecond)
	start := time.Now()
	p.Wait()
	p.Wait()
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("two sends took %v, want >= 40ms", elapsed)
	}
}

func TestNewSendPacerFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"15000", 15 * time.Second},
		{" 250 ", 250 * time.Millisecond},
		{"-5", 0},
		{"soon", 0},
	}
	for _, tc := range cases {
		t.Setenv("WHATSAPP_SEND_MIN_INTERVAL_MS", tc.env)
		if got := newSendPacerFromEnv().minInterval; got != tc.want {
			t.Errorf("WHATSAPP_SEND_MIN_INTERVAL_MS=%q: interval = %v, want %v", tc.env, got, tc.want)
		}
	}
}
