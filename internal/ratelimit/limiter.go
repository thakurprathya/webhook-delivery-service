package ratelimit

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Sentinel Error Pattern: Package-level error variable allows callers to check error identity with errors.Is(), rather than string comparison.
var ErrRateLimitExceeded = errors.New("rate limit exceeded")

// Strategy Pattern (Interface): Defines a family of algorithms (Fixed Window, Token Bucket, Leaky Bucket) and makes them interchangeable.
// Dependency Inversion Principle: Callers depend on this abstraction, not on any specific algorithm.
type Limiter interface {
	Allow(ctx context.Context, userID string) (bool, error)
}

// ----> Concrete Strategy A: Fixed Window Rate Limiter
// It's simple but has "burst" issues at the edges of the window.
// Composition over Inheritance: Embeds redis.Client as a dependency, not via inheritance.
type FixedWindowLimiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

// Factory Function Pattern: Encapsulates construction complexity.
// Open/Closed Principle (OCP): Returns the Limiter interface.
// Adding a new strategy (e.g., TokenBucketLimiter) requires zero changes to existing code — just a new struct + factory.
func NewFixedWindowLimiter(rdb *redis.Client, limit int, window time.Duration) Limiter {
	return &FixedWindowLimiter{
		rdb:    rdb,
		limit:  limit,
		window: window,
	}
}

// Atomic Counter Pattern: Uses Redis INCR for thread-safe, distributed counting.
// This works correctly even with multiple API server replicas.
func (l *FixedWindowLimiter) Allow(ctx context.Context, userID string) (bool, error) {
	key := "rate_limit:" + userID

	// Atomic Increment: INCR is a single Redis command — no race conditions.
	count, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}

	// Lazy Expiration: Only set TTL on the first request of each window.
	// This starts the "clock" for the rate limit window.
	if count == 1 {
		l.rdb.Expire(ctx, key, l.window)
	}

	// Check Limit
	if count > int64(l.limit) {
		return false, nil
	}
	return true, nil
}

// ----> Concrete Strategy B: No-Op Limiter (Null Object Pattern)
// Null Object Pattern: Eliminates nil checks. Instead of "if limiter != nil",
// we use a limiter that always says "yes". Useful for development, testing, or "super admin" bypass scenarios.
type NoOpLimiter struct{}

// Factory Function Pattern: Returns the Limiter interface.
func NewNoOpLimiter() Limiter {
	return &NoOpLimiter{}
}

// Null Object Behavior: Always permits, never errors.
func (l *NoOpLimiter) Allow(ctx context.Context, userID string) (bool, error) {
	return true, nil
}
