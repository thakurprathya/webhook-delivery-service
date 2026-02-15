package backoff

import (
	"math"
	"time"
)

// (Strategy Pattern)
type Strategy interface {
	GetNextInterval(retryCount int) time.Duration
}

// ExponentialStrategy implements the interface
type ExponentialStrategy struct {
	Base   time.Duration // Starting wait time (e.g., 1s)
	Factor float64       // Multiplier (e.g., 2.0)
	Max    time.Duration // Cap (e.g., max 1 hour)
}

func NewExponentialStrategy(base time.Duration, factor float64, max time.Duration) *ExponentialStrategy {
	return &ExponentialStrategy{
		Base:   base,
		Factor: factor,
		Max:    max,
	}
}

// GetNextInterval calculates: Base * (Factor ^ RetryCount)
func (s *ExponentialStrategy) GetNextInterval(retryCount int) time.Duration {
	// Standard formula: 2^retry
	multiplier := math.Pow(s.Factor, float64(retryCount))
	interval := time.Duration(float64(s.Base) * multiplier)

	if interval > s.Max {
		return s.Max
	}
	return interval
}
