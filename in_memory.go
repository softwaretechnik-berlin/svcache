package svcache

import (
	"context"
	"fmt"
	"sync/atomic"
)

// InMemory is a threadsafe in-memory implementation of SingleValueCache.
type InMemory[V any] struct {
	// The current state of the cache.
	//
	// This pointer should always be accessed via the atomic package.
	state atomic.Pointer[inMemoryState[V]]

	// The context used to load cache entries.
	//nolint:containedctx
	loaderCtx context.Context

	// The function used to load cache entries.
	loader Updater[V]
}

var _ SingleValueCache[any] = (*InMemory[any])(nil)

// NewInMemory returns a new InMemory SingleValueCache using the given Loader.
//
// There will only ever be one goroutine invoking the Loader at any given time.
// If multiple threads call Get concurrently, a single one of them will invoke the Loader, and the others will wait for it to finish.
func NewInMemory[V any](loaderCtx context.Context, loader Updater[V]) *InMemory[V] {
	return NewInMemoryWithInitialValue(loaderCtx, *new(V), loader)
}

// NewInMemoryWithInitialValue returns a new InMemory SingleValueCache using the given initial value and Loader.
//
// There will only ever be one goroutine invoking the Loader at any given time.
// If multiple threads call Get concurrently, a single one of them will invoke the Loader, and the others will wait for it to finish.
func NewInMemoryWithInitialValue[V any](loaderCtx context.Context, initialValue V, loader Updater[V]) *InMemory[V] {
	im := &InMemory[V]{loaderCtx: loaderCtx, loader: loader}
	im.state.Store(newInMemoryState(initialValue))
	return im
}

// Get returns a value from the cache.
//
// The value currently in the cache is passed to the given refresh strategy to determine what should be done.
//
// If the refresh strategy returns `Return`, the value currently in the cache will be returned immediately.
//
// If the refresh strategy returns `TriggerLoadAndReturn`,
// the cache will trigger asynchronous loading of a new value if this is not already in progress,
// and the value currently in the cache will be returned immediately.
//
// If the refresh strategy returns `WaitForLoad`, the cache will block waiting for a new value to be loaded;
// the process then begins again with the new value being passed to the refresh strategy to determine what should be done with it.
// During the waiting, if the context is cancelled, the context error will be returned and the last considered value will be returned.
func (m *InMemory[V]) Get(ctx context.Context, refreshStrategy AccessStrategy[V]) (V, error) {
	for {
		state := m.state.Load()
		value := state.value
		action := refreshStrategy(value)
		if action > UseCachedValue {
			m.triggerNext(state)
		}
		if action < WaitForNewlyLoadedValue {
			return value, nil
		}
		select {
		case <-ctx.Done():
			return value, fmt.Errorf("cancelled get context interrupted cache get wait: %w", ctx.Err())
		case <-m.loaderCtx.Done():
			return value, fmt.Errorf("cancelled loader context interrupted cache get wait: %w", m.loaderCtx.Err())
		case <-state.newStateAvailable:
		}
	}
}

// GetImmediately returns the current value without blocking or potentially failing.
//
// The value currently in the cache is passed to the given function to determine whether an update should be triggered before returning.
// If the refresh strategy returns `true`, a refresh is trigger, otherwise it is not.
func (m *InMemory[V]) GetImmediately(refreshStrategy RefreshStrategy[V]) V {
	state := m.state.Load()
	value := state.value
	if refreshStrategy(value) {
		m.triggerNext(state)
	}
	return value
}

// Peek returns the current value without triggering a load.
func (m *InMemory[V]) Peek() V {
	return m.state.Load().value
}

func (m *InMemory[V]) triggerNext(current *inMemoryState[V]) {
	if current.loadingNewState.Load() {
		return
	}
	go func() {
		newState := newInMemoryState(current.value)
		if current.loadingNewState.Swap(true) {
			return
		}
		defer func() {
			m.state.Store(newState)
			close(current.newStateAvailable)
		}()
		newState.value = m.loader(m.loaderCtx, current.value)
	}()
}

type inMemoryState[V any] struct {
	// value is the value in the cache for this state.
	value V

	loadingNewState atomic.Bool

	// newStateAvailable is a latch indicating when the new value has been loaded.
	newStateAvailable chan struct{}
}

func newInMemoryState[V any](value V) *inMemoryState[V] {
	return &inMemoryState[V]{
		value:             value,
		newStateAvailable: make(chan struct{}),
	}
}
