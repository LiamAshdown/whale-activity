package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter implements a token bucket rate limiter
type Limiter struct {
	rate       float64 // tokens per second
	tokens     float64
	maxTokens  float64
	lastUpdate time.Time
	mu         sync.Mutex
}

// New creates a new rate limiter with the specified rate (requests per second)
func New(rps float64) *Limiter {
	if rps <= 0 {
		rps = 1.0
	}
	return &Limiter{
		rate:       rps,
		tokens:     rps,
		maxTokens:  rps,
		lastUpdate: time.Now(),
	}
}

// Wait blocks until a token is available or context is cancelled
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		if l.tryTake() {
			return nil
		}

		// Calculate wait time
		waitTime := time.Duration(float64(time.Second) / l.rate)
		
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Continue loop
		}
	}
}

func (l *Limiter) tryTake() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.lastUpdate).Seconds()

	// Add tokens based on elapsed time
	l.tokens += elapsed * l.rate
	if l.tokens > l.maxTokens {
		l.tokens = l.maxTokens
	}

	l.lastUpdate = now

	// Try to take a token
	if l.tokens >= 1.0 {
		l.tokens -= 1.0
		return true
	}

	return false
}
