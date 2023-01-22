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

func NewInMemoryWithInitialValue[V any](loaderCtx context.Context, initialValue V, loader Updater[V]) *InMemory[V] {
	im := &InMemory[V]{loaderCtx: loaderCtx, loader: loader}
	im.state.Store(newInMemoryState(initialValue))
	return im
}

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
func (m *InMemory[V]) Get(ctx context.Context, strategy RetrievalStrategy[V]) (V, error) {
	for {
		state := m.state.Load()
		value := state.value
		action := strategy(value)
		if action > Return {
			m.triggerNext(state)
		}
		if action < WaitForLoad {
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
