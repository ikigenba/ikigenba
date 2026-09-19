package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallCLICommandPreservesTransportCauses(t *testing.T) {
	// R-X0NN-ABK0, R-AJZ2-XSUE
	transportErr := errors.New("connection lost")
	result := host.Result{Stderr: []byte("partial detail\n")}
	env := host.Env{Execute: func(context.Context, host.Command) (host.Result, error) {
		return result, transportErr
	}}
	err := executeInstallCLICommand(context.Background(), env, "restart litestream.service", "systemctl", "restart", "litestream.service")
	var commandErr *host.CommandError
	if !errors.Is(err, transportErr) || !errors.As(err, &commandErr) || commandErr.Label != "restart litestream.service" || commandErr.Result.Stderr == nil {
		t.Fatalf("wrapped transport error = %#v", err)
	}

	preexisting := &host.CommandError{Label: "remote restart", Result: result, Err: transportErr}
	env.Execute = func(context.Context, host.Command) (host.Result, error) {
		return host.Result{}, preexisting
	}
	err = executeInstallCLICommand(context.Background(), env, "restart litestream.service", "systemctl", "restart", "litestream.service")
	var returned *host.CommandError
	if !errors.As(err, &returned) || returned != preexisting || !errors.Is(err, transportErr) {
		t.Fatalf("preexisting command error = %#v, want same error %#v", err, preexisting)
	}
}

func TestInstallCLIConfigurationFailuresPreserveActionAndReportCauses(t *testing.T) {
	// R-AJZ2-XSUE
	for _, step := range []string{"nginx", "litestream"} {
		t.Run(step, func(t *testing.T) {
			actionErr := errors.New(step + " action failed")
			reportErr := errors.New(step + " report failed")
			calls := 0
			err := reportInstallConfigurationFailure(func(gotStep, detail string, success bool) error {
				calls++
				if gotStep != step || detail != actionErr.Error() || success {
					t.Fatalf("report = %q, %q, %v", gotStep, detail, success)
				}
				return reportErr
			}, step, actionErr)
			if calls != 1 || !errors.Is(err, actionErr) || !errors.Is(err, reportErr) {
				t.Fatalf("calls = %d, failure = %#v", calls, err)
			}
		})
	}
}
