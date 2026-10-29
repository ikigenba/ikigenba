package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const userUsage = `Usage: opsctl user <subcommand> [arguments]

Manage the people who sign in to this host over SSH: their keys, their
business groups, and whether they may sign in.

Subcommands:
  add NAME GROUP KEY   create NAME in shell and GROUP, signing in with KEY
  list                 print every user as one JSON document
  key add NAME KEY     let NAME sign in with KEY; succeeds if NAME has it
  key remove NAME KEY  take KEY from NAME; succeeds if NAME lacks it
  join NAME GROUP      add NAME to GROUP; succeeds if already a member
  leave NAME GROUP     take NAME out of GROUP; succeeds if not a member
  lock NAME            end NAME's sessions and refuse every sign-in
  unlock NAME          let a locked NAME sign in again
  delete NAME          remove a locked NAME and their home

NAME is a Linux account name, unique on the host. GROUP is a business group
made by 'opsctl group add'. KEY is one argument, a whole ed25519 OpenSSH
public key line, 'ssh-ed25519 BODY [COMMENT]'; keys match on type and BODY,
never COMMENT. A deleted user's files under /srv/SLUG go to the account SLUG.
`

func userUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl user --help' for usage\n")
	return exitUsage
}

func runUser(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, userUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) == 0 {
		return userUsageError(stderr, "no user subcommand given")
	}
	action, operands := args[0], args[1:]
	if action == "key" {
		if len(operands) == 0 {
			return userUsageError(stderr, "no user key subcommand given")
		}
		if operands[0] != "add" && operands[0] != "remove" {
			return userUsageError(stderr, "unknown user key subcommand '"+diagnosticArg(operands[0])+"'")
		}
		action, operands = "key "+operands[0], operands[1:]
	}
	count, needs, takes := 0, "", ""
	switch action {
	case "add":
		count, needs, takes = 3, "NAME GROUP KEY", "NAME, GROUP and KEY"
	case "key add", "key remove":
		count, needs, takes = 2, "NAME KEY", "NAME and KEY"
	case "join", "leave":
		count, needs, takes = 2, "NAME GROUP", "NAME and GROUP"
	case "lock", "unlock", "delete":
		count, needs, takes = 1, "NAME", "one NAME"
	case "list":
		takes = "no arguments"
	default:
		return userUsageError(stderr, "unknown user subcommand '"+diagnosticArg(action)+"'")
	}
	if len(operands) < count {
		return userUsageError(stderr, "user "+action+" needs "+needs)
	}
	if len(operands) > count {
		return userUsageError(stderr, "user "+action+" takes "+takes)
	}
	if count > 0 && !accounts.ValidName(operands[0]) {
		return userUsageError(stderr, "invalid name: "+diagnosticArg(operands[0]))
	}
	if action == "add" || action == "join" || action == "leave" {
		if !accounts.ValidName(operands[1]) {
			return userUsageError(stderr, "invalid name: "+diagnosticArg(operands[1]))
		}
	}
	var key accounts.Key
	if action == "add" || strings.HasPrefix(action, "key ") {
		var err error
		key, err = accounts.ParseKey(operands[len(operands)-1])
		if err != nil {
			return userUsageError(stderr, "invalid key: not an ed25519 OpenSSH public key")
		}
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	ctx := context.Background()
	var err error
	switch action {
	case "add":
		err = accounts.AddUser(ctx, env, operands[0], operands[1], key)
	case "key add":
		err = accounts.AddKey(ctx, env, operands[0], key)
	case "key remove":
		err = accounts.RemoveKey(ctx, env, operands[0], key)
	case "join":
		err = accounts.JoinGroup(ctx, env, operands[0], operands[1])
	case "leave":
		err = accounts.LeaveGroup(ctx, env, operands[0], operands[1])
	case "lock":
		err = accounts.LockUser(ctx, env, operands[0])
	case "unlock":
		err = accounts.UnlockUser(ctx, env, operands[0])
	case "delete":
		err = accounts.DeleteUser(ctx, env, operands[0])
	case "list":
		var list accounts.UserList
		list, err = accounts.ListUsers(ctx, env)
		if err == nil {
			for i := range list.Users {
				if value := list.Users[i].LastSignIn; value != nil {
					wholeSecond := value.UTC().Truncate(time.Second)
					list.Users[i].LastSignIn = &wholeSecond
				}
			}
			encoder := json.NewEncoder(stdout)
			encoder.SetEscapeHTML(false)
			err = encoder.Encode(list)
		}
	}
	if err != nil {
		return accountActionError(stderr, err)
	}
	return exitOK
}
