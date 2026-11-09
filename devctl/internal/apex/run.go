package apex

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const helpCommand = "devctl apex --help"

const usageText = `Usage: devctl apex <subcommand> [arguments]

Point the root domain at one app on one space, say where it points, or take
it away. The root is an A record at the space's address; the space's host
carries the root in its certificate and routes it to the app.

Subcommands:
  set <app>.<space>   make <app> on <space> answer at the root
  show                print the app and address the root points at
  clear               remove the root's record and the holder's apex configuration

Run 'devctl apex <subcommand> --help' for details.
`

type invocation struct {
	subcommand string
	operand    string
	help       bool
}

// Run executes an apex command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	inv, err := parse(args)
	if err != nil {
		return err
	}
	if inv.help {
		_, err = io.WriteString(stdout, usageText)
		return err
	}

	root, err := checkout.ReadRootFile(ctx, deps)
	if err != nil {
		return err
	}
	var app spaceref.App
	if inv.subcommand == "set" {
		app, err = spaceref.ParseApp(inv.operand, root.Domain)
		if err != nil {
			return err
		}
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}

	switch inv.subcommand {
	case "set":
		return runSet(ctx, stdout, deps, session.Clients, root.Domain, app)
	case "show":
		return runShow(ctx, stdout, deps, session.Clients, root.Domain)
	default:
		return runClear(ctx, stdout, deps, session.Clients, root.Domain)
	}
}

func parse(args []string) (invocation, error) {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return invocation{help: true}, nil
		}
	}
	if len(args) == 0 {
		return invocation{}, usage("apex needs <subcommand>")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return invocation{}, usage("unknown option '" + arg + "'")
		}
	}

	inv := invocation{subcommand: args[0]}
	switch inv.subcommand {
	case "set":
		if len(args) == 1 {
			return invocation{}, usage("apex set needs <app>.<space>")
		}
		if len(args) > 2 {
			return invocation{}, usage("apex set takes only <app>.<space>")
		}
		inv.operand = args[1]
	case "show", "clear":
		if len(args) > 1 {
			return invocation{}, usage("apex " + inv.subcommand + " takes no arguments")
		}
	default:
		return invocation{}, usage("unknown subcommand '" + inv.subcommand + "'")
	}
	return inv, nil
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: helpCommand}
}

type setPreflight struct {
	target cloud.Space
	zone   cloud.Zone
	holder cloud.Space
}

func runSet(ctx context.Context, stdout io.Writer, deps seam.Deps, clients cloud.Clients, root string, app spaceref.App) error {
	pre, err := preflightSet(ctx, clients, root, app)
	if err != nil {
		return err
	}

	space.Step(stdout, "space", fmt.Sprintf("%s running, %s", pre.target.Domain, pre.target.Address))
	if err := GrantApex(ctx, stdout, clients.IAM, root, pre.zone, app.Space); err != nil {
		return err
	}

	if err := configureHost(ctx, deps, pre.target.Domain, "host", func(target host.Host) error {
		return hostsetup.SetKey(ctx, target, "host", hostsetup.KeyHostApex, app.Name)
	}); err != nil {
		return err
	}
	space.Step(stdout, "host", "host.apex="+app.Name+", certificate obtained, nginx applied")

	if err := PointRoot(ctx, stdout, deps, clients.Route53, pre.zone, root, pre.target.Address); err != nil {
		return err
	}
	if err := ClearPrevious(ctx, stdout, deps, clients.IAM, root, pre.zone, pre.holder); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s %s\n", root, app.Hostname)
	return err
}

func preflightSet(ctx context.Context, clients cloud.Clients, root string, app spaceref.App) (setPreflight, error) {
	result := setPreflight{}
	var err error
	result.target, err = cloud.LookupInstance(ctx, clients.EC2, root, app.Space.Domain)
	if err != nil {
		return result, err
	}
	if result.target.State != cloud.StateRunning {
		return result, &space.NotRunningError{Domain: result.target.Domain, State: result.target.State}
	}
	result.zone, err = clients.Route53.Zone(ctx, root)
	if err != nil {
		return result, err
	}
	result.holder, err = CheckHolder(ctx, clients.Route53, clients.EC2, result.zone, root, result.target.Domain)
	return result, err
}

func configureHost(ctx context.Context, deps seam.Deps, domain, step string, configure func(host.Host) error) error {
	target := host.Host{Domain: domain, Deps: deps}
	if err := target.Wait(ctx); err != nil {
		return &StepError{Step: step, Err: err}
	}
	if err := configure(target); err != nil {
		return err
	}
	if _, err := target.Sudo(ctx, step, "opsctl", "cert", "obtain"); err != nil {
		return err
	}
	if _, err := target.Sudo(ctx, step, "opsctl", "nginx", "apply"); err != nil {
		return err
	}
	return nil
}

func find(ctx context.Context, clients cloud.Clients, root string) (cloud.Zone, space.ApexRecord, error) {
	zone, err := clients.Route53.Zone(ctx, root)
	if err != nil {
		return cloud.Zone{}, space.ApexRecord{}, err
	}
	record, err := space.FindApex(ctx, clients.Route53, clients.EC2, zone.ID, root)
	return zone, record, err
}

