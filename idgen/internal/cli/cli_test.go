package cli

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/idgen/internal/idgen"
)

type fakeClock struct {
	now        time.Time
	nowCalls   int
	sleepCalls int
}

func (c *fakeClock) Now() time.Time {
	c.nowCalls++
	return c.now
}

func (c *fakeClock) Sleep(time.Duration) {
	c.sleepCalls++
}

type advancingClock struct {
	now      time.Time
	reported []time.Time
	sleeps   []time.Duration
}

func (c *advancingClock) Now() time.Time {
	c.reported = append(c.reported, c.now)
	return c.now
}

func (c *advancingClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func (c *advancingClock) totalSleep() time.Duration {
	var total time.Duration
	for _, d := range c.sleeps {
		total += d
	}
	return total
}

type backwardClock struct {
	now      time.Time
	nowCalls int
	reported []time.Time
	sleeps   []time.Duration
}

func (c *backwardClock) Now() time.Time {
	c.nowCalls++
	if c.nowCalls == 2 {
		c.now = c.now.Add(-3 * time.Millisecond)
	}
	c.reported = append(c.reported, c.now)
	return c.now
}

func (c *backwardClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func runMint(t *testing.T, args []string, clock Clock) ([]string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run(args, bytes.NewReader(nil), &stdout, &stderr, clock)
	output := stdout.String()
	if output == "" || !strings.HasSuffix(output, "\n") {
		t.Fatalf("stdout = %q, want one or more newline-terminated id lines", output)
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			t.Fatalf("stdout line %d is empty in %q", i, output)
		}
	}
	return lines, stderr.String(), exitCode
}

func decodedTimes(t *testing.T, ids []string) []time.Time {
	t.Helper()
	times := make([]time.Time, len(ids))
	for i, id := range ids {
		instant, err := idgen.TimeOf(id)
		if err != nil {
			t.Fatalf("TimeOf(output[%d]) error = %v", i, err)
		}
		times[i] = instant
	}
	return times
}

// R-SGRW-J1VR: Run returns its exit code directly to the caller.
func TestRunReturnsExitCodeInProcess(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}

	got := Run(nil, bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{}, clock)

	if got != exitSuccess {
		t.Fatalf("Run() exit code = %d, want %d", got, exitSuccess)
	}
}

// R-TA1H-PJOF: a bare invocation mints one newline-terminated default-prefix id.
func TestRunBareInvocationMintsOneDefaultID(t *testing.T) {
	instant := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: instant}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	gotExit := Run([]string{}, bytes.NewReader(nil), &stdout, &stderr, clock)

	if gotExit != exitSuccess {
		t.Errorf("Run() exit code = %d, want %d", gotExit, exitSuccess)
	}
	if got, want := stdout.String(), idgen.MintAt("R", instant)+"\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if !regexp.MustCompile(`^R-[0-9A-Z]{4}-[0-9A-Z]{4}\n$`).MatchString(stdout.String()) {
		t.Errorf("stdout = %q, want exactly one default-prefix id line", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	if clock.nowCalls != 1 {
		t.Errorf("Clock.Now calls = %d, want 1", clock.nowCalls)
	}
	if clock.sleepCalls != 0 {
		t.Errorf("Clock.Sleep calls = %d, want 0", clock.sleepCalls)
	}
}

// R-SU6S-QJ1E: an advancing fake clock mints the requested distinct ids without wall time.
func TestRunNumberMintsRequestedDistinctIDsWithVirtualClock(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
	ids, stderr, exitCode := runMint(t, []string{"-n", "4"}, clock)

	if exitCode != exitSuccess {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if len(ids) != 4 {
		t.Fatalf("output count = %d, want 4", len(ids))
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			t.Errorf("output id %q is duplicated", id)
		}
		seen[id] = struct{}{}
	}
}

