package spacecreate

import (
	"context"
	"fmt"
	"io"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// Run executes a space create command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	invocation, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if invocation.help {
		_, err := fmt.Fprint(stdout, usageText)
		return err
	}
	return runCreate(ctx, stdout, deps, invocation)
}

// runCreate is the handoff from the create grammar to its ordered operation.
// Keeping it isolated lets the provisioning phases extend the operation without
// coupling command-line parsing to those steps.
func runCreate(ctx context.Context, stdout io.Writer, deps seam.Deps, invocation invocation) error {
	result, err := preflight(ctx, deps, invocation.operand)
	if err != nil {
		return err
	}
	return beginProvisioning(ctx, stdout, deps, invocation, result)
}

// beginProvisioning crosses create's first mutation barrier. Later provisioning
// steps continue from this single handoff after secrets have been pushed.
func beginProvisioning(ctx context.Context, stdout io.Writer, deps seam.Deps, invocation invocation, result preflightResult) error {
	_, err := pushSecretsAndReport(ctx, deps, result, stdout)
	if err != nil {
		return err
	}
	return provision(ctx, stdout, deps, invocation, result)
}
