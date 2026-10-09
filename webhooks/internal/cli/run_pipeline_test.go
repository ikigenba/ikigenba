package cli_test

import (
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

type runGuardRandom struct {
	active  atomic.Int64
	overlap atomic.Bool
	reads   atomic.Int64
	mu      sync.Mutex
	n       byte
}

func (r *runGuardRandom) Read(b []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	r.reads.Add(1)
	for i := range b {
		runtime.Gosched()
		r.mu.Lock()
		r.n++
		b[i] = r.n
		r.mu.Unlock()
	}
	return len(b), nil
}

// R-WTY6-NEDD
func TestRunSerializesRandomDuringConcurrentRequests(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	random := &runGuardRandom{}
	h.p.Rand = random
	h.start()
	minted := h.create("tick")
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			<-start
			if i%2 == 0 {
				h.get("/")
				return
			}
			if code := h.post("tick", minted.Secret, []byte("concurrent")); code != http.StatusAccepted {
				t.Errorf("post %d", code)
			}
		})
	}
	close(start)
	group.Wait()
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	if random.overlap.Load() || random.reads.Load() < 20 {
		t.Fatalf("random concurrent=%v reads=%d", random.overlap.Load(), random.reads.Load())
	}
}
