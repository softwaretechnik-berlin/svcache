package svcache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/softwaretechnik-berlin/svcache"
)

func Example() {
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

func fetchNewValue(ctx context.Context) (string, error) {
	// documentation: https://httpbin.org/#/Dynamic_data/get_uuid
	resp, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://httpbin.org/uuid", nil)
	if err != nil {
		return "", fmt.Errorf("initating GET request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}
	var response uuidResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response body: %w", err)
	}
	return response.UUID, nil
}

type uuidResponse struct {
	UUID string `json:"uuid"`
}
