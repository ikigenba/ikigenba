package linux_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
)

const (
	sessionUnitDir = "/etc/systemd/system/"
	sessionCgroup  = "/proc/42/cgroup"
	sessionAccount = "cit-ron"
	sessionUser    = "al-ice"
	sessionGroup   = "shell:x:990:other,al-ice,another\n"
)

func sessionEnvironment(t *testing.T, kind, user string) func(string) string {
	t.Helper()
	return func(key string) string {
		switch key {
		case "PAM_TYPE":
			return kind
		case "PAM_USER":
			return user
		default:
			t.Fatalf("unexpected PAM environment lookup %q", key)
			return ""
		}
	}
}

func TestSessionPlacement(t *testing.T) {
	// R-UDKY-22FU R-UH8N-7DNX
	if linux.SessionName != "accountsd-session" || linux.SessionHelper != "/usr/local/libexec/accountsd-session" ||
		linux.SessionPoll != 10*time.Millisecond || linux.SessionWait != 5*time.Second {
		t.Fatal("session constants differ from the declared values")
	}
	unit := linux.PersonSlice(sessionAccount, sessionUser)
	const scope = "accountsd-session-42.scope"
	placed := "1:name=systemd:/other\n0::/acct.slice/" + unit + "/" + scope + "\n"
	wrongHierarchy := "1:name=systemd:/acct.slice/" + unit + "/" + scope + "\n0::/other\n"
	wrongSlice := "0::/acct.slice/extra-" + unit + "/" + scope + "\n"
	wrongScope := "0::/acct.slice/" + unit + "/" + scope + "-other\n"
	type step struct {
		advance time.Duration
		cgroup  string
	}
	for _, tc := range []struct {
		name     string
		initial  string
		polls    []step
		timedOut bool
	}{
		{name: "immediate", initial: placed},
		{name: "retry failed read", polls: []step{{linux.SessionPoll, placed}}},
		{name: "wrong hierarchy and suffix", initial: wrongHierarchy, polls: []step{
			{linux.SessionPoll, wrongSlice}, {linux.SessionPoll, wrongScope}, {linux.SessionPoll, placed},
		}},
		{name: "read just before deadline", initial: wrongSlice, polls: []step{{linux.SessionWait - time.Nanosecond, placed}}},
		{name: "no read at deadline", initial: wrongSlice, polls: []step{{linux.SessionWait, placed}}, timedOut: true},
		{name: "persistent failure", polls: []step{{linux.SessionPoll, ""}, {linux.SessionWait, ""}}, timedOut: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put("/etc/group", sessionGroup, 0o644)
			f.put(sessionUnitDir+unit, "", 0o644)
			wantArgs := []string{
				"call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager",
				"StartTransientUnit", "ssa(sv)a(sa(sv))", scope, "fail", "2", "PIDs", "au", "1", "42", "Slice", "s", unit, "0",
			}
			now := time.Unix(1_000, 0)
			commands, polls := 0, 0
			env := linux.Env{
				Root: f.root,
				Execute: func(_ context.Context, command linux.Command) (linux.Result, error) {
					commands++
					if commands != 1 || command.Name != "busctl" || !slices.Equal(command.Args, wantArgs) ||
						command.Stdin != nil || command.Stdout != nil {
						t.Fatalf("unexpected session command %+v", command)
					}
					now = now.Add(linux.SessionWait)
					if tc.initial != "" {
						f.put(sessionCgroup, tc.initial, 0o644)
					}
					return linux.Result{}, nil
				},
				Now: func() time.Time { return now },
				Sleep: func(duration time.Duration) {
					if duration != linux.SessionPoll || polls >= len(tc.polls) {
						t.Fatalf("unexpected pause %d of %v", polls+1, duration)
					}
					step := tc.polls[polls]
					polls++
					now = now.Add(step.advance)
					if step.cgroup != "" {
						f.put(sessionCgroup, step.cgroup, 0o644)
					}
				},
			}
			err := linux.Session(t.Context(), env, sessionEnvironment(t, "open_session", sessionUser), 42)
			if (err != nil) != tc.timedOut || commands != 1 || polls != len(tc.polls) {
				t.Fatalf("Session = %v, %d commands and %d pauses, want timeout %v, one command and %d pauses",
					err, commands, polls, tc.timedOut, len(tc.polls))
			}
		})
	}
}

