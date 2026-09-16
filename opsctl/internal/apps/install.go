package apps

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// InstallHooks joins artifact installation to CLI-owned reporting and host
// configuration.
type InstallHooks struct {
	Report    func(step, detail string, success bool) error
	Configure func(context.Context, Manifest) error
}

// InstallError describes a failed installation and retains its cause.
type InstallError struct {
	Code    int
	Message string
	Cause   error
}

// Error returns the stable diagnostic message for an installation failure.
func (failure *InstallError) Error() string { return failure.Message }

// Unwrap exposes the operation that caused an installation failure.
func (failure *InstallError) Unwrap() error { return failure.Cause }

// Install installs the app artifact at uri into env.
func Install(ctx context.Context, env host.Env, remote cloud.Env, store config.Store, uri string, hooks InstallHooks) error {
	_ = ctx
	_ = env
	_ = remote
	_ = store
	_ = uri
	_ = hooks
	return &InstallError{Code: 1, Message: "install failed", Cause: errors.New("installation workflow is unavailable")}
}
