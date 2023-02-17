package svcache

import (
	"context"
	"math"
	"math/rand"
	"sync/atomic"
	"time"
)

// NewBackoffUpdater returns an updater that uses a backoff strategy to determine how long to wait after an error before retrying.
func NewBackoffUpdater[V any](
	loader Loader[Timestamped[V]],
	backoffStrategy BackoffStrategy,
	errorHandler ErrorHandler[Timestamped[V]],
) Updater[Timestamped[V]] {
	updater, _ := newBackoffUpdater(systemClock{}, loader, backoffStrategy, errorHandler)
	return updater
}

// NewBackoffUpdater returns an updater that uses a backoff strategy to determine how long to wait after an error before retrying,
// and also returns a counter of the number of consecutive errors that have occurred.
func NewBackoffUpdaterAndConsecutiveErrorsCounter[V any](
	loader Loader[Timestamped[V]],
	backoffStrategy BackoffStrategy,
	errorHandler ErrorHandler[Timestamped[V]],
) (updater Updater[Timestamped[V]], consecutiveErrors *atomic.Uint32) {
	return newBackoffUpdater(systemClock{}, loader, backoffStrategy, errorHandler)
}

func newBackoffUpdater[V any](
	clock clock,
	loader Loader[Timestamped[V]],
	backoffStrategy BackoffStrategy,
	errorHandler ErrorHandler[Timestamped[V]],
) (updater Updater[Timestamped[V]], consecutiveErrors *atomic.Uint32) {
	consecutiveErrors = new(atomic.Uint32)
	var lastAttempt time.Time
	updater = func(ctx context.Context, previous Timestamped[V]) Timestamped[V] {
		currentConsecutiveErrors := consecutiveErrors.Load()
		if currentConsecutiveErrors > 0 {
			clock.Sleep(backoffStrategy(uint(currentConsecutiveErrors)) - clock.Since(lastAttempt))
		}
		lastAttempt = clock.Now()
		value, err := loader(ctx)
		if err != nil {
			currentConsecutiveErrors++
			errorHandler(ctx, previous, uint(currentConsecutiveErrors), err)
			return previous
		}
		consecutiveErrors.Store(0)
		return value
	}
	return
}

// BakcoffStrategy is a function that takes the number of consecutive errors that have occurred so far,
// and returns the minimum amount of time that should have passed since the previous attempt before retrying.
type BackoffStrategy func(consecutiveErrors uint) time.Duration

// NewBalancedBackoffStrategy returns a BackoffStrategy that
// exponentially scales between two values at the rate of the Fibonacci sequence with some jitter.
func NewBalancedBackoffStrategy(initial, final time.Duration) BackoffStrategy {
	appromiximateFibbonacciBase := 1.618033988749895
	jitterRatio := 0.05
	return NewClampedExponentialBackoffWithJitter(initial, appromiximateFibbonacciBase, final, jitterRatio)
}

// NewClampedExponentialBackoffWithJitter returns a BackoffStrategy that exponentially scales with the given base between two values.
//
// The jitter parameter is a fraction indicating how much random jitter can be added;
// e.g., if the jitter is 0.1 and the raw duration is 3 minutes, then the duration with jitter will be somewhere between 2min 42s and 3min 18s.
func NewClampedExponentialBackoffWithJitter(initial time.Duration, base float64, final time.Duration, jitter float64) BackoffStrategy {
	initialAsFloat := float64(initial)
	finalAsFloat := float64(final)
	return func(consecutiveErrors uint) time.Duration {
		var firstErrorWithNonZeroBackoff uint = 2
		if consecutiveErrors < firstErrorWithNonZeroBackoff {
			return 0
		}
		raw := math.Pow(base, float64(consecutiveErrors-firstErrorWithNonZeroBackoff)) * initialAsFloat
		clamped := math.Min(raw, finalAsFloat)
		withJitter := clamped * (1 + jitter*(2*rand.Float64()-1)) //nolint:gosec
		return time.Duration(withJitter)
	}
}

// ErrorHandler is a function that is called when an error occurs while loading a value, which can be used e.g. for logging.
type ErrorHandler[V any] func(ctx context.Context, previous V, consecutiveErrors uint, err error)