func TestSessionIneligible(t *testing.T) {
	// R-UH8N-7DNX
	unit := linux.PersonSlice(sessionAccount, sessionUser)
	for _, tc := range []struct {
		name, kind, user, group, unit string
	}{
		{name: "close", kind: "close_session", user: sessionUser, group: sessionGroup, unit: unit},
		{name: "no user", kind: "open_session", group: sessionGroup, unit: unit},
		{name: "nonmember", kind: "open_session", user: sessionUser, group: "shell:x:990:al-ice2\nother:x:991:al-ice\n", unit: unit},
		{name: "no matching slice", kind: "open_session", user: sessionUser, group: sessionGroup, unit: linux.PersonSlice(sessionAccount, "else")},
		{name: "no group file", kind: "open_session", user: sessionUser, unit: unit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			if tc.group == "" {
				if err := os.Remove(filepath.Join(f.root, "/etc/group")); err != nil {
					t.Fatal(err)
				}
			} else {
				f.put("/etc/group", tc.group, 0o644)
			}
			f.put(sessionUnitDir+tc.unit, "", 0o644)
			before := snapshotHost(t, f.root)
			env := linux.Env{
				Root: f.root,
				Execute: func(_ context.Context, command linux.Command) (linux.Result, error) {
					t.Fatalf("ineligible session executed %+v", command)
					return linux.Result{}, nil
				},
				Now:   func() time.Time { t.Fatal("ineligible session consulted the clock"); return time.Time{} },
				Sleep: func(time.Duration) { t.Fatal("ineligible session paused") },
			}
			if err := linux.Session(t.Context(), env, sessionEnvironment(t, tc.kind, tc.user), 42); err != nil {
				t.Fatal(err)
			}
			if after := snapshotHost(t, f.root); !reflect.DeepEqual(after, before) {
				t.Fatal("ineligible session changed the host")
			}
		})
	}
}

func TestSessionCommandFailure(t *testing.T) {
	// R-UH8N-7DNX
	cause := errors.New("command unavailable")
	for _, tc := range []struct {
		name   string
		status int
		cause  error
	}{
		{name: "nonzero exit", status: 9},
		{name: "runner error", cause: cause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put("/etc/group", sessionGroup, 0o644)
			f.put(sessionUnitDir+linux.PersonSlice(sessionAccount, sessionUser), "", 0o644)
			result := linux.Result{ExitCode: tc.status, Stderr: []byte("failure details")}
			calls := 0
			env := linux.Env{
				Root: f.root,
				Execute: func(_ context.Context, c linux.Command) (linux.Result, error) {
					calls++
					if c.Name != "busctl" || calls != 1 {
						t.Fatalf("unexpected command %+v at call %d", c, calls)
					}
					return result, tc.cause
				},
				Now:   func() time.Time { t.Fatal("failed command led to polling"); return time.Time{} },
				Sleep: func(time.Duration) { t.Fatal("failed command led to a pause") },
			}
			err := linux.Session(t.Context(), env, sessionEnvironment(t, "open_session", sessionUser), 42)
			var commandErr *linux.CommandError
			if !errors.As(err, &commandErr) || commandErr.Label != "busctl" || !reflect.DeepEqual(commandErr.Result, result) ||
				!errors.Is(commandErr.Err, tc.cause) || calls != 1 {
				t.Fatalf("Session error = %#v after %d commands, want a CommandError reporting %#v and %v", err, calls, result, tc.cause)
			}
		})
	}
}
