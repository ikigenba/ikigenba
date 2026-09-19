package space

import (
	"context"
	"fmt"
	"io"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func runList(ctx context.Context, stdout io.Writer, clients cloud.Clients, root string) error {
	zone, err := clients.Route53.Zone(ctx, root)
	if err != nil {
		return err
	}
	spaces, err := cloud.Spaces(ctx, clients.EC2, root)
	if err != nil {
		return err
	}
	var apex ApexRecord
	if len(spaces) != 0 {
		apex, err = FindApex(ctx, clients.Route53, clients.EC2, zone.ID, root)
		if err != nil {
			return err
		}
	}
	for _, item := range spaces {
		address := item.Address
		if address == "" {
			address = "-"
		}
		marker := "-"
		if apex.Holder == item.Domain {
			marker = "apex"
		}
		if _, err := fmt.Fprintf(stdout, "%s %s %s %s\n", item.Domain, item.State, address, marker); err != nil {
			return err
		}
	}
	return nil
}

func lookupSpace(ctx context.Context, clients cloud.Clients, root string, sp spaceref.Space) (cloud.Space, error) {
	return cloud.LookupSpace(ctx, clients.EC2, root, sp.Domain)
}

func runStatus(ctx context.Context, stdout io.Writer, deps seam.Deps, clients cloud.Clients, root string, sp spaceref.Space) error {
	item, err := lookupSpace(ctx, clients, root, sp)
	if err != nil {
		return err
	}
	if item.State != cloud.StateRunning {
		return &NotRunningError{Domain: sp.Domain, State: item.State}
	}
	output, err := (host.Host{Address: item.Address, Deps: deps}).Sudo(ctx, "", "opsctl", "status")
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, output.Stdout)
	return err
}

func runStop(ctx context.Context, stdout io.Writer, deps seam.Deps, clients cloud.Clients, root string, sp spaceref.Space) error {
	item, err := lookupSpace(ctx, clients, root, sp)
	if err != nil {
		return err
	}
	if item.State == cloud.StateStopped {
		Step(stdout, "instance", "already stopped")
		return nil
	}
	if err := clients.EC2.StopInstance(ctx, item.ID); err != nil {
		return err
	}
	if _, err := WaitState(ctx, deps, clients.EC2, item.ID, cloud.StateStopped); err != nil {
		return err
	}
	Step(stdout, "instance", item.ID+" stopped")
	return nil
}

func runStart(ctx context.Context, stdout io.Writer, deps seam.Deps, clients cloud.Clients, root string, sp spaceref.Space) error {
	item, err := lookupSpace(ctx, clients, root, sp)
	if err != nil {
		return err
	}
	running := cloud.Instance{ID: item.ID, Space: item.Domain, State: item.State, Address: item.Address}
	if item.State != cloud.StateRunning {
		if err := clients.EC2.StartInstance(ctx, item.ID); err != nil {
			return err
		}
		running, err = WaitState(ctx, deps, clients.EC2, item.ID, cloud.StateRunning)
		if err != nil {
			return err
		}
	}
	Step(stdout, "instance", running.ID+" running, "+running.Address)
	if err := WaitChecks(ctx, deps, clients.EC2, item.ID); err != nil {
		return err
	}
	Step(stdout, "host", "status checks passed")
	remote := host.Host{Address: running.Address, Deps: deps}
	if err := remote.Wait(ctx); err != nil {
		return err
	}
	if _, err := remote.Sudo(ctx, "certificate", "certbot", "renew"); err != nil {
		return err
	}
	Step(stdout, "certificate", "certbot renew")
	_, err = fmt.Fprintf(stdout, "%s %s\n", sp.Domain, running.Address)
	return err
}
