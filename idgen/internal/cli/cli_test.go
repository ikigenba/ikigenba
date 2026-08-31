package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/ikigenba/ikigenba/idgen/internal/idgen"
)

const expectedVersionOutput = "v0.1.0\n"

type fakeClock struct {
	now        time.Time
	nowCalls   int
	sleepCalls int
}

type forbiddenReader struct {
	t *testing.T
}

type failingWriter struct {
	writes int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, io.ErrClosedPipe
}

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Helper()
	r.t.Fatal("stdin was read during positional decode")
	return 0, nil
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

type scheduledClock struct {
	now       time.Time
	scheduled []time.Time
	sleeps    []time.Duration
}

func (c *scheduledClock) Now() time.Time {
	if len(c.scheduled) > 0 {
		c.now = c.scheduled[0]
		c.scheduled = c.scheduled[1:]
	}
	return c.now
}

func (c *scheduledClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func runMint(args []string, clock Clock) (string, string, int) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run(args, bytes.NewReader(nil), &stdout, &stderr, clock)
	return stdout.String(), stderr.String(), exitCode
}

func assertMintLines(t *testing.T, output string) []string {
	t.Helper()
	if output == "" || !strings.HasSuffix(output, "\n") {
		t.Fatalf("stdout = %q, want one or more newline-terminated id lines", output)
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			t.Fatalf("stdout line %d is empty in %q", i, output)
		}
	}
	return lines
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

func runCLI(args []string, stdin string, clock Clock) (string, string, int) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(args, strings.NewReader(stdin), &stdout, &stderr, clock)
	return stdout.String(), stderr.String(), exitCode
}

func prefixAgreementSample(seed uint64) []string {
	state := seed
	nextRandom := func() uint64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return state
	}
	const validCharacters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	punctuation := []string{"_", "!", ".", "@", "#"}
	nonASCII := []string{"é", "１", "界", "🙂"}

	randomValidRun := func() string {
		length := 1 + int(nextRandom()%16)
		var prefix strings.Builder
		prefix.Grow(length)
		for range length {
			prefix.WriteByte(validCharacters[nextRandom()%62])
		}
		return prefix.String()
	}

	candidates := []string{""}
	for range 32 {
		valid := randomValidRun()
		candidates = append(candidates,
			valid,
			valid+"-",
			valid+punctuation[nextRandom()%5],
			valid+nonASCII[nextRandom()%4],
		)
	}
	return candidates
}

func decodedLine(instant time.Time) string {
	return instant.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z") + "\n"
}

const expectedUsage = `Usage: idgen [options] [ID ...]

Mint an identifier using the current time by default.

Options:
  -n, --number N       mint N identifiers (default 1)
  -p, --prefix PREFIX  use PREFIX (default "R")
      --decode         decode ID arguments, or whitespace-delimited IDs from stdin
  -h, --help           print this help
  -V, --version        print version
`

