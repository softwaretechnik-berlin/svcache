package svcache

// AccessStrategy is a function that determines what should be done when accessing the value in the cache
// using the potentially blocking `Get` function.
//
// It can either return `UseCachedValue` to return the current value immediately,
// or `TriggerLoadAndUseCachedValue` to trigger an asynchronous load of a new value but immediately return the current value,
// or `WaitForNewlyLoadedValue` to block until a new value is loaded and then return that.
//
// It is given the currently cached value, in case this decision needs to be value-dependent.
type AccessStrategy[V any] func(currentValue V) Action

var _ AccessStrategy[any] = JustReturn[any]

// JustReturn is a AccessStrategy that always returns the current value without triggering an update.
func JustReturn[V any](current V) Action {
	return UseCachedValue
}

// RefreshStrategy is a function that determines whether to trigger a refresh of the cached value.
//
// It can either return `true` to trigger a refresh, or `false` to not trigger a refresh.
//
// It is given the currently cached value, in case this decision needs to be value-dependent.
type RefreshStrategy[V any] func(currentValue V) bool

var _ RefreshStrategy[any] = NeverTrigger[any]
var _ RefreshStrategy[any] = AlwaysTrigger[any]

// NeverTrigger is a RefreshStrategy that never triggers an update.
func NeverTrigger[V any](current V) bool {
	return false
}

// AlwaysTrigger is a RefreshStrategy that always triggers an update.
func AlwaysTrigger[V any](current V) bool {
	return true
}

// AsNonBlockingAccessStrategy promotes a non-blocking refresh strategy to a refresh strategy.
func AsNonBlockingAccessStrategy[V any](refreshStrategy RefreshStrategy[Timestamped[V]]) AccessStrategy[Timestamped[V]] {
	return func(current Timestamped[V]) Action {
		return nonBlockingAction(refreshStrategy, current)
	}
}

func nonBlockingAction[V any](strategy RefreshStrategy[V], value V) Action {
	if strategy(value) {
		return TriggerLoadAndUseCachedValue
	}
	return UseCachedValue
}

func AccessStrategyFromRefreshStrategyAndWaitPredicate[V any](trigger RefreshStrategy[V], shouldWait func(currentValue V) bool) AccessStrategy[V] {
	return func(current V) Action {
		switch {
		case shouldWait(current):
			return WaitForNewlyLoadedValue
		case trigger(current):
			return TriggerLoadAndUseCachedValue
		default:
			return UseCachedValue
		}
	}
}
