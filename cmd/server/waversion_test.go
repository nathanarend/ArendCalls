package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/store"
)

func TestWAVersionNewer(t *testing.T) {
	cases := []struct {
		a, b store.WAVersionContainer
		want bool
	}{
		{store.WAVersionContainer{2, 3000, 2}, store.WAVersionContainer{2, 3000, 1}, true},
		{store.WAVersionContainer{2, 3000, 1}, store.WAVersionContainer{2, 3000, 2}, false},
		{store.WAVersionContainer{2, 3000, 1}, store.WAVersionContainer{2, 3000, 1}, false},
		{store.WAVersionContainer{2, 3001, 0}, store.WAVersionContainer{2, 3000, 9}, true},
	}
	for _, c := range cases {
		if got := waVersionNewer(c.a, c.b); got != c.want {
			t.Errorf("waVersionNewer(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNextLocalHour(t *testing.T) {
	loc := time.FixedZone("BRT", -3*3600)
	cases := []struct {
		now, want time.Time
	}{
		{time.Date(2026, 10, 27, 1, 0, 0, 0, loc), time.Date(2026, 10, 27, 3, 0, 0, 0, loc)},
		{time.Date(2026, 10, 27, 3, 0, 0, 0, loc), time.Date(2026, 10, 28, 3, 0, 0, 0, loc)},
		{time.Date(2026, 10, 31, 10, 0, 0, 0, loc), time.Date(2026, 11, 1, 3, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := nextLocalHour(c.now, 3); !got.Equal(c.want) {
			t.Errorf("nextLocalHour(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}

func TestQueueReconnectSerial(t *testing.T) {
	var running, maxRunning int32
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queueReconnect(context.Background(), &Session{}, func() bool {
				n := atomic.AddInt32(&running, 1)
				for {
					m := atomic.LoadInt32(&maxRunning)
					if n <= m || atomic.CompareAndSwapInt32(&maxRunning, m, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&running, -1)
				return false
			})
		}()
	}
	wg.Wait()
	if maxRunning != 1 {
		t.Fatalf("reconnects overlapped: max %d at once", maxRunning)
	}
}
