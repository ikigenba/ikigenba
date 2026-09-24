package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestHostPublicContract(t *testing.T) {
	// R-T3CY-DCHC
	if User != "ec2-user" {
		t.Fatalf("User = %q", User)
	}
	assertFields(t, Host{}, []field{{"Address", reflect.TypeFor[string]()}, {"Deps", reflect.TypeFor[seam.Deps]()}})
	h := Host{Address: "3.19.79.227"}
	if got, want := h.Target(), "ec2-user@3.19.79.227"; got != want {
		t.Fatalf("Target() = %q, want %q", got, want)
	}

	// R-T5SR-4VYQ
	if ProbeInterval != 5*time.Second || ProbeAttempts != 12 {
		t.Fatalf("probe constants = %v, %d", ProbeInterval, ProbeAttempts)
	}

	// R-T88J-WFG4
	assertFields(t, UnreachableError{}, []field{{"Address", reflect.TypeFor[string]()}})
	err := (&UnreachableError{Address: "3.145.72.19"}).Error()
	if want := "ssh ec2-user@3.145.72.19: connection timed out"; err != want {
		t.Fatalf("Error() = %q, want %q", err, want)
	}

	// R-D6VV-A7PF
	assertFields(t, Output{}, []field{{"Stdout", reflect.TypeFor[string]()}, {"Stderr", reflect.TypeFor[string]()}})
	assertType(t, "Host.Run", Host.Run, reflect.TypeFor[func(Host, context.Context, string, ...string) (Output, error)]())
	assertType(t, "Host.Sudo", Host.Sudo, reflect.TypeFor[func(Host, context.Context, string, ...string) (Output, error)]())
	assertType(t, "Host.StreamSudo", Host.StreamSudo, reflect.TypeFor[func(Host, context.Context, io.Writer, string, ...string) error]())
	assertType(t, "Host.Wait", Host.Wait, reflect.TypeFor[func(Host, context.Context) error]())
}

func TestCommandErrorContract(t *testing.T) {
	// R-JA8W-5LZ5
	assertFields(t, CommandError{}, []field{
		{"Step", reflect.TypeFor[string]()},
		{"Command", reflect.TypeFor[[]string]()},
		{"Status", reflect.TypeFor[int]()},
		{"Stdout", reflect.TypeFor[string]()},
		{"Stderr", reflect.TypeFor[string]()},
	})
	tests := []struct {
		name string
		err  CommandError
		want string
	}{
		{"without step", CommandError{Command: []string{"ssh", "host", "false"}, Status: 7}, "ssh host false: exit status 7"},
		{"with step", CommandError{Step: "retire", Command: []string{"ssh", "host", "false"}, Status: 7}, "retire: ssh host false: exit status 7"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.err.Error(); got != test.want {
				t.Fatalf("Error() = %q, want %q", got, test.want)
			}
		})
	}
	detailCases := []struct {
		name   string
		stdout string
		stderr string
		want   string
	}{
		{"both streams", "a\nb\n", "c\n\n> d\n", "> a\n> b\n> c\n> \n> > d"},
		{"empty stderr", "report\n", "", "> report"},
		{"whitespace stderr", "report\n", " \n\t", "> report"},
		{"empty stdout", "", "failure\n", "> failure"},
		{"whitespace stdout", " \n\t", "failure\n", "> failure"},
		{"neither stream", "", " \n\t", ""},
	}
	for _, test := range detailCases {
		t.Run(test.name, func(t *testing.T) {
			got := (&CommandError{Stdout: test.stdout, Stderr: test.stderr}).Detail()
			if got != test.want {
				t.Fatalf("Detail() = %q, want %q", got, test.want)
			}
		})
	}
	if got := (&CommandError{}).ExitCode(); got != 1 {
		t.Fatalf("ExitCode() = %d", got)
	}
}

