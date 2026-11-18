// Package hosts manages the IAM permissions of the hosts operator.
package hosts

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const helpCommand = "devctl hosts --help"

const usageText = `Usage: devctl hosts <subcommand> [arguments]

Make one space the hosts operator, or stop it being one. The hosts operator's
role has the managed policy <root>-hosts-operator, which lets the accounts
service on that space run hosts; at most one space is the hosts operator.

Subcommands:
  set <space>     make <space> the hosts operator
  clear <space>   stop <space> being the hosts operator

Run 'devctl hosts <subcommand> --help' for details.
`

// NoRoleError reports a missing space role.
type NoRoleError struct {
	Domain string
}

func (err *NoRoleError) Error() string { return "no role for '" + err.Domain + "'" }

// OperatorError reports the space currently marked as the hosts operator.
type OperatorError struct {
	Domain string
	Label  string
}

func (err *OperatorError) Error() string { return "'" + err.Domain + "' is the hosts operator" }

// ExitCode identifies the refusal as an operation failure.
func (*OperatorError) ExitCode() int { return 1 }

// Detail gives the command that clears the current operator.
func (err *OperatorError) Detail() string { return "run 'devctl hosts clear " + err.Label + "' first" }

// Run makes a space the hosts operator or removes that designation.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			_, err := io.WriteString(stdout, usageText)
			return err
		}
	}
	if err := validateArgs(args); err != nil {
		return err
	}

	root, err := checkout.ReadRootFile(ctx, deps)
	if err != nil {
		return err
	}
	sp, err := spaceref.Parse(args[1], root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}

	clients := session.Clients
	if args[0] == "set" {
		return runSet(ctx, stdout, clients, root.Domain, sp.Domain)
	}
	return runClear(ctx, stdout, clients.IAM, root.Domain, sp.Domain)
}

func validateArgs(args []string) error {
	if len(args) == 0 {
		return usage("hosts needs <subcommand>")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usage("unknown option '" + arg + "'")
		}
	}
	subcommand := args[0]
	if subcommand != "set" && subcommand != "clear" {
		return usage("unknown subcommand '" + subcommand + "'")
	}
	switch len(args) {
	case 1:
		return usage("hosts " + subcommand + " needs <space>")
	case 2:
		return nil
	default:
		return usage("hosts " + subcommand + " takes only <space>")
	}
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: helpCommand}
}

func runSet(ctx context.Context, stdout io.Writer, clients cloud.Clients, root, domain string) error {
	if _, err := cloud.LookupSpace(ctx, clients.EC2, root, domain); err != nil {
		return err
	}
	role := space.RoleName(domain)
	iam := clients.IAM
	if err := requireRole(ctx, iam, role, domain); err != nil {
		return err
	}
	policy := space.HostsPolicyName(root)
	arn, err := iam.ManagedPolicy(ctx, policy)
	if err != nil {
		return err
	}
	tagged, err := iam.RolesTagged(ctx, space.HostsTagKey, space.HostsTagValue)
	if err != nil {
		return err
	}
	if operator := otherOperator(tagged, role, root); operator.Domain != "" {
		return &OperatorError{Domain: operator.Domain, Label: operator.Label}
	}

	if err := iam.TagRole(ctx, role, space.HostsTagKey, space.HostsTagValue); err != nil {
		return err
	}
	space.Step(stdout, "tag", domain+" tagged hosts operator")

	if err := iam.AttachRolePolicy(ctx, role, arn); err != nil {
		return err
	}
	space.Step(stdout, "role", policy+" attached to "+domain)
	_, err = fmt.Fprintln(stdout, domain)
	return err
}

func otherOperator(tagged []string, role, root string) spaceref.Space {
	var first spaceref.Space
	for _, name := range tagged {
		if name == role || (first.Domain != "" && name >= first.Domain) {
			continue
		}
		if sp, err := spaceref.Parse(name, root); err == nil && sp.Domain == name {
			first = sp
		}
	}
	return first
}

func runClear(ctx context.Context, stdout io.Writer, iam cloud.IAM, root, domain string) error {
	role := space.RoleName(domain)
	if err := requireRole(ctx, iam, role, domain); err != nil {
		return err
	}
	detached, err := space.DetachHostsPolicy(ctx, iam, root, domain)
	if err != nil {
		return err
	}
	detail := "already detached"
	if detached {
		detail = space.HostsPolicyName(root) + " detached from " + domain
	}
	space.Step(stdout, "role", detail)

	_, tagged, err := iam.RoleTag(ctx, role, space.HostsTagKey)
	if err != nil {
		return err
	}
	if !tagged {
		space.Step(stdout, "tag", "already gone")
		return nil
	}
	if err := iam.UntagRole(ctx, role, space.HostsTagKey); err != nil {
		return err
	}
	space.Step(stdout, "tag", domain+" untagged")
	return nil
}

func requireRole(ctx context.Context, iam cloud.IAM, role, domain string) error {
	exists, err := iam.RoleExists(ctx, role)
	if err != nil {
		return err
	}
	if !exists {
		return &NoRoleError{Domain: domain}
	}
	return nil
}
