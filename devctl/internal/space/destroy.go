package space

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

type destroyOptions struct {
	noBackup      bool
	deleteSecrets bool
	deleteBackups bool
}

func runDestroy(ctx context.Context, stdout io.Writer, deps seam.Deps, clients cloud.Clients, root string, sp spaceref.Space, options destroyOptions) error {
	item, exists, err := destroySpace(ctx, clients.EC2, root, sp.Domain)
	if err != nil {
		return err
	}
	zone, err := clients.Route53.Zone(ctx, root)
	if err != nil {
		return err
	}
	if exists && item.ID != "" && !options.noBackup && item.State != cloud.StateRunning {
		return &RetireStateError{ID: item.ID, State: item.State, Label: sp.Label}
	}
	apex, err := FindApex(ctx, clients.Route53, clients.EC2, zone.ID, root)
	if err != nil {
		return err
	}
	if apex.Holder == sp.Domain {
		if _, err := clients.Route53.ChangeRecords(ctx, zone.ID, []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: apex.Record}}); err != nil {
			return err
		}
		Step(stdout, "apex", root+" record deleted")
	}

	switch {
	case !exists || item.ID == "":
		Step(stdout, "retire", "already gone")
	case options.noBackup:
		_, _ = fmt.Fprintln(stdout, "retire: skipped (--no-backup)")
	default:
		if _, err := (host.Host{Domain: sp.Domain, Deps: deps}).Sudo(ctx, "retire", "opsctl", "retire"); err != nil {
			return err
		}
		Step(stdout, "retire", "opsctl retire")
	}

	if item.ID != "" {
		if err := clients.EC2.TerminateInstance(ctx, item.ID); err != nil {
			return err
		}
		if _, err := WaitState(ctx, deps, clients.EC2, item.ID, cloud.StateTerminated); err != nil {
			return err
		}
		Step(stdout, "instance", item.ID+" terminated")
	} else {
		Step(stdout, "instance", "already gone")
	}
	remaining, _, err := destroySpace(ctx, clients.EC2, root, sp.Domain)
	if err != nil {
		return err
	}
	switch {
	case remaining.ElasticIP.AllocationID != "":
		if err := ReleaseElasticIP(ctx, clients.EC2, remaining.ElasticIP); err != nil {
			return err
		}
		Step(stdout, "address", "elastic ip "+remaining.ElasticIP.IP+" released")
	case item.ID != "":
		Step(stdout, "address", "no elastic ip")
	default:
		Step(stdout, "address", "already gone")
	}

	deletedRecords, err := DeleteRecords(ctx, clients.Route53, zone, sp.Domain)
	if err != nil {
		return err
	}
	if len(deletedRecords) == 0 {
		Step(stdout, "records", "already gone")
	} else {
		Step(stdout, "records", "deleted "+strings.Join(deletedRecords, ", "))
	}
	if err := destroySecrets(ctx, stdout, clients.SSM, sp.Domain, options.deleteSecrets); err != nil {
		return err
	}
	if err := destroyBackups(ctx, stdout, clients.S3, root, sp.Label, options.deleteBackups); err != nil {
		return err
	}
	deletedRole, err := DeleteRole(ctx, clients.IAM, sp.Domain)
	if err != nil {
		return err
	}
	if deletedRole {
		Step(stdout, "role", RoleName(sp.Domain)+" deleted")
	} else {
		Step(stdout, "role", "already gone")
	}
	return nil
}

func destroySpace(ctx context.Context, ec2 cloud.EC2, root, domain string) (cloud.Space, bool, error) {
	item, err := cloud.LookupSpace(ctx, ec2, root, domain)
	if err == nil {
		return item, true, nil
	}
	var noSpace *cloud.NoSpaceError
	if errors.As(err, &noSpace) {
		return cloud.Space{}, false, nil
	}
	return cloud.Space{}, false, err
}

func destroySecrets(ctx context.Context, stdout io.Writer, ssm cloud.SSM, domain string, remove bool) error {
	if !remove {
		Step(stdout, "secrets", "kept")
		return nil
	}
	parameters, err := ssm.ListParameters(ctx, secrets.Prefix(domain))
	if err != nil {
		return err
	}
	for _, parameter := range parameters {
		if err := ssm.DeleteParameter(ctx, parameter.Name); err != nil {
			return err
		}
	}
	if len(parameters) == 0 {
		Step(stdout, "secrets", "already gone")
	} else {
		Step(stdout, "secrets", fmt.Sprintf("%d parameters deleted", len(parameters)))
	}
	return nil
}

func destroyBackups(ctx context.Context, stdout io.Writer, s3 cloud.S3, root, label string, remove bool) error {
	if !remove {
		Step(stdout, "backups", "kept")
		return nil
	}
	count := 0
	var deleteErr error
	err := s3.ListObjects(ctx, root, BackupPrefix(label), func(page []cloud.Object) bool {
		keys := make([]string, len(page))
		for i, object := range page {
			keys[i] = object.Key
		}
		deleteErr = s3.DeleteObjects(ctx, root, keys)
		if deleteErr != nil {
			return false
		}
		count += len(keys)
		return true
	})
	if deleteErr != nil {
		return deleteErr
	}
	if err != nil {
		return err
	}
	if count == 0 {
		Step(stdout, "backups", "already gone")
	} else {
		Step(stdout, "backups", fmt.Sprintf("%d objects deleted", count))
	}

	return nil
}
