package spacecreate

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

// RunHostSteps waits for a newly created host, installs and configures opsctl,
// restores retained host data when present, and initializes the deployment.
func RunHostSteps(
	ctx context.Context,
	stdout io.Writer,
	deps seam.Deps,
	acct *account.Account,
	domain string,
	address string,
	instanceID string,
	zone cloud.Zone,
	acmeEmail string,
) error {
	if err := space.WaitChecks(ctx, deps, acct, instanceID); err != nil {
		return err
	}
	target := host.Host{Address: address, Deps: deps}
	if err := target.Wait(ctx); err != nil {
		return err
	}
	if _, err := target.Sudo(ctx, "host", "cloud-init", "status", "--wait"); err != nil {
		return err
	}
	space.Step(stdout, "host", "status checks passed, cloud-init done")

	version, err := hostsetup.InstallLatest(ctx, target)
	if err != nil {
		return err
	}
	configured, err := hostsetup.Configure(ctx, target, acct.Properties, zone, domain, &acmeEmail)
	if err != nil {
		return err
	}
	space.Step(stdout, "opsctl", fmt.Sprintf("%s installed, %d keys set", version, configured))

	if !acct.Properties.DeleteBackupsOnDestroy {
		prefix := domain + "/host/"
		objects, err := acct.Clients.S3.ListObjects(ctx, acct.Properties.BackupBucket, prefix)
		if err != nil {
			return err
		}
		if len(objects) == 0 {
			space.Step(stdout, "restore", "no host backup")
		} else {
			output, err := target.Sudo(ctx, "restore", "opsctl", "host", "restore")
			if err != nil {
				return err
			}
			selected, err := selectedBackup(output.Stdout, domain)
			if err != nil {
				return err
			}
			configured, err := hostsetup.Configure(ctx, target, acct.Properties, zone, domain, &acmeEmail)
			if err != nil {
				return err
			}
			space.Step(stdout, "restore", fmt.Sprintf("%s, %d keys set again", selected, configured))
		}
	}

	if _, err := target.Sudo(ctx, "init", "opsctl", "init"); err != nil {
		return err
	}
	space.Step(stdout, "init", "")
	return nil
}

func selectedBackup(output, domain string) (string, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		selected := strings.TrimSpace(lines[index])
		if selected == "" {
			continue
		}
		selected = strings.TrimPrefix(selected, domain+"/")
		return selected, nil
	}
	return "", fmt.Errorf("opsctl host restore returned no backup key")
}
