package svcache

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func ExampleInMemory() {
	// Given a loader function that returns a new value, like this one:

	type uuidResponse struct {
		UUID string `json:"uuid"`
	}

	fetchNewValue := func(ctx context.Context) (value string, ok bool) {
		// documentation: https://httpbin.org/#/Dynamic_data/get_uuid
		resp, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://httpbin.org/uuid", nil)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return
		}
		var response uuidResponse
		ok = json.Unmarshal(body, &response) == nil
		return response.UUID, ok
	}

	// We can create an InMemory cache like this:
	ctx := context.Background()
	cache := NewInMemory(ctx, func(ctx context.Context, previous string) string {
		value, ok := fetchNewValue(ctx)
		if !ok {
			return previous
		}
		return value
	})

	// block for a non-empty value
	blockIfEmpty := func(current string) Action {
		if current == "" {
			return WaitForNewlyLoadedValue
		}
		return UseCachedValue
	}
	if value, err := cache.Get(context.Background(), blockIfEmpty); err == nil {
		println(value)
	}

	// get current value without blocking
	println(cache.Peek())
}

const testRenewalbleAfter = time.Second
const testTTL = 3 * time.Second
const semiTestTick = time.Millisecond

// fullTestTick is designed to move the test time forward such that we skip over the exact time boundary,
// giving the implementation flexibility about how to handle that edge case.
const fullTestTick = 2 * semiTestTick

func refreshStrategyForTest(probe *testProbe) AccessStrategy[Timestamped[int]] {
	return triggerOrWaitIfAged[int](probe.clock, testRenewalbleAfter, testTTL)
}

func TestPointInTimeSequential(t *testing.T) {
	t.Parallel()
	cache, probe := newCacheForTest()

	refreshStrategy := refreshStrategyForTest(probe)

	value := cache.Peek()
	assert.True(t, value.Timestamp.IsZero())
	assert.Equal(t, 0, value.Value)
	assert.Equal(t, 0, probe.LoaderInvocations())

	value, err := cache.Get(context.Background(), refreshStrategy)
	assert.NoError(t, err)
	assert.Equal(t, 1, value.Value)
	assert.Equal(t, 1, probe.LoaderInvocations(), refreshStrategy)

	value, err = cache.Get(context.Background(), refreshStrategy)
	assert.NoError(t, err)
	assert.Equal(t, 1, value.Value)
	assert.Equal(t, 1, probe.LoaderInvocations())

	value = cache.Peek()
	assert.Equal(t, probe.clock.Now(), value.Timestamp)
	assert.Equal(t, 1, value.Value)
	assert.Equal(t, 1, probe.LoaderInvocations())
}

func TestPointInTimeConcurrent(t *testing.T) {
	t.Parallel()
	cache, probe := newCacheForTest()

	refreshStrategy := refreshStrategyForTest(probe)

	assertValueWithConcurrency[Timestamped[int]](t, Timestamped[int]{1, probe.clock.Now()}, 100_000, cache, probe, refreshStrategy)
	assert.Equal(t, 1, probe.LoaderInvocations())
}

func TestTTL(t *testing.T) {
	t.Parallel()
	cache, probe := newCacheForTest()

	refreshStrategy := refreshStrategyForTest(probe)

	expectedValueAfterLoad1 := Timestamped[int]{1, probe.clock.Now()}
	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100, cache, probe, refreshStrategy)
	assert.Equal(t, 1, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)

	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100, cache, probe, refreshStrategy)
	assert.Equal(t, 1, probe.LoaderInvocations())

	probe.clock.Advance(testTTL - testRenewalbleAfter)

	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100, cache, probe, refreshStrategy)

	probe.clock.Advance(fullTestTick)

	// allow loading to complete after starting to retrieve value
	expectedValueAfterLoad2 := Timestamped[int]{2, probe.clock.Now()}
	assertValueWithConcurrencyNoAdd[Timestamped[int]](t, expectedValueAfterLoad2, 100, cache, probe, refreshStrategy)
	assert.Equal(t, 2, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)

	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad2, 100, cache, probe, refreshStrategy)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(testTTL - testRenewalbleAfter)

	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad2, 100, cache, probe, refreshStrategy)

	probe.clock.Advance(fullTestTick)

	// allow loading to complete after starting to retrieve value
	assertValueWithConcurrencyNoAdd[Timestamped[int]](t, Timestamped[int]{3, probe.clock.Now()}, 100, cache, probe, refreshStrategy)
	assert.Equal(t, 3, probe.LoaderInvocations())
}

func TestRenew(t *testing.T) {
	t.Parallel()
	cache, probe := newCacheForTest()

	refreshStrategy := refreshStrategyForTest(probe)

	expectedValueAfterLoad1 := Timestamped[int]{1, probe.clock.Now()}
	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100, cache, probe, refreshStrategy)
	assert.Equal(t, 1, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)

	value, err := cache.Get(context.Background(), refreshStrategy)
	assert.NoError(t, err)
	assert.Equal(t, expectedValueAfterLoad1, value)
	assert.Equal(t, 1, probe.LoaderInvocations())

	probe.clock.Advance(fullTestTick)

	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100_000, cache, probe, refreshStrategy)
	waitActivelyUntil(t, func() bool { return probe.LoaderInvocations() != 1 }, time.Second)
	assert.Equal(t, 2, probe.LoaderInvocations())

	// Allow the loading to complete
	probe.waitGroup.Done()

	waitActivelyUntil(t, func() bool { return cache.Peek() != expectedValueAfterLoad1 }, time.Second)

	expectedValueAfterLoad2 := Timestamped[int]{2, probe.clock.Now()}

	value = cache.Peek()
	assert.Equal(t, expectedValueAfterLoad2, value)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)
	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad2, 100, cache, probe, refreshStrategy)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(fullTestTick)
	value, err = cache.Get(context.Background(), refreshStrategy)
	assert.NoError(t, err)
	assert.Equal(t, expectedValueAfterLoad2, value)
	waitActivelyUntil(t, func() bool { return probe.LoaderInvocations() != 2 }, time.Second)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 3, probe.LoaderInvocations())
}

