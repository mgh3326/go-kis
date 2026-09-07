package ws

import (
	"math"
	"math/rand/v2"
	"time"
)

// Default reconnect pacing, used for any BackoffConfig member left at zero.
const (
	defaultBackoffMin    = 500 * time.Millisecond
	defaultBackoffMax    = 30 * time.Second
	defaultBackoffFactor = 2.0
	defaultBackoffJitter = 0.2
)

// BackoffConfig paces reconnect attempts. The delay grows by Factor from Min
// and is always capped at Max, so a long outage settles into a steady retry
// rate instead of drifting towards never retrying.
type BackoffConfig struct {
	// Min is the delay before the first retry. Defaults to 500ms.
	Min time.Duration
	// Max is the hard ceiling on any delay. Defaults to 30s and is never
	// unbounded.
	Max time.Duration
	// Factor multiplies the delay after each failed attempt. Defaults to 2.
	Factor float64
	// Jitter is the fraction of the delay, in [0,1], randomised away to keep
	// many clients from retrying in lockstep. Defaults to 0.2. Set it to a
	// negative value for exactly reproducible delays in tests.
	Jitter float64
}

func (b BackoffConfig) normalized() BackoffConfig {
	if b.Min <= 0 {
		b.Min = defaultBackoffMin
	}
	if b.Max <= 0 {
		b.Max = defaultBackoffMax
	}
	if b.Factor < 1 {
		b.Factor = defaultBackoffFactor
	}
	if b.Jitter == 0 {
		b.Jitter = defaultBackoffJitter
	}
	if b.Jitter > 1 {
		b.Jitter = 1
	}
	if b.Max < b.Min {
		b.Max = b.Min
	}
	return b
}

// delay returns the wait before attempt n, counting from 1.
func (b BackoffConfig) delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	grown := float64(b.Min) * math.Pow(b.Factor, float64(attempt-1))
	if grown > float64(b.Max) || math.IsInf(grown, 0) {
		grown = float64(b.Max)
	}
	if b.Jitter > 0 {
		grown -= grown * b.Jitter * rand.Float64()
	}
	if grown < float64(b.Min) {
		grown = float64(b.Min)
	}
	return time.Duration(grown)
}