// R-T2Q3-EX89: both explicit help aliases print the usage block once and do no other work.
func TestRunHelpAliasesPrintUsageExactlyOnce(t *testing.T) {
	for _, alias := range []string{"--help", "-h"} {
		t.Run(alias, func(t *testing.T) {
			clock := &fakeClock{}
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := Run([]string{alias}, forbiddenReader{t: t}, &stdout, &stderr, clock)

			if exitCode != exitSuccess {
				t.Errorf("Run(%q) exit code = %d, want %d", alias, exitCode, exitSuccess)
			}
			if got := stdout.String(); got != expectedUsage {
				t.Errorf("stdout = %q, want exact usage %q", got, expectedUsage)
			}
			if got := strings.Count(stdout.String(), expectedUsage); got != 1 {
				t.Errorf("usage block count = %d, want 1", got)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if clock.nowCalls != 0 || clock.sleepCalls != 0 {
				t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
			}
		})
	}
}

// R-TOOA-ASKR: help output is byte-for-byte the specified usage block.
func TestRunUsageTextIsExact(t *testing.T) {
	stdout, stderr, exitCode := runCLI([]string{"--help"}, "", &fakeClock{})

	if stdout != expectedUsage {
		t.Errorf("stdout = %q, want exact independently declared usage block %q", stdout, expectedUsage)
	}
	if stderr != "" || exitCode != exitSuccess {
		t.Errorf("Run() = (stderr %q, exit %d), want (empty, %d)", stderr, exitCode, exitSuccess)
	}
}

// R-TPW6-OKBG: the returned usage block names every supported option spelling.
func TestRunUsageTextMentionsEveryOption(t *testing.T) {
	stdout, stderr, exitCode := runCLI([]string{"-h"}, "", &fakeClock{})
	if stderr != "" || exitCode != exitSuccess {
		t.Fatalf("Run() = (stderr %q, exit %d), want (empty, %d)", stderr, exitCode, exitSuccess)
	}

	for _, option := range []string{"-n", "--number", "-p", "--prefix", "--decode", "-h", "--help", "-V", "--version"} {
		if !strings.Contains(stdout, option) {
			t.Errorf("usage output does not mention %q: %q", option, stdout)
		}
	}
}

// R-T3XZ-SOYY: --version prints the source-carried version and exits successfully.
func TestRunLongVersionPrintsVersion(t *testing.T) {
	clock := &fakeClock{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"--version"}, forbiddenReader{t: t}, &stdout, &stderr, clock)

	if exitCode != exitSuccess || stdout.String() != expectedVersionOutput || stderr.Len() != 0 {
		t.Errorf("Run() = (stdout %q, stderr %q, exit %d), want (%q, empty, %d)", stdout.String(), stderr.String(), exitCode, expectedVersionOutput, exitSuccess)
	}
	if clock.nowCalls != 0 || clock.sleepCalls != 0 {
		t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
	}
}

// R-TR43-2C25: --version output is exactly the v0.1.0 product version line.
func TestRunLongVersionOutputIsExact(t *testing.T) {
	tests := [][]string{
		{"--version"},
		{"--version", "--help", "--help=false"},
	}
	for _, args := range tests {
		stdout, stderr, exitCode := runCLI(args, "", &fakeClock{})

		if stdout != expectedVersionOutput {
			t.Errorf("Run(%q) stdout = %q, want %q", args, stdout, expectedVersionOutput)
		}
		if stderr != "" || exitCode != exitSuccess {
			t.Errorf("Run(%q) = (stderr %q, exit %d), want (empty, %d)", args, stderr, exitCode, exitSuccess)
		}
	}
}

// R-TSBZ-G3SU: -V and --version have identical exact output, errors, and exit codes.
func TestRunVersionAliasesAreIdentical(t *testing.T) {
	longOut, longErr, longExit := runCLI([]string{"--version"}, "", &fakeClock{})
	shortOut, shortErr, shortExit := runCLI([]string{"-V"}, "", &fakeClock{})

	if shortOut != longOut || shortErr != longErr || shortExit != longExit {
		t.Errorf("-V = (%q, %q, %d), want --version result (%q, %q, %d)", shortOut, shortErr, shortExit, longOut, longErr, longExit)
	}
	if shortOut != expectedVersionOutput || shortErr != "" || shortExit != exitSuccess {
		t.Errorf("shared version result = (%q, %q, %d), want (%q, empty, %d)", shortOut, shortErr, shortExit, expectedVersionOutput, exitSuccess)
	}
}

func TestRunInformationalModesIgnoreOutputWriteErrors(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		stdout := &failingWriter{}
		var stderr bytes.Buffer

		exitCode := Run(args, strings.NewReader(""), stdout, &stderr, &fakeClock{})

		if exitCode != exitSuccess {
			t.Errorf("Run(%q) exit code = %d, want %d", args, exitCode, exitSuccess)
		}
		if stdout.writes != 1 {
			t.Errorf("Run(%q) stdout writes = %d, want 1", args, stdout.writes)
		}
		if stderr.Len() != 0 {
			t.Errorf("Run(%q) stderr = %q, want empty", args, stderr.String())
		}
	}
}

// R-T55W-6GPN: an unknown option returns usage failure with one diagnostic and one complete usage block.
func TestRunUnknownOptionPrintsDiagnosticAndUsageExactlyOnce(t *testing.T) {
	clock := &fakeClock{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"--not-an-option"}, forbiddenReader{t: t}, &stdout, &stderr, clock)

	if exitCode != exitUsage {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	const diagnostic = "flag provided but not defined: -not-an-option\n"
	if got, want := stderr.String(), diagnostic+expectedUsage; got != want {
		t.Errorf("stderr = %q, want exactly one diagnostic and usage block %q", got, want)
	}
	if clock.nowCalls != 0 || clock.sleepCalls != 0 {
		t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
	}
}

