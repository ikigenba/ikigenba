package space

import (
	"context"
	"fmt"
	"io"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func runList(ctx context.Context, stdout io.Writer, acct *account.Account) error {
	spaces, err := acct.Spaces(ctx)
	if err != nil {
		return err
	}
	for _, item := range spaces {
		address := item.Address
		if address == "" {
			address = "-"
		}
		if _, err := fmt.Fprintf(stdout, "%s %s %s\n", item.Domain, item.State, address); err != nil {
			return err
		}
	}
	return nil
}

func runStatus(
	ctx context.Context,
	domain string,
	stdout io.Writer,
	deps seam.Deps,
	acct *account.Account,
) error {
	item, err := acct.Space(ctx, domain)
	if err != nil {
		return err
	}
	if item.State != cloud.StateRunning {
		return &NotRunningError{Domain: domain, State: item.State}
	}

	output, err := (host.Host{Address: item.Address, Deps: deps}).Sudo(ctx, "", "opsctl", "status")
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, output.Stdout)
	return err
}

func runStop(
	ctx context.Context,
	stdout io.Writer,
	deps seam.Deps,
	acct *account.Account,
	domain string,
) error {
	item, err := acct.Space(ctx, domain)
	if err != nil {
		return err
	}
	if item.State == cloud.StateStopped {
		Step(stdout, "instance", "already stopped")
		return nil
	}
	if err := acct.Clients.EC2.StopInstance(ctx, item.ID); err != nil {
		return err
	}
	if _, err := WaitState(ctx, deps, acct, item.ID, cloud.StateStopped); err != nil {
		return err
	}
	Step(stdout, "instance", item.ID+" stopped")
	return nil
}
