package svcache

import (
	"context"
	"time"
)

// Timestamped augments a value with a timestamp.
type Timestamped[V any] struct {
	Value     V
	Timestamp time.Time
}

// NewTimestampedLoader creates a loader that automatically wraps values with a timestamp derived from the time loading started.
// This is useful for determining whether to trigger or wait for a new value to be loaded.
// Alternatively, the value you are loading may have its own timestamp, in which case you can use that instead of using this utility.
func NewTimestampedLoader[V any](loader Loader[V]) Loader[Timestamped[V]] {
	return newTimestampedLoader(systemClock{}, loader)
}

func newTimestampedLoader[V any](clock clock, loader Loader[V]) Loader[Timestamped[V]] {
	return func(ctx context.Context) (Timestamped[V], error) {
		start := clock.Now()
		value, err := loader(ctx)
		return Timestamped[V]{value, start}, err
	}
}

// WaitForNonZeroTimestamp returns a AccessStrategy for Timestamped values that waits for the timestamp to be non-zero,
// then uses the given strategy.
func WaitForNonZeroTimestamp[V any](nonBlockingStrategy RefreshStrategy[Timestamped[V]]) AccessStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) Action {
		if current.Timestamp.IsZero() {
			return WaitForNewlyLoadedValue
		}
		return nonBlockingAction(nonBlockingStrategy, current)
	}
}

// TriggerIfAged returns a AccessStrategy for Timestamped values that never waits
// but triggers a refresh if the value's age is at least the given duration.
func TriggerIfAged[V any](threshold time.Duration) RefreshStrategy[Timestamped[V]] {
	return triggerIfAged[V](systemClock{}, threshold)
}

func triggerIfAged[V any](clock clock, threshold time.Duration) RefreshStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) bool {
		return clock.Since(current.Timestamp) >= threshold
	}
}

// TriggerUnlessNewerThan returns a AccessStrategy for Timestamped values that never waits
// but triggers a refresh unless the value's timestamp is newer than the given time.
func TriggerUnlessNewerThan[V any](threshold time.Time) RefreshStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) bool {
		return !current.Timestamp.After(threshold)
	}
}

// TriggerOrWaitIfAged returns a AccessStrategy for Timestamped values that waits if the value's age is at least the waitThreashold,
// and triggers a refresh if the value's age is at least the triggerThreshold.
func TriggerOrWaitIfAgedWaitIfAged[V any](triggerThreshold, waitThreshold time.Duration) AccessStrategy[Timestamped[V]] {
	return triggerOrWaitIfAged[V](systemClock{}, triggerThreshold, waitThreshold)
}

func triggerOrWaitIfAged[V any](clock clock, triggerThreshold, waitThreshold time.Duration) AccessStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) Action {
		elapsed := clock.Since(current.Timestamp)
		if elapsed >= waitThreshold {
			return WaitForNewlyLoadedValue
		}
		if elapsed >= triggerThreshold {
			return TriggerLoadAndUseCachedValue
		}
		return UseCachedValue
	}
}

// TriggerOrWaitUnlessNewThan returns a AccessStrategy for Timestamped values that waits unless the value is newer than the waitThreshold,
// and triggers a refresh unless the value's timestamp is newer than the given time.
func TriggerOrWaitUnlessNewerThan[V any](triggerThreshold, waitThreshold time.Time) AccessStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) Action {
		if !current.Timestamp.After(waitThreshold) {
			return TriggerLoadAndUseCachedValue
		}
		if !current.Timestamp.After(triggerThreshold) {
			return TriggerLoadAndUseCachedValue
		}
		return UseCachedValue
	}
}

// WaitIfAged returns a AccessStrategy for Timestamped values that waits if the value's age is at least the waitThreashold,
// and otherwise uses the given non-blocking strategy to determine whether to trigger a reload.
func WaitIfAged[V any](threshold time.Duration, nonBlockingStrategy RefreshStrategy[Timestamped[V]]) AccessStrategy[Timestamped[V]] {
	return waitIfAged(systemClock{}, threshold, nonBlockingStrategy)
}

func waitIfAged[V any](
	clock clock,
	threshold time.Duration,
	nonBlockingStrategy RefreshStrategy[Timestamped[V]],
) AccessStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) Action {
		elapsed := clock.Since(current.Timestamp)
		if elapsed >= threshold {
			return WaitForNewlyLoadedValue
		}
		return nonBlockingAction(nonBlockingStrategy, current)
	}
}

// WaitUnlessNewThan returns a AccessStrategy for Timestamped values that waits unless the value is newer than the waitThreshold,
// and otherwise uses the given non-blocking strategy to determine whether to trigger a reload.
func WaitUnlessNewerThan[V any](threshold time.Time, nonBlockingStrategy RefreshStrategy[Timestamped[V]]) AccessStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) Action {
		if !current.Timestamp.After(threshold) {
			return TriggerLoadAndUseCachedValue
		}
		return nonBlockingAction(nonBlockingStrategy, current)
	}
}
