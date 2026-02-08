package ratelimit

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// --------------------------------------------------------------- STRATEGY PATTERN + Open/Closed Principle  ---------------------------------------------------------------

var ErrRateLimitExceeded = errors.New("rate limit exceeded")

// The Strategy Interface
// This defines the "Contract". Any algorithm (Fixed Window, Token Bucket, Leaky Bucket)
type Limiter interface {
	Allow(ctx context.Context, userID string) (bool, error)
}

// ----> Concrete Strategy A: Fixed Window
// It's simple but has "burst" issues at the edges of the window.
type FixedWindowLimiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

// Factory for Fixed Window
// return the Interface `Limiter`, not the struct `*FixedWindowLimiter`.
// This allows us to swap this out later without breaking the main code.
func NewFixedWindowLimiter(rdb *redis.Client, limit int, window time.Duration) Limiter {
	return &FixedWindowLimiter{
		rdb:    rdb,
		limit:  limit,
		window: window,
	}
}

func (l *FixedWindowLimiter) Allow(ctx context.Context, userID string) (bool, error) {
	key := "rate_limit:" + userID

	// Increment
	count, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}

	// Set expiry only on the first request
	if count == 1 {
		l.rdb.Expire(ctx, key, l.window)
	}

	// Check Limit
	if count > int64(l.limit) {
		return false, nil
	}
	return true, nil
}

// ----> Concrete Strategy B: No-Op (Null Object Pattern)
// Useful for local development or for "Super Admins" who are never blocked.
type NoOpLimiter struct{}

func NewNoOpLimiter() Limiter {
	return &NoOpLimiter{}
}

func (l *NoOpLimiter) Allow(ctx context.Context, userID string) (bool, error) {
	// Always allow, never error
	return true, nil
}