// R-T6DS-K8GC: mint mode rejects and names its first unexpected positional argument.
func TestRunMintRejectsPositionalArgument(t *testing.T) {
	const unexpected = "definitely-not-an-id"
	clock := &fakeClock{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{unexpected}, forbiddenReader{t: t}, &stdout, &stderr, clock)

	if exitCode != exitUsage {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), unexpected) {
		t.Errorf("stderr = %q, want unexpected argument %q named", stderr.String(), unexpected)
	}
	if !strings.Contains(stderr.String(), expectedUsage) {
		t.Errorf("stderr = %q, want exact usage block %q", stderr.String(), expectedUsage)
	}
	if clock.nowCalls != 0 || clock.sleepCalls != 0 {
		t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
	}
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
	if got, want := stdout.String(), "R-"+"YQT6-50XA"+"\n"; got != want {
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

func TestRunMintReturnsFailureWhenOutputCannotBeWritten(t *testing.T) {
	stdout := &failingWriter{}
	var stderr bytes.Buffer

	exitCode := Run(nil, strings.NewReader(""), stdout, &stderr, &fakeClock{now: time.Unix(0, 0)})

	if exitCode != exitFailure {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitFailure)
	}
	if stdout.writes != 1 {
		t.Errorf("stdout writes = %d, want 1", stdout.writes)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// R-SU6S-QJ1E: an advancing fake clock mints the requested distinct ids without wall time.
func TestRunNumberMintsRequestedDistinctIDsWithVirtualClock(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
	stdout, stderr, exitCode := runMint([]string{"-n", "4"}, clock)
	ids := assertMintLines(t, stdout)

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
	stdout, _, exitCode := runMint([]string{"--number", "5"}, clock)
	ids := assertMintLines(t, stdout)

	if exitCode != exitSuccess {
		t.Fatalf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if len(ids) != 5 {
		t.Fatalf("output count = %d, want 5", len(ids))
	}
	if got, want := clock.totalSleep(), 4*time.Millisecond; got != want {
		t.Errorf("virtual advance = %s, want %s", got, want)
	}
	if got, want := clock.now.Sub(start), 4*time.Millisecond; got != want {
		t.Errorf("clock advance = %s, want %s", got, want)
	}
}

// R-SWML-I2IS: a clock stalled until Sleep still yields a sufficiently separated sequence.
func TestRunNumberTerminatesWhenClockAdvancesOnlyDuringSleep(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
	stdout, _, exitCode := runMint([]string{"-n", "6"}, clock)
	ids := assertMintLines(t, stdout)

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
	if got, want := times[len(times)-1].Sub(times[0]), 5*time.Millisecond; got != want {
		t.Errorf("decoded last-first = %s, want %s", got, want)
	}
}

// R-SZ2E-9M06: the default single mint never sleeps.
func TestRunDefaultSingleMintDoesNotSleep(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
	stdout, _, exitCode := runMint(nil, clock)
	ids := assertMintLines(t, stdout)

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
	stdout, _, exitCode := runMint([]string{"--number", "4"}, clock)
	ids := assertMintLines(t, stdout)

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
	clock := &scheduledClock{scheduled: []time.Time{start, start.Add(-3 * time.Millisecond)}}
	stdout, _, exitCode := runMint([]string{"-n", "3"}, clock)
	ids := assertMintLines(t, stdout)

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

// R-TB9E-3BF4: both prefix aliases fully replace the default prefix for every mint.
func TestRunPrefixAliasesReplaceDefaultPrefix(t *testing.T) {
	instant := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		args       []string
		prefix     string
		wantBodies []string
	}{
		{name: "short alias and one character", args: []string{"-p", "X", "-n", "2"}, prefix: "X", wantBodies: []string{"YQT6-50XA", "YS12-ISNZ"}},
		{name: "long alias and multiple characters", args: []string{"--prefix", "Team42"}, prefix: "Team42", wantBodies: []string{"YQT6-50XA"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &advancingClock{now: instant}
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := Run(test.args, bytes.NewReader(nil), &stdout, &stderr, clock)

			if exitCode != exitSuccess {
				t.Errorf("Run() exit code = %d, want %d", exitCode, exitSuccess)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			var want strings.Builder
			for _, body := range test.wantBodies {
				want.WriteString(test.prefix + "-" + body)
				want.WriteByte('\n')
			}
			if got := stdout.String(); got != want.String() {
				t.Errorf("stdout = %q, want exact newline-terminated output %q", got, want.String())
			}
			pattern := regexp.MustCompile(`^(?:` + regexp.QuoteMeta(test.prefix) + `-[0-9A-Z]{4}-[0-9A-Z]{4}\n){` + strconv.Itoa(len(test.wantBodies)) + `}$`)
			if !pattern.MatchString(stdout.String()) {
				t.Errorf("stdout = %q, want every id to begin with complete prefix %q", stdout.String(), test.prefix)
			}
			for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
				gotPrefix, _, found := strings.Cut(line, "-")
				if !found || gotPrefix != test.prefix {
					t.Errorf("id %q prefix = %q, want complete replacement %q", line, gotPrefix, test.prefix)
				}
				if strings.HasPrefix(line, "R"+test.prefix+"-") {
					t.Errorf("id %q retains and concatenates default prefix R", line)
				}
			}
		})
	}
}

// R-TL0L-5HCO: invalid prefixes fail before consulting the clock or writing stdout.
func TestRunRejectsInvalidPrefixesBeforeMinting(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{name: "empty", prefix: ""},
		{name: "whitespace only", prefix: " \t"},
		{name: "separator", prefix: "bad-prefix"},
		{name: "underscore", prefix: "bad_prefix"},
		{name: "punctuation", prefix: "bad!"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := Run([]string{"--prefix", test.prefix}, bytes.NewReader(nil), &stdout, &stderr, clock)

			if exitCode != exitUsage {
				t.Errorf("Run() exit code = %d, want %d", exitCode, exitUsage)
			}
			if !strings.Contains(stderr.String(), "invalid prefix") {
				t.Errorf("stderr = %q, want invalid prefix diagnostic", stderr.String())
			}
			if !strings.Contains(stderr.String(), expectedUsage) {
				t.Errorf("stderr = %q, want exact usage block %q", stderr.String(), expectedUsage)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if clock.nowCalls != 0 || clock.sleepCalls != 0 {
				t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
			}
		})
	}
}

func TestRunPrefixAcceptanceAgreesWithValidPrefix(t *testing.T) {
	// R-626N-3DAD
	instant := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	const wantBody = "D190-ANLA"
	accepted, rejected := 0, 0
	for _, prefix := range prefixAgreementSample(0x626_3dad) {
		clock := &fakeClock{now: instant}
		stdout, stderr, exitCode := runCLI([]string{"--prefix", prefix}, "", clock)
		if idgen.ValidPrefix(prefix) {
			accepted++
			if exitCode != exitSuccess {
				t.Errorf("valid prefix %q exit code = %d, want %d", prefix, exitCode, exitSuccess)
			}
			if got, want := stdout, prefix+"-"+wantBody+"\n"; got != want {
				t.Errorf("valid prefix %q stdout = %q, want minted id %q", prefix, got, want)
			}
			if strings.Contains(stderr, "invalid prefix") || stderr != "" {
				t.Errorf("valid prefix %q stderr = %q, want empty and no invalid-prefix diagnostic", prefix, stderr)
			}
			continue
		}

		rejected++
		if exitCode != exitUsage {
			t.Errorf("invalid prefix %q exit code = %d, want %d", prefix, exitCode, exitUsage)
		}
		if got, want := stderr, "idgen: invalid prefix\n"+expectedUsage; got != want {
			t.Errorf("invalid prefix %q stderr = %q, want exact diagnostic and usage %q", prefix, got, want)
		}
		if stdout != "" {
			t.Errorf("invalid prefix %q stdout = %q, want no minted id", prefix, stdout)
		}
		if clock.nowCalls != 0 || clock.sleepCalls != 0 {
			t.Errorf("invalid prefix %q clock calls = Now %d, Sleep %d; want zero", prefix, clock.nowCalls, clock.sleepCalls)
		}
	}
	if accepted == 0 || rejected == 0 {
		t.Fatalf("agreement sample exercised %d accepted and %d rejected prefixes, want both branches", accepted, rejected)
	}
}

// R-TM8H-J93D: non-positive numbers fail before consulting the clock or writing stdout.
func TestRunRejectsNonPositiveNumbersBeforeMinting(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "zero through short alias", args: []string{"-n", "0"}},
		{name: "negative through long alias", args: []string{"--number", "-3"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)}
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := Run(test.args, bytes.NewReader(nil), &stdout, &stderr, clock)

			if exitCode != exitUsage {
				t.Errorf("Run() exit code = %d, want %d", exitCode, exitUsage)
			}
			if !strings.Contains(stderr.String(), "--number must be > 0") {
				t.Errorf("stderr = %q, want non-positive number diagnostic", stderr.String())
			}
			if !strings.Contains(stderr.String(), expectedUsage) {
				t.Errorf("stderr = %q, want exact usage block %q", stderr.String(), expectedUsage)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if clock.nowCalls != 0 || clock.sleepCalls != 0 {
				t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
			}
		})
	}
}

