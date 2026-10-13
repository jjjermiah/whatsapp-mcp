package main

// Outbound send pacing. Bursts of sends to people who have never heard from
// the account look like spam to WhatsApp; WHATSAPP_SEND_MIN_INTERVAL_MS makes
// the bridge space them out. Callers wait their turn instead of being
// rejected, so a bulk sender only sees slower responses.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sendPacer struct {
	mu          sync.Mutex
	minInterval time.Duration
	last        time.Time
	now         func() time.Time
	sleep       func(time.Duration)
}

func newSendPacer(minInterval time.Duration) *sendPacer {
	return &sendPacer{minInterval: minInterval, now: time.Now, sleep: time.Sleep}
}

// newSendPacerFromEnv reads WHATSAPP_SEND_MIN_INTERVAL_MS (milliseconds,
// default 0 = no pacing). An unusable value disables pacing with a warning.
func newSendPacerFromEnv() *sendPacer {
	raw := strings.TrimSpace(os.Getenv("WHATSAPP_SEND_MIN_INTERVAL_MS"))
	if raw == "" {
		return newSendPacer(0)
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		fmt.Printf("Warning: ignoring invalid WHATSAPP_SEND_MIN_INTERVAL_MS=%q (want a non-negative integer)\n", raw)
		return newSendPacer(0)
	}
	return newSendPacer(time.Duration(ms) * time.Millisecond)
}

// Wait blocks until at least minInterval has passed since the previous send
// slot was granted. The lock is held while sleeping, so concurrent callers
// queue up and are released one interval apart.
func (p *sendPacer) Wait() {
	if p.minInterval <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.last.IsZero() {
		if d := p.minInterval - p.now().Sub(p.last); d > 0 {
			p.sleep(d)
		}
	}
	p.last = p.now()
}

// outboundPacer paces every outbound text, media and audio send.
var outboundPacer = newSendPacerFromEnv()
