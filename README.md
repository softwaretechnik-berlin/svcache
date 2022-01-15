SVCache
=======

SVCache is a threadsafe, single-value cache with a simple but flexible API.

When there is no fresh value in the cache, an attempt to retrieve the value will block until a new value is loaded.
Only a single goroutine will load a new value; other goroutines will block as necessary until the loading is complete.

SVCache also supports aynchronously loading a new value before a value has expired.

See the GoDoc for details.


Example
-------

Here's a contrived example to show the interface (feel free to [try it in the playground](https://go.dev/play/p/FfrECmHC1aX)):

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/softwaretechnik-berlin/svcache"
)

func main() {
    cache := svcache.NewInMemory(func(previous svcache.Entry) svcache.Entry {
        value, ok := determineNewValue()
        if !ok {
            return previous
        }

        now := time.Now()
        return svcache.Entry{
            Value:            value,
            BecomesRenewable: now.Add(time.Second / 2),
            Expires:          now.Add(time.Second),
        }
    })

    for i := 0; i < 50; i++ {
        i := i
        go func() {
            // Block for a value
            value := cache.Get(context.Background())
            fmt.Println(i, "Get ", value)
        }()
        time.Sleep(100 * time.Millisecond)
    }

    // peek at the current value if available
    if value, ok := cache.Peek(); ok {
        fmt.Println("Peek", value)
    }
}

// this is a stand-in for whatever type you want to use with the cache
type whateverTypeYouPlease struct {
    AField time.Time
}

// this is a stub for whatever fancy behaviour you have for getting a value
func determineNewValue() (whateverTypeYouPlease, bool) {
    time.Sleep(200 * time.Millisecond)
    return whateverTypeYouPlease{time.Now()}, true
}
```