func TestExpiresDuringRenew(t *testing.T) {
	t.Parallel()
	cache, probe := newCacheForTest()

	refreshStrategy := refreshStrategyForTest(probe)

	expectedValueAfterLoad1 := Timestamped[int]{1, probe.clock.Now()}
	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100, cache, probe, refreshStrategy)
	assert.Equal(t, 1, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter + semiTestTick)

	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100, cache, probe, refreshStrategy)
	waitActivelyUntil(t, func() bool { return probe.LoaderInvocations() != 1 }, time.Second)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(-fullTestTick + testTTL - testRenewalbleAfter)
	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad1, 100_000, cache, probe, refreshStrategy)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(fullTestTick)

	// Allow the loading to complete after starting to retrieve value
	expectedValueAfterLoad2 := Timestamped[int]{2, probe.clock.Now()}
	assertValueWithConcurrencyNoAdd[Timestamped[int]](t, expectedValueAfterLoad2, 100_000, cache, probe, refreshStrategy)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())

	value := cache.Peek()
	assert.Equal(t, expectedValueAfterLoad2, value)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)
	assertValueWithConcurrency[Timestamped[int]](t, expectedValueAfterLoad2, 100_000, cache, probe, refreshStrategy)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())
}

func TestGetContext(t *testing.T) {
	t.Parallel()
	cache, probe := newCacheForTest()

	refreshStrategy := refreshStrategyForTest(probe)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, err := cache.Get(ctx, refreshStrategy)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, Timestamped[int]{}, value, "Empty value expected because the context was cancelled and no value has yet been loaded")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, probe.LoaderInvocations(),
		"Though the retrieval context was cancelled, the loader context was not, so there's no reason not to retrieve")
	expectedValueAfterLoad1 := Timestamped[int]{1, probe.clock.Now()}
	assert.Equal(t, expectedValueAfterLoad1, cache.Peek())
	probe.clock.Advance(2 * testTTL)

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	value, err = cache.Get(ctx, refreshStrategy)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, expectedValueAfterLoad1, value)
	assert.Equal(t, 2, probe.LoaderInvocations())

	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	value, err = cache.Get(ctx, refreshStrategy)
	cancel()
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, expectedValueAfterLoad1, value)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.waitGroup.Done()
}

func newCacheForTest() (*InMemory[Timestamped[int]], *testProbe) {
	probe := &testProbe{
		sync.WaitGroup{},
		atomic.Uint32{},
		newManualClock(),
	}
	updater := func(ctx context.Context, previous Timestamped[int]) Timestamped[int] {
		value := int(probe.loaderInvocations.Add(1))
		probe.waitGroup.Wait()
		now := probe.clock.Now()
		return Timestamped[int]{value, now}
	}
	cache := NewInMemory(context.Background(), updater)
	return cache, probe
}

type testProbe struct {
	waitGroup         sync.WaitGroup
	loaderInvocations atomic.Uint32
	clock             *manualClock
}

func (p *testProbe) LoaderInvocations() int {
	return int(p.loaderInvocations.Load())
}

func assertValueWithConcurrency[V any](
	t *testing.T,
	value V,
	concurrency int,
	cache SingleValueCache[V],
	probe *testProbe,
	refreshStrategy AccessStrategy[V],
) {
	t.Helper()
	probe.waitGroup.Add(1)
	assertValueWithConcurrencyNoAdd(t, value, concurrency, cache, probe, refreshStrategy)
}

type result[V any] struct {
	value V
	err   error
}

//nolint:thelper
func assertValueWithConcurrencyNoAdd[V any](
	t *testing.T,
	value V,
	concurrency int,
	cache SingleValueCache[V],
	probe *testProbe,
	refreshStrategy AccessStrategy[V],
) {
	results := make(chan result[V], concurrency)
	for i := 0; i <= concurrency; i++ {
		go func() {
			value, err := cache.Get(context.Background(), refreshStrategy)
			results <- result[V]{value, err}
		}()
	}

	runtime.Gosched()
	probe.waitGroup.Done()

	for iteration := 0; iteration <= concurrency; iteration++ {
		select {
		case result := <-results:
			if !assert.NoError(t, result.err, "iteration %d", iteration) {
				return
			}
			if !assert.Equal(t, value, result.value, "iteration %d", iteration) {
				return
			}
		case <-time.After(time.Second):
			assert.Fail(t, "Timed out while waiting for result")
			return
		}
	}
}

//nolint:unparam
func waitActivelyUntil(t *testing.T, condition func() bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return true
		}
		time.Sleep(time.Millisecond)
		if time.Now().After(deadline) {
			assert.Fail(t, "Timed out waiting actively for condition")
			return false
		}
	}
}
