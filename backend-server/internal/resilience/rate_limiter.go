package resilience

import (
	"context"

	"golang.org/x/time/rate"
)

// RateLimiter is a thin wrapper around rate.Limiter to allow swapping implementations.
type RateLimiter struct {
	limiter *rate.Limiter
}

// NewRateLimiter creates a limiter for the provided throughput.
func NewRateLimiter(rps, burst int) *RateLimiter {
	if rps <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = rps
	}
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	if rl == nil || rl.limiter == nil {
		return nil
	}
	return rl.limiter.Wait(ctx)
}
