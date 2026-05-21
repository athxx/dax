// Package ratelimit provides token-bucket rate limiting middleware.
//
// Usage:
//
//	s.Use(ratelimit.New(ratelimit.Config{
//	    Rate:  100,       // requests per second
//	    Burst: 50,        // burst size
//	    Key:   ratelimit.ByIP,
//	}))
package ratelimit

import (
	"sync"
	"time"

	"github.com/athxx/dax"
)

// KeyFunc defines how to extract the rate limit key from a context.
type KeyFunc func(dax.Context) string

// Predefined key functions.
var (
	// ByIP uses the client's remote address as the rate limit key.
	ByIP KeyFunc = func(ctx dax.Context) string {
		return ctx.Request().Header("X-Forwarded-For")
	}

	// ByPath uses the request path as the rate limit key.
	ByPath KeyFunc = func(ctx dax.Context) string {
		return ctx.Request().Path()
	}
)

// Config holds the rate limiter configuration.
type Config struct {
	// Rate is the maximum number of requests per second.
	Rate float64
	// Burst is the maximum burst size.
	Burst int
	// Key extracts the rate limit key from the context.
	Key KeyFunc
}

var defaultConfig = Config{
	Rate:  100,
	Burst: 50,
	Key:   ByIP,
}

// New creates rate limiter middleware.
func New(config ...Config) dax.Handler {
	cfg := defaultConfig
	if len(config) > 0 {
		c := config[0]
		if c.Rate != 0 {
			cfg.Rate = c.Rate
		}
		if c.Burst != 0 {
			cfg.Burst = c.Burst
		}
		if c.Key != nil {
			cfg.Key = c.Key
		}
	}

	rl := &rateLimiter{
		rate:    cfg.Rate,
		burst:   float64(cfg.Burst),
		buckets: make(map[string]*bucket),
		keyFunc: cfg.Key,
	}

	return func(ctx dax.Context) error {
		key := rl.keyFunc(ctx)
		if key == "" {
			key = ctx.Request().Path()
		}

		if !rl.allow(key) {
			ctx.Response().SetHeader("Retry-After", "1")
			ctx.Response().SetStatus(429)
			ctx.Response().SetBody([]byte("Too Many Requests"))
			return nil
		}

		return ctx.Next(ctx)
	}
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*bucket
	keyFunc KeyFunc
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: rl.burst, lastCheck: now}
		rl.buckets[key] = b
	}

	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastCheck = now

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}
