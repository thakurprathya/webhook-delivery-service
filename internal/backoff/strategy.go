package backoff

import (
	"math"
	"time"
)

// Strategy Pattern (Interface): Defines a family of backoff algorithms (Exponential, Linear, Constant) and makes them interchangeable at runtime.
// Single Responsibility Principle (SRP): This package ONLY handles retry timing. It knows nothing about queues, HTTP calls, or Redis.
type Strategy interface {
	GetNextInterval(retryCount int) time.Duration
}

// ----> Concrete Strategy A: Exponential Backoff
// Prevents "Thundering Herd" problem: If 1000 tasks fail simultaneously,
// exponential backoff spreads retries over time instead of hammering the destination server with 1000 simultaneous retries.
type ExponentialStrategy struct {
	Base   time.Duration // Starting wait time (e.g., 1s)
	Factor float64       // Multiplier (e.g., 2.0)
	Max    time.Duration // Cap (e.g., max 1 hour)
}

// Factory Function Pattern: Encapsulates object creation.
// Open/Closed Principle (OCP): Returns the Strategy interface, not *ExponentialStrategy.
// This allows us to swap this out later without breaking the main code.
func NewExponentialStrategy(base time.Duration, factor float64, max time.Duration) Strategy {
	return &ExponentialStrategy{
		Base:   base,
		Factor: factor,
		Max:    max,
	}
}

// GetNextInterval calculates: Base * (Factor ^ RetryCount)
// e.g., 1s, 2s, 4s, 8s, 16s, 32s... capped at Max.
// Defensive Programming: The Max cap prevents unbounded growth
func (s *ExponentialStrategy) GetNextInterval(retryCount int) time.Duration {
	// Standard formula: 2^retry
	multiplier := math.Pow(s.Factor, float64(retryCount))
	interval := time.Duration(float64(s.Base) * multiplier)

	// Guard Clause Pattern: Return early with bounded value.
	if interval > s.Max {
		return s.Max
	}
	return interval
}
