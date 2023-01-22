package svcache

import (
	"context"
	"math"
	"math/rand"
	"time"
)

func NewBackoffUpdater[V any](
	loader Loader[Timestamped[V]],
	backoffStrategy BackoffStrategy,
	errorHandler ErrorHandler[Timestamped[V]],
) Updater[Timestamped[V]] {
	return newBackoffUpdater(systemClock{}, loader, backoffStrategy, errorHandler)
}

func newBackoffUpdater[V any](
	clock clock,
	loader Loader[Timestamped[V]],
	backoffStrategy BackoffStrategy,
	errorHandler ErrorHandler[Timestamped[V]],
) Updater[Timestamped[V]] {
	consecutiveErrors := uint(0)
	var lastAttempt time.Time
	return func(ctx context.Context, previous Timestamped[V]) Timestamped[V] {
		if consecutiveErrors > 0 {
			clock.Sleep(backoffStrategy(consecutiveErrors) - clock.Since(lastAttempt))
		}
		lastAttempt = clock.Now()
		value, err := loader(ctx)
		if err != nil {
			consecutiveErrors++
			errorHandler(ctx, previous, consecutiveErrors, err)
			return previous
		}
		consecutiveErrors = 0
		return value
	}
}

type BackoffStrategy func(consecutiveErrors uint) time.Duration

func NewBalancedBackoffStrategy(initial, final time.Duration) BackoffStrategy {
	appromiximateFibbonacciBase := 1.618033988749895
	jitterRatio := 0.05
	return NewClampedExponentialBackoffWithJitter(initial, appromiximateFibbonacciBase, final, jitterRatio)
}

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

type ErrorHandler[V any] func(ctx context.Context, previous V, consecutiveErrors uint, err error)