// R-T7LO-Y071: --decode routes to decoding without consulting the mint clock.
func TestRunDecodeRoutesAwayFromMinting(t *testing.T) {
	instant := time.Date(2026, time.August, 29, 12, 34, 56, 789123000, time.FixedZone("test", -5*60*60))
	id := idgen.MintAt("Route", instant)
	clock := &fakeClock{now: instant.Add(time.Hour)}

	stdout, stderr, exitCode := runCLI([]string{"--decode", "--version", "--version=false", id}, "", clock)

	if exitCode != exitSuccess {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if got, want := stdout, decodedLine(instant); got != want {
		t.Errorf("stdout = %q, want decoded timestamp %q", got, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if clock.nowCalls != 0 || clock.sleepCalls != 0 {
		t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
	}
}

// R-T8TL-BRXQ: number and prefix flags are accepted but inert in decode mode.
func TestRunMintFlagsDoNotChangeDecodeOutput(t *testing.T) {
	instant := time.Date(2026, time.September, 1, 2, 3, 4, 567890000, time.UTC)
	id := idgen.MintAt("Inert", instant)
	wantStdout := decodedLine(instant)
	tests := []struct {
		name string
		args []string
	}{
		{name: "short aliases", args: []string{"-n", "0", "-p", "bad-prefix", "--decode", id}},
		{name: "long aliases", args: []string{"--number", "-2", "--prefix", "bad_prefix", "--decode", id}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &fakeClock{now: instant.Add(2 * time.Hour)}
			stdout, stderr, exitCode := runCLI(test.args, "", clock)

			if stdout != wantStdout || stderr != "" || exitCode != exitSuccess {
				t.Errorf("Run() = (stdout %q, stderr %q, exit %d), want (%q, empty, %d)", stdout, stderr, exitCode, wantStdout, exitSuccess)
			}
			if clock.nowCalls != 0 || clock.sleepCalls != 0 {
				t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
			}
		})
	}
}

// R-TCHA-H35T: positional ids decode to exact ordered UTC timestamp lines.
func TestRunDecodesPositionalsInOrder(t *testing.T) {
	instants := []time.Time{
		time.Date(2026, time.September, 2, 3, 4, 5, 123456000, time.FixedZone("west", -7*60*60)),
		time.Date(2027, time.November, 30, 22, 21, 20, 987654000, time.FixedZone("east", 9*60*60)),
	}
	ids := []string{idgen.MintAt("First", instants[0]), idgen.MintAt("Second", instants[1])}

	stdout, stderr, exitCode := runCLI(append([]string{"--decode"}, ids...), "", &fakeClock{})

	if exitCode != exitSuccess {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitSuccess)
	}
	if got, want := stdout, decodedLine(instants[0])+decodedLine(instants[1]); got != want {
		t.Errorf("stdout = %q, want exact ordered lines %q", got, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestRunDecodeReturnsFailureWhenOutputCannotBeWritten(t *testing.T) {
	instant := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	token := idgen.MintAt("R", instant)
	stdout := &failingWriter{}
	var stderr bytes.Buffer

	exitCode := Run([]string{"--decode", token}, strings.NewReader(""), stdout, &stderr, &fakeClock{})

	if exitCode != exitFailure {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitFailure)
	}
	if stdout.writes != 1 {
		t.Errorf("stdout writes = %d, want 1", stdout.writes)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunDecodeInvalidDiagnosticAddsOnlyTokenContext(t *testing.T) {
	const token = "broken"

	stdout, stderr, exitCode := runCLI([]string{"--decode", token}, "", &fakeClock{})

	if exitCode != exitFailure {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitFailure)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if got, want := stderr, "idgen: \"broken\": invalid id: non-canonical format\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestRunDecodeReportsWhenInputStopsEarly(t *testing.T) {
	instant := time.Date(2026, time.January, 2, 3, 4, 5, 678000000, time.UTC)
	token := idgen.MintAt("Read", instant)
	stdin := io.MultiReader(strings.NewReader(token+" "), iotest.ErrReader(io.ErrUnexpectedEOF))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"--decode"}, stdin, &stdout, &stderr, &fakeClock{})

	if exitCode != exitFailure {
		t.Errorf("Run() exit code = %d, want %d", exitCode, exitFailure)
	}
	if got, want := stdout.String(), decodedLine(instant); got != want {
		t.Errorf("stdout = %q, want partial decoded output %q", got, want)
	}
	if got, want := stderr.String(), "idgen: reading stdin stopped early: unexpected EOF\n"; got != want {
		t.Errorf("stderr = %q, want explicit truncated-input diagnostic %q", got, want)
	}
}

// R-TDP6-UUWI: mixed-whitespace stdin decoding matches positional decoding.
func TestRunDecodesMixedWhitespaceStdinLikePositionals(t *testing.T) {
	instants := []time.Time{
		time.Date(2026, time.March, 4, 5, 6, 7, 111222000, time.UTC),
		time.Date(2027, time.July, 8, 9, 10, 11, 333444000, time.UTC),
		time.Date(2028, time.December, 13, 14, 15, 16, 555666000, time.UTC),
	}
	ids := []string{idgen.MintAt("A", instants[0]), idgen.MintAt("B", instants[1]), idgen.MintAt("C", instants[2])}
	wantStdout := decodedLine(instants[0]) + decodedLine(instants[1]) + decodedLine(instants[2])
	stdin := " \t" + ids[0] + "\n\n" + ids[1] + " \t\r\n" + ids[2] + "  "

	stdout, stderr, exitCode := runCLI([]string{"--decode"}, stdin, &fakeClock{})

	if stdout != wantStdout || stderr != "" || exitCode != exitSuccess {
		t.Errorf("stdin decode = (stdout %q, stderr %q, exit %d), want (%q, empty, %d)", stdout, stderr, exitCode, wantStdout, exitSuccess)
	}
}

// R-TEX3-8MN7: positional decode does not read stdin.
func TestRunPositionalDecodeNeverReadsStdin(t *testing.T) {
	instant := time.Date(2026, time.October, 2, 3, 4, 5, 678901000, time.UTC)
	id := idgen.MintAt("Position", instant)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"--decode", id}, forbiddenReader{t: t}, &stdout, &stderr, &fakeClock{})

	if exitCode != exitSuccess || stdout.String() != decodedLine(instant) || stderr.Len() != 0 {
		t.Errorf("Run() = (stdout %q, stderr %q, exit %d), want (%q, empty, %d)", stdout.String(), stderr.String(), exitCode, decodedLine(instant), exitSuccess)
	}
}

// R-THCW-064L: malformed tokens are reported while valid tokens keep decoding in order.
func TestRunDecodeContinuesPastMalformedTokens(t *testing.T) {
	instants := []time.Time{
		time.Date(2026, time.April, 5, 6, 7, 8, 234567000, time.UTC),
		time.Date(2026, time.April, 5, 6, 7, 9, 876543000, time.UTC),
	}
	ids := []string{idgen.MintAt("Good", instants[0]), idgen.MintAt("AlsoGood", instants[1])}
	bad := []string{"broken", "not_an_id"}
	args := []string{"--decode", bad[0], ids[0], bad[1], ids[1]}

	stdout, stderr, exitCode := runCLI(args, "", &fakeClock{})

	if exitCode != exitFailure {
		t.Errorf("Run() exit code = %d, want decode failure %d", exitCode, exitFailure)
	}
	if got, want := stdout, decodedLine(instants[0])+decodedLine(instants[1]); got != want {
		t.Errorf("stdout = %q, want ordered valid output %q", got, want)
	}
	for _, token := range bad {
		if !strings.Contains(stderr, token) {
			t.Errorf("stderr = %q, want malformed token %q named", stderr, token)
		}
	}
	if got := strings.Count(stderr, "\n"); got != len(bad) {
		t.Errorf("stderr line count = %d, want %d: %q", got, len(bad), stderr)
	}
}

// R-TIKS-DXVA: empty decode input is silent success and never consults the clock.
func TestRunDecodeEmptyInputIsSilentSuccess(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.August, 29, 1, 2, 3, 0, time.UTC)}

	stdout, stderr, exitCode := runCLI([]string{"--decode"}, "", clock)

	if exitCode != exitSuccess || stdout != "" || stderr != "" {
		t.Errorf("Run() = (stdout %q, stderr %q, exit %d), want empty streams and exit %d", stdout, stderr, exitCode, exitSuccess)
	}
	if clock.nowCalls != 0 || clock.sleepCalls != 0 {
		t.Errorf("clock calls = Now %d, Sleep %d; want zero", clock.nowCalls, clock.sleepCalls)
	}
}

