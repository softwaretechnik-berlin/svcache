package svcache

import (
	"sync"
	"time"
)

// clock abstracts over the advancement of time.
type clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Sleep(d time.Duration)
}

// systemClock is a clock implementation that use the real system time, implemented in terms of time.Now, time.Since and time.Sleep.
type systemClock struct{}

var _ clock = systemClock{}

func (c systemClock) Now() time.Time {
	return time.Now()
}

func (c systemClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

func (c systemClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// manualClock is a clock implementation for use in tests that must be manually advanced.
type manualClock struct {
	mutex    sync.Mutex
	now      time.Time
	sleepers []Timestamped[chan<- struct{}]
}

var _ clock = (*manualClock)(nil)

func newManualClock() *manualClock {
	return &manualClock{
		sync.Mutex{},
		time.Date(2021, 1, 9, 14, 3, 0, 0, time.UTC),
		nil,
	}
}

func (c *manualClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *manualClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

func (c *manualClock) Sleep(duration time.Duration) {
	if duration <= 0 {
		return
	}
	done := make(chan struct{})
	func() {
		c.mutex.Lock()
		defer c.mutex.Unlock()
		c.sleepers = append(c.sleepers, Timestamped[chan<- struct{}]{done, c.now.Add(duration)})
	}()
	<-done
}

func (c *manualClock) Advance(duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = c.now.Add(duration)
	index := 0
	for index < len(c.sleepers) {
		if c.sleepers[index].Timestamp.Before(c.now) {
			index++
		} else {
			close(c.sleepers[index].Value)
			c.sleepers[index] = c.sleepers[len(c.sleepers)-1]
			c.sleepers = c.sleepers[:len(c.sleepers)-1]
		}
	}
}
