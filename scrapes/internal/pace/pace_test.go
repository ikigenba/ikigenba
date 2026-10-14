package pace_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scrapes/internal/pace"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

func fixedNow() time.Time { return time.Unix(0, 0) }

func phase(budget int64, pauses *[]time.Duration) *pace.Phase {
	return pace.Start(pace.Config{Budget: budget, Now: fixedNow, Sleep: func(_ context.Context, d time.Duration) { *pauses = append(*pauses, d) }})
}

// R-R7IF-1KVL R-R8QB-FCMA R-R9Y7-T4CZ R-RCE0-KNUD R-3UAD-HL7S
func TestPublicPhaseAndBudget(t *testing.T) {
	for _, tt := range []struct {
		name  string
		bytes []int64
		took  []time.Duration
		want  []time.Duration
	}{
		{"successive", []int64{500, 1000}, []time.Duration{0, 0}, []time.Duration{500 * time.Millisecond, time.Second}},
		{"write-duration", []int64{1000}, []time.Duration{400 * time.Millisecond}, []time.Duration{600 * time.Millisecond}},
		{"round-down", []int64{1}, []time.Duration{0}, []time.Duration{time.Second / 1000}},
		{"exact-time", []int64{1000}, []time.Duration{time.Second}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var pauses []time.Duration
			p := phase(1000, &pauses)
			var step store.Step = p.Step
			for i, n := range tt.bytes {
				if err := step(context.Background(), n, tt.took[i]); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(pauses, tt.want) {
				t.Fatalf("pauses = %v, want %v", pauses, tt.want)
			}
			b, _, _ := p.Totals()
			var want int64
			for _, n := range tt.bytes {
				want += n
			}
			if b != want {
				t.Fatalf("bytes = %d, want %d", b, want)
			}
		})
	}
	var pauses []time.Duration
	p := phase(3, &pauses)
	if err := p.Step(context.Background(), 1, 0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pauses, []time.Duration{333333333 * time.Nanosecond}) {
		t.Fatalf("fractional expected time = %v", pauses)
	}
}

// R-RDLW-YFL2 R-RETT-C7BR R-3VI9-VCYH
func TestLatencyBackoffAndRestore(t *testing.T) {
	if pace.FloorDivisor != 16 || pace.RestoreDivisor != 10 {
		t.Fatal("backoff divisors differ from contract")
	}
	for _, tt := range []struct {
		name   string
		budget int64
		bytes  []int64
		took   []time.Duration
		want   []time.Duration
	}{
		{"restore", 1000, []int64{1000, 1000, 600, 700, 800, 900, 1000, 1000}, []time.Duration{2 * time.Second, 0, 0, 0, 0, 0, 0, 0}, []time.Duration{2 * time.Second, time.Second, time.Second, time.Second, time.Second, time.Second, time.Second}},
		{"equal-time-restores", 1000, []int64{1000, 1000, 600}, []time.Duration{2 * time.Second, 2 * time.Second, 0}, []time.Duration{time.Second}},
		{"floor", 1600, []int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 100}, []time.Duration{time.Second, time.Second, time.Second, time.Second, time.Second, time.Second, time.Second, time.Second, time.Second, time.Second, 0}, []time.Duration{time.Second}},
		{"minimum-restore", 3, []int64{3, 1, 2, 3}, []time.Duration{2 * time.Second, 0, 0, 0}, []time.Duration{time.Second, time.Second, time.Second}},
		{"minimum-floor", 1, []int64{1, 1, 1}, []time.Duration{2 * time.Second, 2 * time.Second, 0}, []time.Duration{time.Second}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var pauses []time.Duration
			p := phase(tt.budget, &pauses)
			for i, n := range tt.bytes {
				if err := p.Step(context.Background(), n, tt.took[i]); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(pauses, tt.want) {
				t.Fatalf("pauses = %v, want %v", pauses, tt.want)
			}
		})
	}
}

// R-3WQ6-94P6
func TestDisabledBudget(t *testing.T) {
	for _, b := range []int64{0, -3} {
		var pauses []time.Duration
		p := phase(b, &pauses)
		for _, took := range []time.Duration{0, 2 * time.Second} {
			if err := p.Step(context.Background(), 1000, took); err != nil {
				t.Fatal(err)
			}
		}
		if len(pauses) != 0 {
			t.Fatalf("disabled budget %d pauses = %v", b, pauses)
		}
	}
}

