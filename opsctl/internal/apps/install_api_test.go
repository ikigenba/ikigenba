package apps_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallHooksAPI(t *testing.T) {
	// R-YSKN-TNBR
	var reported, configured string
	hooks := apps.InstallHooks{
		Report: func(step, detail string, success bool) error {
			if success {
				reported = step + ":" + detail
			}
			return nil
		},
		Configure: func(_ context.Context, manifest apps.Manifest) error {
			configured = manifest.App
			return nil
		},
	}
	var (
		report    func(string, string, bool) error
		configure func(context.Context, apps.Manifest) error
	)
	report, configure = hooks.Report, hooks.Configure
	if report("fetch", "ok", true) != nil || configure(context.Background(), apps.Manifest{App: "notes"}) != nil {
		t.Fatal("InstallHooks returned an error")
	}
	if reported != "fetch:ok" || configured != "notes" {
		t.Fatalf("InstallHooks observed (%q, %q)", reported, configured)
	}
}

func TestInstallAPI(t *testing.T) {
	// R-OLR2-NBFZ
	for _, install := range []func(context.Context, host.Env, cloud.Env, config.Store, string, apps.InstallHooks) error{apps.Install} {
		err := install(t.Context(), host.Env{}, cloud.Env{}, config.Store{Root: t.TempDir()}, "https://bucket/app.tar.xz", apps.InstallHooks{
			Report:    func(string, string, bool) error { return nil },
			Configure: func(context.Context, apps.Manifest) error { return nil },
		})
		var failure *apps.InstallError
		if !errors.As(err, &failure) {
			t.Fatalf("Install(invalid URI) = %v, want *apps.InstallError", err)
		}
	}
}

func TestInstallErrorAPI(t *testing.T) {
	// R-YTSK-7F2G
	cause := errors.New("cause")
	failure := &apps.InstallError{Code: 2, Message: "message", Cause: cause}
	var (
		code    int
		message string
		wrapped error
	)
	code, message, wrapped = failure.Code, failure.Message, failure.Cause
	if code != 2 || message != "message" || !errors.Is(wrapped, cause) {
		t.Fatalf("InstallError fields = (%d, %q, %v)", code, message, wrapped)
	}
	if got := failure.Error(); got != "message" {
		t.Fatalf("Error() = %q, want %q", got, "message")
	}
	if got := failure.Unwrap(); !reflect.DeepEqual(got, cause) {
		t.Fatalf("Unwrap() = %v, want cause", got)
	}
	if !errors.Is(failure, cause) {
		t.Fatal("errors.Is does not reach InstallError.Cause")
	}
}
