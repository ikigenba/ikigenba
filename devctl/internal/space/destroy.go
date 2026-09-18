package space

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
)

func runDestroy(
	ctx context.Context,
	stdout io.Writer,
	deps seam.Deps,
	acct *account.Account,
	domain string,
	noBackup bool,
	profile string,
) error {
	item, exists, err := destroySpace(ctx, acct, domain)
	if err != nil {
		return err
	}

	if !acct.Properties.DeleteBackupsOnDestroy {
		switch {
		case !exists:
			Step(stdout, "retire", "already gone")
		case noBackup:
			_, _ = fmt.Fprintln(stdout, "retire: skipped (--no-backup)")
		default:
			if item.State != cloud.StateRunning {
				return &RetireStateError{ID: item.ID, State: item.State, Domain: domain, Profile: profile}
			}
			_, err := (host.Host{Address: item.Address, Deps: deps}).Sudo(ctx, "retire", "opsctl", "retire")
			if err != nil {
				return err
			}
			Step(stdout, "retire", "opsctl retire")
		}
	}

	terminated := exists
	if exists {
		if err := acct.Clients.EC2.TerminateInstance(ctx, item.ID); err != nil {
			return err
		}
		if _, err := WaitState(ctx, deps, acct, item.ID, cloud.StateTerminated); err != nil {
			return err
		}
		Step(stdout, "instance", item.ID+" terminated")
	} else {
		Step(stdout, "instance", "already gone")
	}

	address, found, err := ElasticIP(ctx, acct, domain)
	if err != nil {
		return err
	}
	switch {
	case found:
		if err := ReleaseElasticIP(ctx, acct, address); err != nil {
			return err
		}
		Step(stdout, "address", "elastic ip "+address.IP+" released")
	case terminated:
		Step(stdout, "address", "no elastic ip")
	default:
		Step(stdout, "address", "already gone")
	}

	if err := destroyRecords(ctx, stdout, acct, domain); err != nil {
		return err
	}
	if err := destroySecrets(ctx, stdout, acct, domain, terminated); err != nil {
		return err
	}
	if err := destroyBackups(ctx, stdout, acct, domain, terminated); err != nil {
		return err
	}
	deleted, err := DeleteRole(ctx, acct, domain)
	if err != nil {
		return err
	}
	if deleted {
		Step(stdout, "role", RoleName(domain)+" deleted")
	} else {
		Step(stdout, "role", "already gone")
	}
	return nil
}

func destroySpace(ctx context.Context, acct *account.Account, domain string) (account.Space, bool, error) {
	item, err := acct.Space(ctx, domain)
	if err == nil {
		return item, true, nil
	}
	var noSpace *account.NoSpaceError
	if errors.As(err, &noSpace) {
		return account.Space{}, false, nil
	}
	return account.Space{}, false, err
}

func destroyRecords(ctx context.Context, stdout io.Writer, acct *account.Account, domain string) error {
	zone, err := acct.Zone(ctx, domain)
	if err != nil {
		var noZone *account.NoZoneError
		if errors.As(err, &noZone) {
			Step(stdout, "records", "already gone")
			return nil
		}
		return err
	}
	records, err := acct.Clients.Route53.ListRecords(ctx, zone.ID)
	if err != nil {
		return err
	}
	changes := make([]cloud.RecordChange, 0, 2)
	names := make([]string, 0, 2)
	for _, wanted := range RecordNames(domain) {
		for _, record := range records {
			if record.Type == "A" && (record.Name == wanted || wanted == "*."+domain && record.Name == `\052.`+domain) {
				changes = append(changes, cloud.RecordChange{Action: cloud.ChangeDelete, Record: record})
				names = append(names, wanted)
			}
		}
	}
	if len(changes) == 0 {
		Step(stdout, "records", "already gone")
		return nil
	}
	if _, err := acct.Clients.Route53.ChangeRecords(ctx, zone.ID, changes); err != nil {
		return err
	}
	Step(stdout, "records", "deleted "+strings.Join(names, ", "))
	return nil
}

func destroySecrets(ctx context.Context, stdout io.Writer, acct *account.Account, domain string, terminated bool) error {
	if !acct.Properties.DeleteSecretsOnDestroy {
		Step(stdout, "secrets", "kept, delete_secrets_on_destroy=false")
		return nil
	}
	parameters, err := acct.Clients.SSM.ListParameters(ctx, secrets.Prefix(domain))
	if err != nil {
		return err
	}
	for _, parameter := range parameters {
		if err := acct.Clients.SSM.DeleteParameter(ctx, parameter.Name); err != nil {
			return err
		}
	}
	if len(parameters) == 0 && !terminated {
		Step(stdout, "secrets", "already gone")
	} else {
		Step(stdout, "secrets", fmt.Sprintf("%d parameters deleted", len(parameters)))
	}
	return nil
}

func destroyBackups(ctx context.Context, stdout io.Writer, acct *account.Account, domain string, terminated bool) error {
	if !acct.Properties.DeleteBackupsOnDestroy {
		Step(stdout, "backups", "kept, delete_backups_on_destroy=false")
		return nil
	}
	objects, err := acct.Clients.S3.ListObjects(ctx, acct.Properties.BackupBucket, BackupPrefix(domain))
	if err != nil {
		return err
	}
	keys := make([]string, len(objects))
	for index, object := range objects {
		keys[index] = object.Key
	}
	if err := acct.Clients.S3.DeleteObjects(ctx, acct.Properties.BackupBucket, keys); err != nil {
		return err
	}
	if len(keys) == 0 && !terminated {
		Step(stdout, "backups", "already gone")
	} else {
		Step(stdout, "backups", fmt.Sprintf("%d objects deleted", len(keys)))
	}
	return nil
}