func TestRunAndSudoPreserveRemoteArguments(t *testing.T) {
	// R-D83R-NZG4
	var commands []seam.Cmd
	h := Host{Address: "192.0.2.1", Deps: seam.Deps{
		Dir: "/checkout",
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command)
			return seam.Result{}, nil
		},
	}}
	args := []string{"space here", `double"quote`, "single'quote", "*?[abc]", "; $(touch no) & | >", ""}
	if _, err := h.Run(context.Background(), "", args...); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Sudo(context.Background(), "", args...); err != nil {
		t.Fatal(err)
	}
	prefix := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@192.0.2.1"}
	wantRun := append(append([]string{}, prefix...), `'space here' 'double"quote' 'single'\''quote' '*?[abc]' '; $(touch no) & | >' ''`)
	wantSudo := append(append([]string{}, prefix...), `'sudo' 'space here' 'double"quote' 'single'\''quote' '*?[abc]' '; $(touch no) & | >' ''`)
	for i, want := range [][]string{wantRun, wantSudo} {
		if commands[i].Path != "ssh" || commands[i].Dir != "/checkout" || !reflect.DeepEqual(commands[i].Args, want) {
			t.Errorf("command %d = %#v, want path ssh, dir /checkout, args %#v", i, commands[i], want)
		}
	}
}

func TestRunResultsAndErrors(t *testing.T) {
	// R-D9BO-1R6T
	wantStart := errors.New("cannot start")
	type invocation struct {
		name    string
		call    func(Host) (Output, error)
		logical []string
	}
	invocations := []invocation{
		{
			name: "run",
			call: func(h Host) (Output, error) {
				return h.Run(context.Background(), "deploy", "opsctl", "status")
			},
			logical: []string{"ssh", "ec2-user@host", "opsctl", "status"},
		},
		{
			name: "sudo",
			call: func(h Host) (Output, error) {
				return h.Sudo(context.Background(), "deploy", "opsctl", "status")
			},
			logical: []string{"ssh", "ec2-user@host", "sudo", "opsctl", "status"},
		},
	}
	tests := []struct {
		name       string
		result     seam.Result
		runnerErr  error
		wantOutput Output
		wantStatus int
	}{
		{"success", seam.Result{Stdout: []byte("out\x00\n"), Stderr: []byte("warn\n")}, nil, Output{Stdout: "out\x00\n", Stderr: "warn\n"}, 0},
		{"exit", seam.Result{Stdout: []byte("partial\n"), Stderr: []byte("bad\n"), ExitCode: 23}, nil, Output{}, 23},
		{"start", seam.Result{}, wantStart, Output{}, 0},
	}
	for _, invocation := range invocations {
		for _, test := range tests {
			t.Run(invocation.name+"/"+test.name, func(t *testing.T) {
				h := Host{Address: "host", Deps: seam.Deps{Dir: "/work", Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					return test.result, test.runnerErr
				}}}
				got, err := invocation.call(h)
				if got != test.wantOutput {
					t.Errorf("Output = %#v, want %#v", got, test.wantOutput)
				}
				if test.runnerErr != nil {
					if !errors.Is(err, wantStart) || !strings.Contains(err.Error(), "ssh") {
						t.Fatalf("error = %v, want wrapped ssh start error", err)
					}
					var commandErr *CommandError
					if errors.As(err, &commandErr) {
						t.Fatal("start error masquerades as CommandError")
					}
					return
				}
				if test.wantStatus == 0 {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				var commandErr *CommandError
				if !errors.As(err, &commandErr) {
					t.Fatalf("error = %T %v", err, err)
				}
				if commandErr.Step != "deploy" || commandErr.Status != 23 || commandErr.Stdout != "partial\n" || commandErr.Stderr != "bad\n" {
					t.Errorf("CommandError = %#v", commandErr)
				}
				if !reflect.DeepEqual(commandErr.Command, invocation.logical) {
					t.Errorf("Command = %#v, want %#v", commandErr.Command, invocation.logical)
				}
			})
		}
	}
}

func TestWaitRetriesUntilReachable(t *testing.T) {
	// R-TD45-FIEW
	var commands []seam.Cmd
	var waits []time.Duration
	h := Host{Address: "192.0.2.2", Deps: seam.Deps{
		Dir: "/work",
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command)
			if len(commands) < 3 {
				return seam.Result{ExitCode: 255}, nil
			}
			return seam.Result{}, nil
		},
		After: func(delay time.Duration) <-chan time.Time {
			waits = append(waits, delay)
			ready := make(chan time.Time, 1)
			ready <- time.Time{}
			return ready
		},
	}}
	if err := h.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 || !reflect.DeepEqual(waits, []time.Duration{ProbeInterval, ProbeInterval}) {
		t.Fatalf("got %d probes and waits %v", len(commands), waits)
	}
	wantArgs := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@192.0.2.2", "'true'"}
	for _, command := range commands {
		if command.Path != "ssh" || command.Dir != "/work" || !reflect.DeepEqual(command.Args, wantArgs) {
			t.Errorf("probe = %#v", command)
		}
	}
}

