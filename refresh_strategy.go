package svcache

// RefreshStrategy is a function that determines what should be done re refreshing when encountering the given value
// while trying to get a value from the cache.
type RefreshStrategy[V any] func(currentValue V) Action

var _ RefreshStrategy[any] = JustReturn[any]

// JustReturn is a RefreshStrategy that always returns the current value without triggering an update.
func JustReturn[V any](current V) Action {
	return Return
}

// NonBlockingRefreshStrategy is a function that determines whether to trigger a refreshing when encountering the given value
// while trying to get a value in a non-blocking call.
type NonBlockingRefreshStrategy[V any] func(currentValue V) bool

var _ NonBlockingRefreshStrategy[any] = NeverTrigger[any]
var _ NonBlockingRefreshStrategy[any] = AlwaysTrigger[any]

// NeverTrigger is a NonBlockingRefreshStrategy that never triggers an update.
func NeverTrigger[V any](current V) bool {
	return false
}

// AlwaysTrigger is a NonBlockingRefreshStrategy that always triggers an update.
func AlwaysTrigger[V any](current V) bool {
	return true
}

// AsRefreshStrategy promotes a non-blocking refresh strategy to a refresh strategy.
func AsRefreshStrategy[V any](nonBlockingStrategy NonBlockingRefreshStrategy[Timestamped[V]]) RefreshStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) Action {
		return nonBlockingAction(nonBlockingStrategy, current)
	}
}

func nonBlockingAction[V any](strategy NonBlockingRefreshStrategy[V], value V) Action {
	if strategy(value) {
		return TriggerLoadAndReturn
	}
	return Return
}
