// Package svcache provides a threadsafe in-memory single-value cache.
package svcache

import (
	"context"
)

type Action uint8

const (
	UseCachedValue Action = iota
	TriggerLoadAndUseCachedValue
	WaitForNewlyLoadedValue
)

// Cache is a cache for a single value of type V.
type Cache[V any] interface {
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
	Get(context.Context, AccessStrategy[V]) (V, error)
}

// Updater is function that loads values into the cache.
//
// The Updater receives the previous timestamped value, and returns a new one.
//
// If the Updater fails to load a new value it should return the previous value.
// The Updater may be implemented with the assumption that there will only be a single invocation at a time.
// The Updater can block as necessary while loading the value,
// and may even choose to sleep e.g. to implement backoff logic to avoid overwhelming a struggling value source.
type Updater[V any] func(ctx context.Context, previous V) V

// Loader is a function that loads values into the cache.
// It is not used directly by the cache, but can be used to create an Updater.
type Loader[V any] func(context.Context) (V, error)
