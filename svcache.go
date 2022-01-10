// Package svcache provides a threadsafe in-memory single-value cache.
package svcache

import (
	"sync/atomic"
	"time"
	"unsafe"
)

var alreadyLoaded <-chan struct{}
var expiredState inMemoryState

func init() {
	closedChannel := make(chan struct{})
	close(closedChannel)
	alreadyLoaded = closedChannel
	expiredState = inMemoryState{loaded: alreadyLoaded}
}

// Entry contains a cache value and information about how long to use it for.
//
// A Loader returns an Entry, instead of just a value.
// This allows the durations after which values become renewable or expired to be time- or value-dependent.
type Entry struct {

	// Value is value to return from the cache.
	Value interface{}

	// BecomesRenewable gives a time after which the cache should start trying to renew the cache entry.
	BecomesRenewable time.Time

	// expires gives a time after which the cache should no longer return this value
	Expires time.Time
}

func (e Entry) unexpiredValue(now time.Time) (value interface{}, isUnexpired bool) {
	if now.After(e.Expires) {
		return nil, false
	}
	return e.Value, true
}

// Loader is function for loading values into the cache.
//
// A Loader returns receives the previous cache Entry, and returns a new Entry.
//
// The new Entry should have an Expires time in the future, otherwise the value can't be used.
type Loader func(previous Entry) Entry

// SingleValueCache is a cache for a single value.
type SingleValueCache interface {
	// Get returns the current value.
	//
	// If there is no value readily available, it will block until a value is available.
	Get() (value interface{})

	// Peek returns an unexpired value if one is already loaded.
	//
	// If there is no unexpired value immediately available, the value will be nil and hasValue will be false.
	Peek() (value interface{}, hasValue bool)
}

// InMemory is threadsafe in-memory implementation of SingleValueCache.
type InMemory struct {
	// The current state of the cache.
	//
	// This pointer should always be accessed via the atomic package.
	state unsafe.Pointer // *inMemoryState

	// The function used to load cache entries.
	loader Loader

	// The clock used to obtain the current time.
	//
	// This clock is used rather than calling time.Now() direct to make it easier to test the cache's behaviour.
	clock clock
}

type inMemoryState struct {
	// previousEntry is immutable, and holds the previous entry to allow the old value to be used while loading the new entry as long as it hasn't expired.
	previousEntry Entry

	// loaded is a latch indicating when the entry has been loaded.
	loaded <-chan struct{}

	// currentEntry is not readable before the loaded channel is closed, and is immutable thereafter.
	currentEntry Entry
}

var _ SingleValueCache = (*InMemory)(nil)

// NewInMemory returns a new InMemory SingleValueCache using the given Loader.
//
// There will only ever be one goroutine invoking the Loader at any given time.
// If multiple threads call Get concurrently, a single one of them will invoke the Loader, and the others will wait for it to finish.
func NewInMemory(loader Loader) *InMemory {
	return &InMemory{
		state:  unsafe.Pointer(&expiredState),
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
func (m *InMemory) Get() interface{} {
	for {
		state, loaded := m.getState()
		now := m.clock.Now()
		if loaded {
			if value, unexpired := state.currentEntry.unexpiredValue(now); unexpired {
				if !now.Before(state.currentEntry.BecomesRenewable) {
					go m.loadIfStateIsStill(state)
				}
				return value
			}
			m.loadIfStateIsStill(state)
		} else {
			if value, unexpired := state.previousEntry.unexpiredValue(now); unexpired {
				return value
			}
			<-state.loaded
		}
	}
}

// Peek returns an unexpired value if one is already loaded.
//
// If there is no unexpired value immediately available, the value will be nil and hasValue will be false.
func (m *InMemory) Peek() (value interface{}, hasValue bool) {
	state, loaded := m.getState()
	entry := state.previousEntry
	if loaded {
		entry = state.currentEntry
	}
	value, hasValue = entry.unexpiredValue(m.clock.Now())
	return
}

func (m *InMemory) getState() (state *inMemoryState, loaded bool) {
	state = (*inMemoryState)(atomic.LoadPointer(&m.state))
	select {
	case <-state.loaded:
		loaded = true
	default:
	}
	return
}

func (m *InMemory) loadIfStateIsStill(old *inMemoryState) {
	loaded := make(chan struct{})
	newState := &inMemoryState{
		previousEntry: old.currentEntry,
		loaded:        loaded,
	}
	if !m.replaceState(old, newState) {
		return
	}
	newState.currentEntry = m.loader(newState.previousEntry)
	close(loaded)
}

func (m *InMemory) replaceState(old *inMemoryState, new *inMemoryState) bool {
	return atomic.CompareAndSwapPointer(&m.state, unsafe.Pointer(old), unsafe.Pointer(new))
}