// R-SVEP-4AS3: minting N ids advances virtual time by at least N-1 milliseconds.
func TestRunNumberAdvancesVirtualTimeForEachAdditionalID(t *testing.T) {
	start := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	clock := &advancingClock{now: start}
	ids, _, exitCode := runMint(t, []string{"--number", "5"}, clock)

	if exitCode != exitSuccess {
		t.Fatalf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if len(ids) != 5 {
		t.Fatalf("output count = %d, want 5", len(ids))
	}
	if got, want := clock.totalSleep(), 4*time.Millisecond; got < want {
		t.Errorf("virtual advance = %s, want at least %s", got, want)
	}
	if got, want := clock.now.Sub(start), 4*time.Millisecond; got < want {
		t.Errorf("clock advance = %s, want at least %s", got, want)
	}
}

// R-SWML-I2IS: a clock stalled until Sleep still yields a sufficiently separated sequence.
func TestRunNumberTerminatesWhenClockAdvancesOnlyDuringSleep(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
	ids, _, exitCode := runMint(t, []string{"-n", "6"}, clock)

	if exitCode != exitSuccess {
		t.Fatalf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if len(ids) != 6 {
		t.Fatalf("output count = %d, want 6", len(ids))
	}
	times := decodedTimes(t, ids)
	seen := make(map[int64]struct{}, len(times))
	for _, instant := range times {
		millisecond := instant.UnixMilli()
		if _, duplicate := seen[millisecond]; duplicate {
			t.Errorf("decoded millisecond %d is duplicated", millisecond)
		}
		seen[millisecond] = struct{}{}
	}
	if got, want := times[len(times)-1].Sub(times[0]), 5*time.Millisecond; got < want {
		t.Errorf("decoded last-first = %s, want at least %s", got, want)
	}
}

// R-SZ2E-9M06: the default single mint never sleeps.
func TestRunDefaultSingleMintDoesNotSleep(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
	ids, _, exitCode := runMint(t, nil, clock)

	if exitCode != exitSuccess {
		t.Fatalf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if len(ids) != 1 {
		t.Fatalf("output count = %d, want 1", len(ids))
	}
	if clock.sleepCalls != 0 {
		t.Errorf("Clock.Sleep calls = %d, want 0", clock.sleepCalls)
	}
}

// R-T0AA-NDQV: every minted millisecond was previously reported by the injected clock.
func TestRunMintsOnlyAtClockReportedInstants(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 731000, time.UTC)}
	ids, _, exitCode := runMint(t, []string{"--number", "4"}, clock)

	if exitCode != exitSuccess {
		t.Fatalf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	reportedMilliseconds := make(map[int64]struct{}, len(clock.reported))
	for _, instant := range clock.reported {
		reportedMilliseconds[instant.UnixMilli()] = struct{}{}
	}
	for i, instant := range decodedTimes(t, ids) {
		if _, reported := reportedMilliseconds[instant.UnixMilli()]; !reported {
			t.Errorf("output[%d] decoded millisecond %d was never reported by Clock.Now", i, instant.UnixMilli())
		}
	}
}

// R-T1I7-15HK: the loop sleeps through a backward step and resumes above its prior value.
func TestRunNumberWaitsOutBackwardClockStep(t *testing.T) {
	start := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	clock := &backwardClock{now: start}
	ids, _, exitCode := runMint(t, []string{"-n", "3"}, clock)

	if exitCode != exitSuccess {
		t.Fatalf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if len(ids) != 3 {
		t.Fatalf("output count = %d, want 3", len(ids))
	}
	times := decodedTimes(t, ids)
	for i := 1; i < len(times); i++ {
		if times[i].UnixMilli() <= times[i-1].UnixMilli() {
			t.Errorf("decoded milliseconds are not strictly increasing at %d: %d then %d", i, times[i-1].UnixMilli(), times[i].UnixMilli())
		}
	}
	if times[len(times)-1].UnixMilli() <= start.UnixMilli() {
		t.Errorf("last decoded millisecond = %d, want greater than pre-step %d", times[len(times)-1].UnixMilli(), start.UnixMilli())
	}
	if len(clock.sleeps) == 0 {
		t.Error("Clock.Sleep was not called while waiting out backward step")
	}
}