func runShow(ctx context.Context, stdout io.Writer, deps seam.Deps, clients cloud.Clients, root string) error {
	_, record, err := find(ctx, clients, root)
	if err != nil || !record.Found {
		return err
	}
	if record.Holder == "" {
		return &NotSpaceAddressError{Root: root, Values: record.Record.Values}
	}
	holder, err := cloud.LookupInstance(ctx, clients.EC2, root, record.Holder)
	if err != nil {
		return err
	}
	if holder.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: holder.Domain, State: holder.State}
	}
	value, set, err := hostsetup.GetKey(ctx, host.Host{Domain: holder.Domain, Deps: deps}, "show", hostsetup.KeyHostApex)
	if err != nil {
		return err
	}
	if !set {
		return &NoApexAppError{Root: root, Domain: holder.Domain}
	}
	_, err = fmt.Fprintf(stdout, "%s.%s %s\n", value, holder.Domain, holder.Address)
	return err
}

func runClear(ctx context.Context, stdout io.Writer, deps seam.Deps, clients cloud.Clients, root string) error {
	zone, record, err := find(ctx, clients, root)
	if err != nil {
		return err
	}
	if !record.Found {
		space.Step(stdout, "record", "already gone")
		return nil
	}
	if record.Holder == "" {
		_, err := clients.Route53.ChangeRecords(ctx, zone.ID, []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: record.Record}})
		if err != nil {
			return err
		}
		space.Step(stdout, "record", root+" deleted")
		return nil
	}

	prev, err := spaceref.Parse(record.Holder, root)
	if err != nil {
		return err
	}
	holder, err := cloud.LookupInstance(ctx, clients.EC2, root, record.Holder)
	if err != nil {
		return err
	}
	if holder.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: holder.Domain, State: holder.State}
	}

	space.Step(stdout, "space", fmt.Sprintf("%s running, %s", holder.Domain, holder.Address))
	if _, err := clients.Route53.ChangeRecords(ctx, zone.ID, []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: record.Record}}); err != nil {
		return err
	}
	space.Step(stdout, "record", root+" deleted")
	if err := clients.IAM.PutRolePolicy(ctx, space.RoleName(record.Holder), space.PolicyName, space.PolicyDocument(root, zone.ID, prev, false)); err != nil {
		return err
	}
	space.Step(stdout, "role", holder.Domain+" may no longer prove "+root)
	if err := configureHost(ctx, deps, holder.Domain, "host", func(target host.Host) error {
		return hostsetup.DelKey(ctx, target, "host", hostsetup.KeyHostApex)
	}); err != nil {
		return err
	}
	space.Step(stdout, "host", "host.apex removed, certificate obtained, nginx applied")
	return nil
}

// CheckHolder checks whether the apex can be given to target.
func CheckHolder(ctx context.Context, route53 cloud.Route53, ec2 cloud.EC2, zone cloud.Zone, root, target string) (cloud.Space, error) {
	apex, err := space.FindApex(ctx, route53, ec2, zone.ID, root)
	if err != nil {
		return cloud.Space{}, err
	}
	if apex.Found && apex.Holder == "" {
		return cloud.Space{}, &NotSpaceAddressError{Root: root, Values: apex.Record.Values}
	}
	if !apex.Found || apex.Holder == target {
		return cloud.Space{}, nil
	}
	prev, err := spaceref.Parse(apex.Holder, root)
	if err != nil {
		return cloud.Space{}, err
	}
	holder, err := cloud.LookupInstance(ctx, ec2, root, apex.Holder)
	if err != nil {
		return cloud.Space{}, err
	}
	if holder.State != cloud.StateRunning {
		return cloud.Space{}, &HolderStoppedError{Domain: holder.Domain, Label: prev.Label, State: holder.State}
	}
	return holder, nil
}

// GrantApex grants the space's role permission to prove the root domain.
func GrantApex(ctx context.Context, w io.Writer, iam cloud.IAM, root string, zone cloud.Zone, sp spaceref.Space) error {
	if err := iam.PutRolePolicy(ctx, space.RoleName(sp.Domain), space.PolicyName, space.PolicyDocument(root, zone.ID, sp, true)); err != nil {
		return err
	}
	space.Step(w, "role", sp.Domain+" may prove "+root)
	return nil
}

// PointRoot points the root A record at address and reports propagation.
func PointRoot(ctx context.Context, w io.Writer, deps seam.Deps, route53 cloud.Route53, zone cloud.Zone, root, address string) error {
	id, err := route53.ChangeRecords(ctx, zone.ID, []cloud.RecordChange{{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: root, Type: "A", TTL: space.RecordTTL, Values: []string{address}}}})
	if err != nil {
		return err
	}
	if err := space.WaitInsync(ctx, deps, route53, id); err != nil {
		return err
	}
	space.Step(w, "record", root+" -> "+address+", INSYNC")
	return nil
}

// ClearPrevious removes the old holder's apex permission and host setting.
func ClearPrevious(ctx context.Context, w io.Writer, deps seam.Deps, iam cloud.IAM, root string, zone cloud.Zone, holder cloud.Space) error {
	if holder.Domain == "" {
		space.Step(w, "previous", "none")
		return nil
	}
	prev, err := spaceref.Parse(holder.Domain, root)
	if err != nil {
		return err
	}
	if err := iam.PutRolePolicy(ctx, space.RoleName(holder.Domain), space.PolicyName, space.PolicyDocument(root, zone.ID, prev, false)); err != nil {
		return err
	}
	if err := configureHost(ctx, deps, holder.Domain, "previous", func(target host.Host) error {
		return hostsetup.DelKey(ctx, target, "previous", hostsetup.KeyHostApex)
	}); err != nil {
		return err
	}
	space.Step(w, "previous", holder.Domain+" cleared")
	return nil
}
