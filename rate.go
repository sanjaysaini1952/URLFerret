package main

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu         sync.Mutex
	tokens     int
	maxTokens  int
	interval   time.Duration
	lastRefill time.Time
}

func newRateLimiter(max int, interval time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens:     max,
		maxTokens:  max,
		interval:   interval,
		lastRefill: time.Now(),
	}
}

func (rl *rateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	rl.lastRefill = now

	// Refill tokens based on elapsed time
	rl.tokens += int(elapsed / rl.interval)
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}
	return false
}

type multiRateLimiter struct {
	global     *rateLimiter
	globalMin  *rateLimiter
	hostLimit  map[string]*rateLimiter
	hostMinute map[string]*rateLimiter
	mu         sync.RWMutex
}

func newMultiRateLimiter(perSec, perMin, hostPerSec, hostPerMin int) *multiRateLimiter {
	m := &multiRateLimiter{
		hostLimit:  make(map[string]*rateLimiter),
		hostMinute: make(map[string]*rateLimiter),
	}
	if perSec > 0 {
		m.global = newRateLimiter(perSec, time.Second)
	}
	if perMin > 0 {
		m.globalMin = newRateLimiter(perMin, time.Minute)
	}
	if hostPerSec > 0 {
		m.hostLimit = make(map[string]*rateLimiter)
	}
	if hostPerMin > 0 {
		m.hostMinute = make(map[string]*rateLimiter)
	}
	return m
}

func (m *multiRateLimiter) Allow(host string) bool {
	if m.global != nil && !m.global.Allow() {
		return false
	}
	if m.globalMin != nil && !m.globalMin.Allow() {
		return false
	}

	if m.hostLimit != nil {
		m.mu.RLock()
		rl, ok := m.hostLimit[host]
		m.mu.RUnlock()
		if !ok {
			rl = newRateLimiter(cfgForHostLimit, time.Second) // default 150
			m.mu.Lock()
			m.hostLimit[host] = rl
			m.mu.Unlock()
		}
		if !rl.Allow() {
			return false
		}
	}

	return true
}

var cfgForHostLimit int
