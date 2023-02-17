package svcache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

//nolint:funlen
func TestGivingUpAfterTooManyErrors(t *testing.T) {
	t.Parallel()

	clock := newManualClock()

	type loadResult struct {
		value int
		err   error
	}
	loadableResults := make(chan loadResult)

	type loadError struct {
		previous          Timestamped[int]
		consecutiveErrors uint
		err               error
	}
	loadErrors := make(chan loadError, 1)

	updater, consecutiveErrors := newBackoffUpdater(
		clock,
		func(ctx context.Context) (Timestamped[int], error) {
			result := <-loadableResults
			return Timestamped[int]{result.value, clock.Now()}, result.err
		},
		NewBalancedBackoffStrategy(time.Second, time.Minute),
		func(ctx context.Context, previous Timestamped[int], consecutiveErrors uint, err error) {
			loadErrors <- loadError{previous, consecutiveErrors, err}
		},
	)
	cache := NewInMemory(context.Background(), updater)
	accessStrategy := DoNotWaitAfterTooManyErrors(3, consecutiveErrors, WaitForNonZeroTimestamp(NeverTrigger[Timestamped[int]]))

	ctx := context.Background()

	getWithTimeout := func() (Timestamped[int], error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		return cache.Get(ctx, accessStrategy)
	}

	sendLoadResult := func(r loadResult) {
		t.Helper()
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case loadableResults <- r:
			timer.Stop()
		case <-timer.C:
			t.Fatal("timed out waiting for cache to load result")
		}
	}

	assert.Equal(t, uint(0), consecutiveErrors.Value())

	// First attempt: blocking

	_, err := getWithTimeout()
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	sendLoadResult(loadResult{value: 0, err: fmt.Errorf("attempt 1 failed")}) //nolint:goerr113
	loadErrorResult := <-loadErrors
	assert.Equal(t, Timestamped[int]{}, loadErrorResult.previous)
	assert.Equal(t, uint(1), loadErrorResult.consecutiveErrors)
	assert.Equal(t, "attempt 1 failed", loadErrorResult.err.Error())
	assert.Equal(t, uint(1), consecutiveErrors.Value())

	// Second attempt: blocking

	_, err = getWithTimeout()
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	sendLoadResult(loadResult{value: 0, err: fmt.Errorf("attempt 2 failed")}) //nolint:goerr113
	loadErrorResult = <-loadErrors
	assert.Equal(t, Timestamped[int]{}, loadErrorResult.previous)
	assert.Equal(t, uint(2), loadErrorResult.consecutiveErrors)
	assert.Equal(t, "attempt 2 failed", loadErrorResult.err.Error())
	assert.Equal(t, uint(2), consecutiveErrors.Value())

	// Third attempt: blocking

	_, err = getWithTimeout()
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case loadableResults <- loadResult{0, nil}:
		assert.FailNow(t, "updater should be in backoff")
	default:
		clock.Advance(time.Second)
	}

	_, err = getWithTimeout()
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	sendLoadResult(loadResult{value: 0, err: fmt.Errorf("attempt 3 failed")}) //nolint:goerr113
	loadErrorResult = <-loadErrors
	assert.Equal(t, Timestamped[int]{}, loadErrorResult.previous)
	assert.Equal(t, uint(3), loadErrorResult.consecutiveErrors)
	assert.Equal(t, "attempt 3 failed", loadErrorResult.err.Error())
	assert.Equal(t, uint(3), consecutiveErrors.Value())

	// Fourth attempt: no blocking

	realClockStartTime := time.Now()
	value, err := getWithTimeout()
	assert.LessOrEqual(t, time.Since(realClockStartTime), 1*time.Millisecond)
	assert.NoError(t, err)
	assert.Equal(t, Timestamped[int]{}, value)
}
