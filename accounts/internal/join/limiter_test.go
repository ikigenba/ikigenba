package join_test

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/join"
)

func allow(t *testing.T, limiter *join.Limiter, key string, want bool, wait time.Duration) {
	t.Helper()
	if got, d := limiter.Allow(key); got != want || d != wait {
		t.Fatalf("Allow(%q) = %v, %v; want %v, %v", key, got, d, want, wait)
	}
}

func drain(t *testing.T, limiter *join.Limiter, key string) {
	t.Helper()
	for range join.Burst {
		allow(t, limiter, key, true, 0)
	}
}

// R-1I5E-OJKW R-H8AZ-1M14 R-1KL7-G32A
func TestLimiterRefill(t *testing.T) {
	makeLimiter := signature[func(func() time.Time) *join.Limiter](join.NewLimiter)
	type text string
	type count int
	const path, tokenParam text = join.Path, join.TokenParam
	const burst, addresses, maxBody count = join.Burst, join.MaxAddresses, join.MaxBodyBytes
	const every = join.Every
	if path != "/join" || tokenParam != "token" || burst != 10 || any(every) != 6*time.Second || addresses != 4096 || maxBody != 4096 {
		t.Fatal("enrollment route, query parameter or limits changed")
	}
	var now time.Time
	limiter := makeLimiter(func() time.Time { return now })
	const a, b = "192.0.2.7", "192.0.2.8"
	drain(t, limiter, a)
	allow(t, limiter, a, false, 6*time.Second)
	now = now.Add(3 * time.Second)
	allow(t, limiter, a, false, 3*time.Second)
	allow(t, limiter, b, true, 0)
	now = now.Add(3 * time.Second)
	allow(t, limiter, a, true, 0)
	allow(t, limiter, a, false, 6*time.Second)
	now = now.Add(60 * time.Second)
	drain(t, limiter, a)
	allow(t, limiter, a, false, 6*time.Second)
}

// R-SZTB-UBVW
func TestLimiterEvictsLeastRecentlyCalled(t *testing.T) {
	limiter := join.NewLimiter(func() time.Time { return time.Time{} })
	const a = "address-A"
	drain(t, limiter, a)
	drain(t, limiter, "0")
	for i := 1; i < join.MaxAddresses-2; i++ {
		allow(t, limiter, strconv.Itoa(i), true, 0)
	}
	allow(t, limiter, a, false, join.Every)
	allow(t, limiter, "fill", true, 0)
	allow(t, limiter, "evict", true, 0)
	allow(t, limiter, a, false, join.Every)
	drain(t, limiter, "0")
	allow(t, limiter, "0", false, join.Every)

	limiter = join.NewLimiter(func() time.Time { return time.Time{} })
	drain(t, limiter, a)
	for i := range join.MaxAddresses - 1 {
		allow(t, limiter, strconv.Itoa(i), true, 0)
	}
	allow(t, limiter, a, false, join.Every)
	for i := range join.MaxAddresses {
		allow(t, limiter, "next-"+strconv.Itoa(i), true, 0)
	}
	drain(t, limiter, a)
	allow(t, limiter, a, false, join.Every)
}

// R-1N10-7MJO
func TestLimiterConcurrent(t *testing.T) {
	limiter := join.NewLimiter(func() time.Time { return time.Unix(1, 0) })
	var admitted atomic.Int32
	var group sync.WaitGroup
	for range 100 {
		group.Go(func() {
			if ok, _ := limiter.Allow("192.0.2.7"); ok {
				admitted.Add(1)
			}
		})
	}
	group.Wait()
	if got := admitted.Load(); got != join.Burst {
		t.Fatalf("admitted %d, want %d", got, join.Burst)
	}
}
