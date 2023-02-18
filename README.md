SVCache
=======

[![Go Reference](https://pkg.go.dev/badge/github.com/softwaretechnik-berlin/svcache.svg)](https://pkg.go.dev/github.com/softwaretechnik-berlin/svcache) [![Build](https://github.com/softwaretechnik-berlin/svcache/actions/workflows/build.yml/badge.svg)](https://github.com/softwaretechnik-berlin/svcache/actions/workflows/build.yml)

SVCache is a threadsafe, single-value cache with a simple but flexible API.

When there is no fresh value in the cache, an attempt to retrieve the value will block until a new value is loaded.
Only a single goroutine will load a new value; other goroutines will block as necessary until the loading is complete.

SVCache also supports aynchronously loading a new value before a value has expired.

See the GoDoc for details.


Example
-------

Here's a brief example demonstrate how it can be used:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/softwaretechnik-berlin/svcache"
)

func main() {
	// You can create a full-featured cache like this:
	cache := svcache.NewInMemory(

		// This context will be used for all calls to the loader, and is respected when a caller waits for a fresh cache value.
		// E.g. you can cancel the context to signal to the load that it should stop and to prevent readers from blocking on updates that aren't coming.
		context.Background(),

		// The backoff updater
		svcache.NewBackoffUpdater(

			// The backoff updater We'll automatically add timestamps to the values returned by the loader.
			// Alternatively
			svcache.NewTimestampedLoader(fetchNewValue),

			// Define a backoff strategy.
			// Here we use our balanced backoff strategy which exponentially scales between two values at the rate of the Fibonacci sequence with some jitter.
			// This is a good default strategy for many cases, but you might choose to use a strategy tailored to your use case.
			svcache.NewBalancedBackoffStrategy(100*time.Millisecond, time.Minute),

			// The error handler is called whenever the loader returns an error, allowing you to log or otherwise handle the error.
			func(ctx context.Context, previous svcache.Timestamped[string], consecutiveErrors uint, err error) {
				fmt.Printf(
					"error retrieving new value for cache (number %v in a row, current value has timestamp %v): %v",
					consecutiveErrors, previous.Timestamp, err,
				)
			},
		),
	)

	// Now you can use the cache.
	// Each time you access it, you can use whatever retrieval strategy is appropriate for that call.
	// You'll probably want to instantiate the strategies once and reuse them, but you can also create them on the fly.

	// some retrieval strategies:
	alwaysTrigger := svcache.AlwaysTrigger[svcache.Timestamped[string]]
	waitForAnyValue := svcache.WaitForNonZeroTimestamp(alwaysTrigger)
	triggerIfTooOld := svcache.TriggerIfAged[string](5 * time.Minute)

	// some accesses:
	value, err := cache.Get(context.Background(), waitForAnyValue)
	fmt.Println(value, err)
	value = cache.GetImmediately(triggerIfTooOld)
	fmt.Println(value)
	value = cache.GetImmediately(alwaysTrigger)
	fmt.Println(value)

	// the strategy in the last access always returns a value, and for that we can use the simpler Peek method:
	fmt.Println(cache.Peek())
}
```
