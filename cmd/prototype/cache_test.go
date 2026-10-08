package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richard-mauri/marine-weather-visualization/geo"
	"github.com/richard-mauri/marine-weather-visualization/sst"
)

func TestCacheCoalescingAndStale(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	c := newGridStore(func(ctx context.Context, b geo.Bounds) (sst.Grid, error) {
		calls.Add(1)
		time.Sleep(30 * time.Millisecond)
		if fail.Load() {
			return sst.Grid{}, errors.New("ERDDAP returned HTTP 503")
		}
		return sst.Grid{Timestamp: "test", Points: []sst.Point{{Lat: 37.5, Lon: -122.5, Temp: 19}}}, nil
	})
	b := geo.Bounds{West: -122.6, South: 37.4, East: -122.3, North: 37.7}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := c.get(context.Background(), b, false)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("upstream calls=%d, want 1", n)
	}
	if _, status, err := c.get(context.Background(), b, false); err != nil || status != "HIT" {
		t.Fatalf("cache hit status=%q err=%v", status, err)
	}
	fail.Store(true)
	g, status, err := c.get(context.Background(), b, true)
	if err != nil || status != "STALE" || g.Timestamp != "test" {
		t.Fatalf("fallback status=%q err=%v grid=%+v", status, err, g)
	}
}
func TestNearbyViewUsesSameBucket(t *testing.T) {
	a := bucketBounds(geo.Bounds{West: -122.51, East: -122.43, South: 37.71, North: 37.77})
	b := bucketBounds(geo.Bounds{West: -122.50, East: -122.42, South: 37.72, North: 37.78})
	if a != b {
		t.Fatalf("nearby bounds differ: %+v %+v", a, b)
	}
}

func TestCooldownAndCounters(t *testing.T) {
	var calls atomic.Int32
	c := newGridStore(func(ctx context.Context, b geo.Bounds) (sst.Grid, error) {
		calls.Add(1)
		return sst.Grid{}, errors.New("ERDDAP returned HTTP 503")
	})
	b := geo.Bounds{West: -122.6, South: 37.4, East: -122.3, North: 37.7}
	_, _, err := c.get(context.Background(), b, false)
	if err == nil || calls.Load() != 3 {
		t.Fatalf("first fetch err=%v calls=%d", err, calls.Load())
	}
	_, state, err := c.get(context.Background(), b, false)
	if state != "COOLDOWN" || err == nil || calls.Load() != 3 {
		t.Fatalf("cooldown state=%s err=%v calls=%d", state, err, calls.Load())
	}
	stat := c.stats()
	if stat["cooldown_keys"].(int) != 1 || stat["max_upstream"].(int) != 2 || stat["retries"].(uint64) != 2 {
		t.Fatalf("bad stats: %v", stat)
	}
}

func TestDatedCacheIsolation(t *testing.T) {
	var count atomic.Int32
	c := newGridStore(func(ctx context.Context, b geo.Bounds) (sst.Grid, error) {
		count.Add(1)
		day, _ := ctx.Value(dateContextKey{}).(string)
		return sst.Grid{Timestamp: day, Points: []sst.Point{{Lat: 37.5, Lon: -122.5, Temp: 18}}}, nil
	})
	b := geo.Bounds{West: -122.6, South: 37.4, East: -122.3, North: 37.7}
	for _, day := range []string{"2026-09-20", "2026-09-21", "2026-09-20"} {
		g, _, err := c.getDated(context.Background(), b, false, day)
		if err != nil || g.Timestamp != day {
			t.Fatalf("date %s returned %q: %v", day, g.Timestamp, err)
		}
	}
	if count.Load() != 2 {
		t.Fatalf("upstream calls=%d want 2", count.Load())
	}
}
