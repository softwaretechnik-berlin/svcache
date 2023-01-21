// Package svcache provides a threadsafe in-memory single-value cache.
package svcache

import (
	"context"
	"sync/atomic"
	"time"
	"unsafe"
)

var alreadyLoaded <-chan struct{}

func init() {
	closedChannel := make(chan struct{})
	close(closedChannel)
	alreadyLoaded = closedChannel
}

// Entry contains a cache value and information about how long to use it for.
//
// A Loader returns an Entry, instead of just a value.
// This allows the durations after which values become renewable or expired to be time- or value-dependent.
type Entry[V any] struct {

	// Value is value to return from the cache.
	Value V

	// BecomesRenewable gives a time after which the cache should start trying to renew the cache entry.
	BecomesRenewable time.Time

	// expires gives a time after which the cache should no longer return this value
	Expires time.Time
}

func (e Entry[V]) unexpiredValue(now time.Time) (value V, isUnexpired bool) {
	return e.Value, now.Before(e.Expires)
}

// Loader is function for loading values into the cache.
//
// A Loader returns receives the previous cache Entry, and returns a new Entry.
//
// If the new Entry has an Expires time in the past, loading will be immediately retried as long as there are goroutines trying to retrive a value.
type Loader[V any] func(previous Entry[V]) Entry[V]

// SingleValueCache is a cache for a single value.
type SingleValueCache[V any] interface {
	// Get returns the current value.
	//
	// If there is no value readily available, it will block until a value is available.
	//
	// If the context is cancelled, the context error will be returned and an expired value will be returned if available, otherwise nil.
	Get(ctx context.Context) (value V, err error)

	// Peek returns an unexpired value if one is already loaded.
	//
	// If there is no unexpired value immediately available, the value will be nil and hasValue will be false.
	Peek() (value V, hasValue bool)
}

// InMemory is threadsafe in-memory implementation of SingleValueCache.
type InMemory[V any] struct {
	// The current state of the cache.
	//
	// This pointer should always be accessed via the atomic package.
	state unsafe.Pointer // *inMemoryState

	// The function used to load cache entries.
	loader Loader[V]

	// The clock used to obtain the current time.
	//
	// This clock is used rather than calling time.Now() direct to make it easier to test the cache's behaviour.
	clock clock
}

type inMemoryState[V any] struct {
	// previousEntry is immutable, and holds the previous entry to allow the old value to be used while loading the new entry as long as it hasn't expired.
	previousEntry Entry[V]

	// loaded is a latch indicating when the entry has been loaded.
	loaded <-chan struct{}

	// currentEntry is not readable before the loaded channel is closed, and is immutable thereafter.
	currentEntry Entry[V]
}

func (s *inMemoryState[V]) isLoaded() bool {
	select {
	case <-s.loaded:
		return true
	default:
		return false
	}
}

func (s *inMemoryState[V]) latestReadableEntry() Entry[V] {
	if s.isLoaded() {
		return s.currentEntry
	}
	return s.previousEntry
}

var _ SingleValueCache[any] = (*InMemory[any])(nil)

// NewInMemory returns a new InMemory SingleValueCache using the given Loader.
//
// There will only ever be one goroutine invoking the Loader at any given time.
// If multiple threads call Get concurrently, a single one of them will invoke the Loader, and the others will wait for it to finish.
func NewInMemory[V any](loader Loader[V]) *InMemory[V] {
	return &InMemory[V]{
		state:  unsafe.Pointer(&inMemoryState[V]{loaded: alreadyLoaded}),
		loader: loader,
		clock:  systemClock{},
	}
}

// func (m *InMemory) Clear() {
// 	m.setState(&expiredState)
// }

// Get returns the current value.
//
// If there is no value in the cache (never loaded or loaded but expired), Get will block until an unexpired value is loaded.
//
// If there is a value in the cache and its Entry's BecomesRenewable time has passed,
// loading of a new value will be triggered asynchronously and the current value will be returned immediately.
//
// Otherwise, there is a value in the cache that is not renewable and hasn't expired,
// and it will be returned immediately.
//
// There will only ever be one goroutine invoking the Loader at any given time.
// If multiple threads call Get concurrently, a single one of them will invoke the Loader, and the others will wait for it to finish.
//
// If the context is cancelled, the context error will be returned and an expired value will be returned if available, otherwise nil.
func (m *InMemory[V]) Get(ctx context.Context) (value V, err error) {
	state := m.getState()

	if err := ctx.Err(); err != nil {
		return state.latestReadableEntry().Value, err
	}

	if state.isLoaded() {
		goto LOADED
	} else if value, unexpired := state.previousEntry.unexpiredValue(m.clock.Now()); unexpired {
		return value, nil
	}

	// The rest of this function is just a for loop that we can jump into the middle of to optimise the case where we already know we're loaded

WAIT_UNTIL_LOADED:
	select {
	case <-ctx.Done():
		return state.previousEntry.Value, ctx.Err()
	case <-state.loaded:
	}

LOADED:
	now := m.clock.Now()
	if value, unexpired := state.currentEntry.unexpiredValue(now); unexpired {
		if !now.Before(state.currentEntry.BecomesRenewable) {
			m.triggerLoading(state)
		}
		return value, nil
	}
	state = m.triggerLoading(state)
	goto WAIT_UNTIL_LOADED // loop
}

// Peek returns an unexpired value if one is already loaded.
//
// If there is no unexpired value immediately available, the value will be nil and hasValue will be false.
func (m *InMemory[V]) Peek() (value V, hasValue bool) {
	value, hasValue = m.getState().latestReadableEntry().unexpiredValue(m.clock.Now())
	return
}

func (m *InMemory[V]) getState() *inMemoryState[V] {
	return (*inMemoryState[V])(atomic.LoadPointer(&m.state))
}

func (m *InMemory[V]) triggerLoading(old *inMemoryState[V]) *inMemoryState[V] {
	loaded := make(chan struct{})
	newState := &inMemoryState[V]{
		previousEntry: old.currentEntry,
		loaded:        loaded,
	}
	if !m.replaceState(old, newState) {
		return m.getState()
	}
	go func() {
		newState.currentEntry = m.loader(newState.previousEntry)
		close(loaded)
	}()
	return newState
}

func (m *InMemory[V]) replaceState(old *inMemoryState[V], new *inMemoryState[V]) bool {
	return atomic.CompareAndSwapPointer(&m.state, unsafe.Pointer(old), unsafe.Pointer(new))
}
