package cli

import (
	"bytes"
	"regexp"
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
