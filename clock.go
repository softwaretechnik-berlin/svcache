package svcache

import (
	"sync"
	"time"
)

// clock abstracts the ability to get the current time
type clock interface {
	Now() time.Time
}

// systemClock is a clock implementation that use time.Now() to get the current time
type systemClock struct{}

var _ clock = systemClock{}

func (c systemClock) Now() time.Time {
	return time.Now()
}

// manualClock is a clock implementation for use in tests that must be manually advanced
type manualClock struct {
	mutex sync.Mutex
	now   time.Time
}

var _ clock = (*manualClock)(nil)

func newManualClock() *manualClock {
	return &manualClock{
		now: time.Date(2021, 1, 9, 14, 3, 0, 0, time.UTC),
	}
}

func (c *manualClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *manualClock) Advance(duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = c.now.Add(duration)
}
