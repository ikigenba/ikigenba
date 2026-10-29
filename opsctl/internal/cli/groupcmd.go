package cli

import (
	"context"
	"encoding/json"
	"io"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const groupUsage = `Usage: opsctl group <subcommand> [arguments]

Manage the host's business groups. A group SLUG is a Linux group, a service
account of the same name, and the folder /srv/SLUG, a shared git repository
the group's members can write and no one else can read.

Subcommands:
  add SLUG      create the group, its account and /srv/SLUG, adopting a folder
                already there
  remove SLUG   remove the group and its account; refused while it has members;
                /srv/SLUG is kept
  list          print every business group and its member count as JSON
  members SLUG  print the group's members as JSON

SLUG must be valid as a Linux account name, and add refuses one any account or
group on the host already has.
`

func runGroup(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, groupUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) == 0 {
		return writeLifecycleUsageError(stderr, "group", "no group subcommand given")
	}
	sub, operands := args[0], args[1:]
	switch sub {
	case "list":
		if len(operands) != 0 {
			return writeLifecycleUsageError(stderr, "group", "group list takes no arguments")
		}
	case "add", "remove", "members":
		if len(operands) == 0 {
			return writeLifecycleUsageError(stderr, "group", "group "+sub+" needs SLUG")
		}
		if len(operands) != 1 {
			return writeLifecycleUsageError(stderr, "group", "group "+sub+" takes one SLUG")
		}
		if !accounts.ValidName(operands[0]) {
			return writeLifecycleUsageError(stderr, "group", "invalid name: "+diagnosticArg(operands[0]))
		}
	default:
		return writeLifecycleUsageError(stderr, "group", "unknown group subcommand '"+diagnosticArg(sub)+"'")
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	ctx := context.Background()
	var err error
	var result any
	switch sub {
	case "add":
		err = accounts.AddGroup(ctx, env, operands[0])
	case "remove":
		err = accounts.RemoveGroup(ctx, env, operands[0])
	case "list":
		result, err = accounts.ListGroups(ctx, env)
	case "members":
		result, err = accounts.GroupMembers(ctx, env, operands[0])
	}
	if err != nil {
		return accountActionError(stderr, err)
	}
	if result != nil {
		if err = json.NewEncoder(stdout).Encode(result); err != nil {
			writeDiagnostic(stderr, err)
			return exitFail
		}
	}
	return exitOK
}
