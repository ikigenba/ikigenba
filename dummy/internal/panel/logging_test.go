package panel

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

type panelLogWriter struct {
	mu         sync.Mutex
	writes     []string
	inProgress atomic.Int32
	overlapped atomic.Bool
}

func (w *panelLogWriter) Write(p []byte) (int, error) {
	if w.inProgress.Add(1) != 1 {
		w.overlapped.Store(true)
	}
	for range 16 {
		runtime.Gosched()
	}
	w.mu.Lock()
	w.writes = append(w.writes, string(p))
	w.mu.Unlock()
	w.inProgress.Add(-1)
	return len(p), nil
}

func (w *panelLogWriter) entries() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.writes)
}

// R-MM92-HZAL R-Y1D9-4V24
func TestPanelRequestDiagnostics(t *testing.T) {
	cases := []struct {
		method, path, userID, requestID, want string
	}{
		{"GET", "/widgets", "", "trace-42", "dummy: request trace-42: X-User-Id is missing\n"},
		{"HEAD", "/widgets/table", "", "", "dummy: request -: X-User-Id is missing\n"},
		{"POST", "/unknown", "", "trace-post", "dummy: request trace-post: X-User-Id is missing\n"},
		{"GET", "/widgets", "reader", "healthy", ""},
		{"GET", "/missing", "reader", "not-found", ""},
		{"PUT", "/widgets", "reader", "wrong-method", ""},
		{"POST", "/widgets", "reader", "wrong-media", ""},
	}
	for _, tc := range cases {
		t.Run(tc.requestID+tc.method, func(t *testing.T) {
			log := &panelLogWriter{}
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.userID != "" {
				r.Header.Set("X-User-Id", tc.userID)
			} else if tc.requestID == "trace-post" {
				r.Header.Set("X-User-Id", "")
			}
			if tc.requestID != "" {
				r.Header.Set("X-Request-Id", tc.requestID)
			} else {
				r.Header.Set("X-Request-Id", "")
			}
			w := httptest.NewRecorder()
			Handler(widget.NewStore(), log).ServeHTTP(w, r)
			entries := log.entries()
			if tc.want == "" {
				if w.Code >= 500 || len(entries) != 0 {
					t.Fatalf("status %d wrote %q", w.Code, entries)
				}
				return
			}
			if w.Code != http.StatusInternalServerError || len(entries) != 1 || entries[0] != tc.want {
				t.Fatalf("status %d, writes %q; want one %q", w.Code, entries, tc.want)
			}
		})
	}
}

// R-MOOV-9IRZ
func TestPanelDiagnosticsSerializeWrites(t *testing.T) {
	const requests = 64
	log := &panelLogWriter{}
	h := Handler(widget.NewStore(), log)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := httptest.NewRequest(http.MethodGet, "/widgets", nil)
			r.Header.Set("X-Request-Id", strconv.Itoa(i))
			h.ServeHTTP(httptest.NewRecorder(), r)
		}()
	}
	close(start)
	wg.Wait()
	if log.overlapped.Load() {
		t.Fatal("concurrent stderr.Write calls")
	}
	entries := log.entries()
	if len(entries) != requests {
		t.Fatalf("got %d writes, want %d", len(entries), requests)
	}
	slices.Sort(entries)
	want := make([]string, requests)
	for i := range want {
		want[i] = "dummy: request " + strconv.Itoa(i) + ": X-User-Id is missing\n"
	}
	slices.Sort(want)
	if !slices.Equal(entries, want) {
		t.Fatalf("writes %q, want %q", entries, want)
	}
}
