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
			// Use the RIGHTMOST entry: behind a single trusted proxy (the ALB),
			// the last hop is the address the ALB observed and appended. The
			// leftmost entries are client-supplied and trivially spoofable, so
			// trusting them lets an attacker rotate fake IPs to evade per-IP
			// rate limits.
			parts := strings.Split(forwardedFor, ",")
			if candidate := normalizedClientIP(parts[len(parts)-1]); candidate != "" {
				return candidate
			}
		}
		if realIP := normalizedClientIP(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if candidate := normalizedClientIP(r.RemoteAddr); candidate != "" {
			return candidate
		}
		return "unknown"
	}
	return host
}

func normalizedClientIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	return ""
}

func osTrustProxyHeaders() bool {
	return strings.EqualFold(strings.TrimSpace(getEnvDefault("TRUST_PROXY_HEADERS", "")), "1")
}
