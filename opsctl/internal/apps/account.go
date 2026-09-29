package apps

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// EnsureAccount ensures the host has an ikigenba service account.
func EnsureAccount(ctx context.Context, env host.Env) error {
	result, err := env.Execute(ctx, host.Command{Name: "id", Args: []string{"--user", "ikigenba"}})
	if err != nil {
		return &host.CommandError{Label: "inspect ikigenba account", Result: result, Err: err}
	}
	if result.ExitCode == 1 {
		created, createErr := env.Execute(ctx, host.Command{
			Name: "useradd", Args: []string{"--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "--user-group", "ikigenba"},
		})
		if createErr != nil || created.ExitCode != 0 {
			return &host.CommandError{Label: "create ikigenba account", Result: created, Err: createErr}
		}
		return nil
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: "inspect ikigenba account", Result: result}
	}

	uidText := strings.TrimSpace(string(result.Stdout))
	uid, parseErr := strconv.ParseUint(uidText, 10, 64)
	if parseErr != nil {
		return fmt.Errorf("inspect ikigenba account: invalid uid %q: %w", uidText, parseErr)
	}
	if uid == 0 {
		return fmt.Errorf("ikigenba account must not be root")
	}

	group, groupErr := env.Execute(ctx, host.Command{Name: "id", Args: []string{"--group", "--name", "ikigenba"}})
	if groupErr != nil || group.ExitCode != 0 {
		return &host.CommandError{Label: "inspect ikigenba primary group", Result: group, Err: groupErr}
	}
	if strings.TrimSpace(string(group.Stdout)) != "ikigenba" {
		return fmt.Errorf("ikigenba account primary group is %q, want ikigenba", strings.TrimSpace(string(group.Stdout)))
	}
	return nil
}
