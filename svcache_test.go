package svcache

import (
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
	cache := NewInMemory(func(previous Entry) Entry {
		value, ok := determineNewValue()
		if !ok {
			return previous
		}

		now := time.Now()
		return Entry{
			Value:            value,
			BecomesRenewable: now.Add(300 * time.Millisecond),
			Expires:          now.Add(500 * time.Millisecond),
		}
	})

	// block for a value
	println(cache.Get())

	// get a value if available
	if value, ok := cache.Peek(); ok {
		println(value)
	}
}

func determineNewValue() (value json.RawMessage, ok bool) {
	resp, err := http.Get("https://httpbin.org/get")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	ok = json.Unmarshal(body, &value) == nil
	return
}

var testRenewalbleAfter = time.Second
var testTTL = 3 * time.Second
var semiTestTick = time.Millisecond

// fullTestTick is designed to move the test time forward such that we skip over the exact time boundary, giving the implementation flexibility about how to handle that edge case.
var fullTestTick = 2 * semiTestTick

func TestPointInTimeSequential(t *testing.T) {
	cache, probe := newCacheForTest()

	value, isPresent := cache.Peek()
	assert.False(t, isPresent)
	assert.Nil(t, value)
	assert.Equal(t, 0, probe.LoaderInvocations())

	value = cache.Get()
	assert.Equal(t, 1, value)
	assert.Equal(t, 1, probe.LoaderInvocations())

	value = cache.Get()
	assert.Equal(t, 1, value)
	assert.Equal(t, 1, probe.LoaderInvocations())

	value, isPresent = cache.Peek()
	assert.True(t, isPresent)
	assert.Equal(t, 1, value)
	assert.Equal(t, 1, probe.LoaderInvocations())
}

func TestPointInTimeConcurrent(t *testing.T) {
	cache, probe := newCacheForTest()

	assertValueWithConcurrency(t, 1, 100_000, cache, probe)
	assert.Equal(t, 1, probe.LoaderInvocations())
}

func TestTTL(t *testing.T) {
	cache, probe := newCacheForTest()

	assertValueWithConcurrency(t, 1, 100, cache, probe)
	assert.Equal(t, 1, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)

	assertValueWithConcurrency(t, 1, 100, cache, probe)
	assert.Equal(t, 1, probe.LoaderInvocations())

	probe.clock.Advance(testTTL - testRenewalbleAfter)

	assertValueWithConcurrency(t, 1, 100, cache, probe)

	probe.clock.Advance(fullTestTick)

	// allow loading to complete after starting to retrieve value
	assertValueWithConcurrencyNoAdd(t, 2, 100, cache, probe)
	assert.Equal(t, 2, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)

	assertValueWithConcurrency(t, 2, 100, cache, probe)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(testTTL - testRenewalbleAfter)

	assertValueWithConcurrency(t, 2, 100, cache, probe)

	probe.clock.Advance(fullTestTick)

	// allow loading to complete after starting to retrieve value
	assertValueWithConcurrencyNoAdd(t, 3, 100, cache, probe)
	assert.Equal(t, 3, probe.LoaderInvocations())
}

func TestRenew(t *testing.T) {
	cache, probe := newCacheForTest()

	assertValueWithConcurrency(t, 1, 100, cache, probe)
	assert.Equal(t, 1, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)

	assert.Equal(t, 1, cache.Get())
	assert.Equal(t, 1, probe.LoaderInvocations())

	probe.clock.Advance(fullTestTick)

	assertValueWithConcurrency(t, 1, 100_000, cache, probe)
	waitActivelyUntil(t, func() bool { return probe.LoaderInvocations() != 1 }, time.Second)
	assert.Equal(t, 2, probe.LoaderInvocations())

	// Allow the loading to complete
	probe.waitGroup.Done()

	waitActivelyUntil(t, func() bool {
		value, hasValue := cache.Peek()
		return !hasValue || value != 1
	}, time.Second)

	value, hasValue := cache.Peek()
	assert.True(t, hasValue)
	assert.Equal(t, 2, value)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)
	assertValueWithConcurrency(t, 2, 100, cache, probe)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(fullTestTick)
	assert.Equal(t, 2, cache.Get())
	waitActivelyUntil(t, func() bool { return probe.LoaderInvocations() != 2 }, time.Second)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 3, probe.LoaderInvocations())
}

func TestExpiresDuringRenew(t *testing.T) {
	cache, probe := newCacheForTest()

	assertValueWithConcurrency(t, 1, 100, cache, probe)
	assert.Equal(t, 1, probe.LoaderInvocations())

	// we won't let the loading complete until later
	probe.waitGroup.Add(1)

	probe.clock.Advance(testRenewalbleAfter + semiTestTick)

	assertValueWithConcurrency(t, 1, 100, cache, probe)
	waitActivelyUntil(t, func() bool { return probe.LoaderInvocations() != 1 }, time.Second)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(-fullTestTick + testTTL - testRenewalbleAfter)
	assertValueWithConcurrency(t, 1, 100_000, cache, probe)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(fullTestTick)

	// Allow the loading to complete after starting to retrieve value
	assertValueWithConcurrencyNoAdd(t, 2, 100_000, cache, probe)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())

	value, hasValue := cache.Peek()
	assert.True(t, hasValue)
	assert.Equal(t, 2, value)
	assert.Equal(t, 2, probe.LoaderInvocations())

	probe.clock.Advance(testRenewalbleAfter - semiTestTick)
	assertValueWithConcurrency(t, 2, 100_000, cache, probe)
	time.Sleep(time.Millisecond)
	assert.Equal(t, 2, probe.LoaderInvocations())
}

func newCacheForTest() (*InMemory, *testProbe) {
	probe := &testProbe{
		clock: newManualClock(),
	}
	load := func(_ Entry) Entry {
		value := int(probe.loaderInvocations.Increment())
		probe.waitGroup.Wait()
		now := probe.clock.Now()
		return Entry{
			Value:            value,
			BecomesRenewable: now.Add(testRenewalbleAfter),
			Expires:          now.Add(testTTL),
		}
	}
	cache := NewInMemory(load)
	cache.clock = probe.clock
	return cache, probe
}

type testProbe struct {
	waitGroup         sync.WaitGroup
	loaderInvocations atomicCounter
	clock             *manualClock
}

func (p *testProbe) LoaderInvocations() int {
	return int(p.loaderInvocations.Value())
}

type atomicCounter struct {
	uint32
}

func (c *atomicCounter) Value() uint32 {
	return atomic.LoadUint32(&c.uint32)
}

func (c *atomicCounter) Increment() uint32 {
	return atomic.AddUint32(&c.uint32, 1)
}

func assertValueWithConcurrency(t *testing.T, value interface{}, concurrency int, cache SingleValueCache, probe *testProbe) {
	probe.waitGroup.Add(1)
	assertValueWithConcurrencyNoAdd(t, value, concurrency, cache, probe)
}

func assertValueWithConcurrencyNoAdd(t *testing.T, value interface{}, concurrency int, cache SingleValueCache, probe *testProbe) {
	results := make(chan interface{}, concurrency)
	for i := 0; i <= concurrency; i++ {
		go func() {
			results <- cache.Get()
		}()
	}

	runtime.Gosched()
	probe.waitGroup.Done()

	for i := 0; i <= concurrency; i++ {
		select {
		case result := <-results:
			if !assert.Equal(t, value, result, "iteration %d", i) {
				return
			}
		case <-time.After(time.Second):
			assert.Fail(t, "Timed out while waiting for result")
			return
		}
	}
}

func waitActivelyUntil(t *testing.T, condition func() bool, timeout time.Duration) bool {
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
