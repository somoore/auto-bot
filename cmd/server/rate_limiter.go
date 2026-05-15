package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type fixedWindowRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]rateLimitWindow
}

type rateLimitWindow struct {
	start time.Time
	count int
}

func newFixedWindowRateLimiter(limit int, window time.Duration) *fixedWindowRateLimiter {
	return &fixedWindowRateLimiter{
		limit:   limit,
		window:  window,
		clients: map[string]rateLimitWindow{},
	}
}

func (limiter *fixedWindowRateLimiter) Allow(key string) bool {
	if limiter == nil || limiter.limit <= 0 {
		return true
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}

	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	current := limiter.clients[key]
	if current.start.IsZero() || now.Sub(current.start) >= limiter.window {
		limiter.clients[key] = rateLimitWindow{start: now, count: 1}
		limiter.cleanupLocked(now)
		return true
	}

	if current.count >= limiter.limit {
		return false
	}
	current.count++
	limiter.clients[key] = current
	return true
}

func (limiter *fixedWindowRateLimiter) cleanupLocked(now time.Time) {
	for key, current := range limiter.clients {
		if now.Sub(current.start) > 2*limiter.window {
			delete(limiter.clients, key)
		}
	}
}

func clientAddress(r *http.Request) string {
	if osTrustProxyHeaders() {
		if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
			parts := strings.Split(forwardedFor, ",")
			if candidate := strings.TrimSpace(parts[0]); candidate != "" {
				return candidate
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func osTrustProxyHeaders() bool {
	return strings.EqualFold(strings.TrimSpace(getEnvDefault("TRUST_PROXY_HEADERS", "")), "1")
}