// R-TJSO-RPLZ: an id minted through Run decodes to its millisecond minting instant.
func TestRunMintThenDecodeRoundTrip(t *testing.T) {
	instant := time.Date(2026, time.May, 6, 7, 8, 9, 456789000, time.FixedZone("source", 5*60*60+30*60))
	mintClock := &fakeClock{now: instant}
	minted, mintStderr, mintExit := runCLI(nil, "", mintClock)
	if mintExit != exitSuccess || mintStderr != "" {
		t.Fatalf("mint Run() = (stdout %q, stderr %q, exit %d), want successful mint", minted, mintStderr, mintExit)
	}
	id := strings.TrimSuffix(minted, "\n")

	stdout, stderr, exitCode := runCLI([]string{"--decode", id}, "", &fakeClock{})

	if exitCode != exitSuccess || stderr != "" {
		t.Errorf("decode Run() = (stderr %q, exit %d), want empty and %d", stderr, exitCode, exitSuccess)
	}
	if got, want := stdout, decodedLine(instant); got != want {
		t.Errorf("decoded stdout = %q, want millisecond minting instant %q", got, want)
	}
}

// R-TNGD-X0U2: decode output remains UTC when TZ names a non-UTC zone.
func TestRunDecodeOutputIgnoresTZEnvironment(t *testing.T) {
	if os.Getenv("IDGEN_TEST_DECODE_IN_CHILD") == "1" {
		exitCode := Run(
			[]string{"--decode", os.Getenv("IDGEN_TEST_DECODE_ID")},
			strings.NewReader(""),
			os.Stdout,
			os.Stderr,
			&fakeClock{},
		)
		os.Exit(exitCode)
	}

	instant := time.Date(2026, time.June, 7, 8, 9, 10, 765432000, time.FixedZone("origin", 10*60*60))
	id := idgen.MintAt("Zone", instant)
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	cmd := exec.Command(testBinary, "-test.run=^TestRunDecodeOutputIgnoresTZEnvironment$") // #nosec G204 -- the child is this fixed test binary
	cmd.Env = append(os.Environ(),
		"TZ=America/Chicago",
		"IDGEN_TEST_DECODE_IN_CHILD=1",
		"IDGEN_TEST_DECODE_ID="+id,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	want := "2026-06-06T22:09:10.765Z\n"
	if err := cmd.Run(); err != nil || stderr.Len() != 0 || stdout.String() != want {
		t.Errorf("child Run() = (stdout %q, stderr %q, error %v), want (%q, empty, nil)", stdout.String(), stderr.String(), err, want)
	}
	if !strings.HasSuffix(stdout.String(), "Z\n") {
		t.Errorf("stdout = %q, want literal UTC Z suffix", stdout.String())
	}
}
