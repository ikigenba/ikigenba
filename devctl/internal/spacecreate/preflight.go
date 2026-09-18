package spacecreate

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

// preflightResult is the state gathered before space creation may mutate the
// account. Keeping it together makes the read-only barrier explicit to the
// orchestration that follows it.
type preflightResult struct {
	apps      []checkout.App
	account   *account.Account
	zone      cloud.Zone
	accountID string
}

// preflight performs every read-only prerequisite check in dependency order.
func preflight(ctx context.Context, deps seam.Deps, profile, domain string) (preflightResult, error) {
	openedCheckout, err := checkout.Open(ctx, deps)
	if err != nil {
		return preflightResult{}, err
	}
	apps, err := openedCheckout.Apps()
	if err != nil {
		return preflightResult{}, err
	}

	acct, err := account.Open(ctx, deps, profile)
	if err != nil {
		return preflightResult{}, err
	}
	if err := validateAccountDomain(domain, acct.Properties.Domain); err != nil {
		return preflightResult{}, err
	}

	zone, err := acct.Zone(ctx, domain)
	if err != nil {
		return preflightResult{}, err
	}
	delegation, err := acct.Delegation(ctx, zone, domain)
	if err != nil {
		return preflightResult{}, err
	}
	if delegation != "" {
		return preflightResult{}, &RefusedError{Message: fmt.Sprintf("'%s' is delegated away from this account's zone '%s'", domain, zone.Name)}
	}

	spaces, err := acct.Spaces(ctx)
	if err != nil {
		return preflightResult{}, err
	}
	if err := validateSpaces(domain, acct.Properties.Domain, spaces); err != nil {
		return preflightResult{}, err
	}

	roleName := space.RoleName(domain)
	roleExists, err := acct.Clients.IAM.RoleExists(ctx, roleName)
	if err != nil {
		return preflightResult{}, err
	}
	_, profileExists, err := acct.Clients.IAM.InstanceProfileRoles(ctx, roleName)
	if err != nil {
		return preflightResult{}, err
	}
	if roleExists || profileExists {
		return preflightResult{}, &RefusedError{Message: fmt.Sprintf("a role for '%s' already exists (%s)", domain, roleName)}
	}

	accountID, err := acct.CallerAccountID(ctx)
	if err != nil {
		return preflightResult{}, err
	}
	return preflightResult{
		apps:      apps,
		account:   acct,
		zone:      zone,
		accountID: accountID,
	}, nil
}

func validateAccountDomain(domain, accountDomain string) error {
	if domain == accountDomain || strings.HasSuffix(domain, "."+accountDomain) {
		return nil
	}
	return &RefusedError{Message: fmt.Sprintf("'%s' does not end in the account domain '%s'", domain, accountDomain)}
}

func validateSpaces(domain, accountDomain string, spaces []account.Space) error {
	for _, existing := range spaces {
		switch {
		case existing.Domain == domain:
			return &RefusedError{Message: fmt.Sprintf("a space at '%s' already exists (%s)", domain, existing.ID)}
		case existing.Domain != accountDomain && strings.HasSuffix(domain, "."+existing.Domain):
			return &RefusedError{Message: fmt.Sprintf("'%s' lies under space '%s'", domain, existing.Domain)}
		case domain != accountDomain && strings.HasSuffix(existing.Domain, "."+domain):
			return &RefusedError{Message: fmt.Sprintf("'%s' would contain space '%s'", domain, existing.Domain)}
		}
	}
	return nil
}

// pushSecretsAndReport crosses the first mutation barrier. It deliberately
// emits nothing until every secret has been gathered and stored.
func pushSecretsAndReport(ctx context.Context, deps seam.Deps, domain string, result preflightResult, stdout io.Writer) ([]secrets.Entry, error) {
	entries, err := secrets.Push(ctx, deps, result.account, domain, result.apps)
	if err != nil {
		return nil, err
	}
	space.Step(stdout, "account", fmt.Sprintf("%s, %s", result.account.Properties.Domain, result.account.Properties.Region))
	space.Step(stdout, "domain", fmt.Sprintf("zone %s %s", result.zone.Name, result.zone.ID))
	space.Step(stdout, "secrets", fmt.Sprintf("%d apps", len(entries)))
	return entries, nil
}
