SVCache
=======

SVCache is a threadsafe, single-value cache with a simple but flexible API.

When there is no fresh value in the cache, an attempt to retrieve the value will block until a new value is loaded.
Only a single goroutine will load a new value; other goroutines will block as necessary until the loading is complete.

SVCache also supports aynchronously loading a new value before a value has expired.

See the GoDoc for details.


Example
-------

```go
import "github.com/softwaretechnik-berlin/svcache"

demonstrateApi() {
    cache := svcache.NewInMemory(func(previous Entry) Entry {
        value, ok := determineNewValue()
        if !ok {
            return previous
        }

        now := time.Now()
        return Entry{
            Value:            value,
            BecomesRenewable: now.Add(3 * time.Minute),
            Expires:          now.Add(5 * time.Minute),
        }
    })

    // Block for a value
    value := cache.Get()
    println(value)

    // peek at the current value if available
    if value, ok := cache.Peek(); ok {
        println(value)
    }
}
```