func TestWaitStopsAfterProbeLimit(t *testing.T) {
	// R-TD45-FIEW
	probes := 0
	waits := 0
	h := Host{Address: "3.145.72.19", Deps: seam.Deps{
		Dir: "/work",
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			probes++
			return seam.Result{ExitCode: 255}, nil
		},
		After: func(time.Duration) <-chan time.Time {
			waits++
			ready := make(chan time.Time, 1)
			ready <- time.Time{}
			return ready
		},
	}}
	err := h.Wait(context.Background())
	var unreachable *UnreachableError
	if !errors.As(err, &unreachable) || unreachable.Address != h.Address {
		t.Fatalf("Wait error = %T %v", err, err)
	}
	if probes != ProbeAttempts || waits != ProbeAttempts-1 {
		t.Fatalf("got %d probes and %d waits", probes, waits)
	}
}

func TestStreamSudo(t *testing.T) {
	// R-DAJK-FIXI
	var gotCommand seam.Cmd
	h := Host{Address: "host", Deps: seam.Deps{Dir: "/work", Stream: func(_ context.Context, command seam.Cmd, stdout io.Writer) (seam.Result, error) {
		gotCommand = command
		if _, err := io.WriteString(stdout, "first\nsecond"); err != nil {
			return seam.Result{}, err
		}
		return seam.Result{Stderr: []byte("remote failed\n"), ExitCode: 9}, nil
	}}}
	var stdout bytes.Buffer
	err := h.StreamSudo(context.Background(), &stdout, "logs", "journalctl", "-f", "space app")
	if got := stdout.String(); got != "first\nsecond" {
		t.Fatalf("stdout = %q", got)
	}
	wantArgs := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@host", "'sudo' 'journalctl' '-f' 'space app'"}
	if gotCommand.Path != "ssh" || gotCommand.Dir != "/work" || !reflect.DeepEqual(gotCommand.Args, wantArgs) {
		t.Errorf("command = %#v", gotCommand)
	}
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if commandErr.Stdout != "" || commandErr.Stderr != "remote failed\n" || commandErr.Status != 9 || commandErr.Step != "logs" {
		t.Errorf("CommandError = %#v", commandErr)
	}
	wantLogical := []string{"ssh", "ec2-user@host", "sudo", "journalctl", "-f", "space app"}
	if !reflect.DeepEqual(commandErr.Command, wantLogical) {
		t.Errorf("logical command = %#v", commandErr.Command)
	}
}

func TestStreamSudoWrapsRunnerFailures(t *testing.T) {
	// R-DAJK-FIXI
	for _, want := range []error{context.Canceled, errors.New("writer failed"), errors.New("cannot start")} {
		t.Run(want.Error(), func(t *testing.T) {
			h := Host{Address: "host", Deps: seam.Deps{Dir: "/work", Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
				return seam.Result{}, want
			}}}
			err := h.StreamSudo(context.Background(), io.Discard, "", "true")
			if !errors.Is(err, want) || !strings.Contains(err.Error(), "ssh") {
				t.Fatalf("error = %v, want wrapped %v", err, want)
			}
			var commandErr *CommandError
			if errors.As(err, &commandErr) {
				t.Fatal("runner failure masquerades as CommandError")
			}
		})
	}
}

func TestStreamSudoSuccess(t *testing.T) {
	// R-DAJK-FIXI
	h := Host{Address: "host", Deps: seam.Deps{Dir: "/work", Stream: func(_ context.Context, _ seam.Cmd, stdout io.Writer) (seam.Result, error) {
		_, err := io.WriteString(stdout, "unchanged\x00output")
		return seam.Result{Stderr: []byte("warning stays private")}, err
	}}}
	var stdout bytes.Buffer
	if err := h.StreamSudo(context.Background(), &stdout, "", "true"); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "unchanged\x00output"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

type field struct {
	name string
	typ  reflect.Type
}

func assertFields(t *testing.T, value any, want []field) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ, typ.NumField(), len(want))
	}
	for i, expected := range want {
		got := typ.Field(i)
		if got.Name != expected.name || got.Type != expected.typ {
			t.Errorf("field %d = %s %v, want %s %v", i, got.Name, got.Type, expected.name, expected.typ)
		}
	}
}

func assertType(t *testing.T, name string, value any, want reflect.Type) {
	t.Helper()
	if got := reflect.TypeOf(value); got != want {
		t.Errorf("%s type = %v, want %v", name, got, want)
	}
}