func writePressure(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// R-RG1P-PZ2G R-3XY2-MWFV
func TestPressureAndPauseOrder(t *testing.T) {
	if pace.PressurePoll != 5*time.Second {
		t.Fatal("pressure poll differs from contract")
	}
	for _, budget := range []int64{0, 1000} {
		path := filepath.Join(t.TempDir(), "pressure")
		writePressure(t, path, "some avg10=99.00 avg60=0.00\nfull avg10=50.00 avg60=0.00\n")
		var pauses []time.Duration
		polls := 0
		p := pace.Start(pace.Config{Budget: budget, Limit: 10, Pressure: path, Now: fixedNow, Sleep: func(_ context.Context, d time.Duration) {
			pauses = append(pauses, d)
			if d == pace.PressurePoll {
				polls++
				if polls == 2 {
					writePressure(t, path, "full avg10=0.00\n")
				}
			}
		}})
		if err := p.Step(context.Background(), 1000, 0); err != nil {
			t.Fatal(err)
		}
		want := []time.Duration{pace.PressurePoll, pace.PressurePoll}
		if budget > 0 {
			want = append([]time.Duration{time.Second}, want...)
		}
		if !reflect.DeepEqual(pauses, want) {
			t.Fatalf("pauses = %v, want %v", pauses, want)
		}
	}
	for _, body := range []string{"full avg10=10.00\n", "full avg10=9.99\n", "some avg10=99.00\n", "full avg10=bad\n", "full avg60=99.00\n", "full avg10=NaN\n", "fuller avg10=99.00\n"} {
		path := filepath.Join(t.TempDir(), "pressure")
		writePressure(t, path, body)
		p := pace.Start(pace.Config{Limit: 10, Pressure: path, Now: fixedNow, Sleep: func(context.Context, time.Duration) { t.Fatalf("unexpected pressure pause for %q", body) }})
		if err := p.Step(context.Background(), 1000, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		p := pace.Start(pace.Config{Pressure: path, Now: fixedNow, Sleep: func(context.Context, time.Duration) { t.Fatal("unexpected pressure pause") }})
		if err := p.Step(context.Background(), 1, 0); err != nil {
			t.Fatal(err)
		}
	}
}

// R-3Z5Z-0O6K
func TestCanceledPause(t *testing.T) {
	for _, budget := range []int64{0, 1000} {
		t.Run(time.Duration(budget).String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pressure")
			writePressure(t, path, "full avg10=50.00\n")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan context.Context, 1)
			calls := 0
			p := pace.Start(pace.Config{Budget: budget, Limit: 10, Pressure: path, Now: fixedNow, Sleep: func(c context.Context, _ time.Duration) {
				calls++
				entered <- c
				<-c.Done()
			}})
			result := make(chan error, 1)
			go func() { result <- p.Step(ctx, 1000, 0) }()
			select {
			case c := <-entered:
				if c != ctx {
					t.Fatal("pause context changed")
				}
			case <-time.After(time.Second):
				t.Fatal("pause did not begin")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("result = %v, calls = %d", err, calls)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled pause did not return")
			}
		})
	}
}

// R-40DV-EFX9
func TestTotalsUseClockAndRequestedPauses(t *testing.T) {
	now := time.Unix(50, 0)
	path := filepath.Join(t.TempDir(), "pressure")
	writePressure(t, path, "full avg10=50.00\n")
	p := pace.Start(pace.Config{Budget: 1000, Limit: 10, Pressure: path, Now: func() time.Time { return now }, Sleep: func(_ context.Context, d time.Duration) {
		if d == pace.PressurePoll {
			writePressure(t, path, "full avg10=0.00\n")
		}
	}})
	for _, n := range []int64{500, 1000} {
		if err := p.Step(context.Background(), n, 0); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(7 * time.Second)
	b, elapsed, paused := p.Totals()
	if b != 1500 || elapsed != 7*time.Second || paused != 1500*time.Millisecond+pace.PressurePoll {
		t.Fatalf("totals = %d, %v, %v", b, elapsed, paused)
	}
	p = pace.Start(pace.Config{Budget: 1000, Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) {}})
	for _, n := range []int64{500, 1000} {
		if err := p.Step(context.Background(), n, 0); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(7 * time.Second)
	b, elapsed, paused = p.Totals()
	if b != 1500 || elapsed != 7*time.Second || paused != 1500*time.Millisecond {
		t.Fatalf("budget-only totals = %d, %v, %v", b, elapsed, paused)
	}
}
