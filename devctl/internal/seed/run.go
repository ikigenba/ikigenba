// Package seed fills a space from snapshots in a golden set or another space.
package seed

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/golden"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const usage = `Usage: devctl seed <space> <source>

Give <space> the data of <source>, a golden set or another space. Each app's
newest snapshot in <source> is copied under <space>'s own seed/ prefix and
put back there with 'opsctl restore <app> --from <uri>', which replaces the
app's etc/, state/ and database, and writes its etc/env from <space>'s own
secrets. An app on <space> that <source> holds no snapshot of is left alone.

<source> is a golden set when one has that name, and otherwise the space with
that label. A space's full domain always names the space.
`

// UsageError reports invalid seed syntax.
type UsageError struct {
	Message string
	Help    string
}

func (e *UsageError) Error() string { return e.Message }

// ExitCode returns the syntax failure status.
func (e *UsageError) ExitCode() int { return 2 }

// Detail returns the usage hint.
func (e *UsageError) Detail() string { return "see '" + e.Help + "' for usage" }

// NotASourceError reports an invalid source operand.
type NotASourceError struct{ Operand string }

func (e *NotASourceError) Error() string { return "'" + e.Operand + "' is not a golden set or a space" }

// ExitCode returns the invalid operand status.
func (e *NotASourceError) ExitCode() int { return 2 }

// NoSourceError reports a source with no usable snapshots.
type NoSourceError struct{ Operand string }

func (e *NoSourceError) Error() string {
	return "no golden set or space snapshots for '" + e.Operand + "'"
}

// ExitCode returns the missing source status.
func (e *NoSourceError) ExitCode() int { return 1 }

type source struct {
	label string
	full  bool
}
type snapshot struct{ app, file, key string }

// Run copies source snapshots into the target and restores them through opsctl.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			_, err := io.WriteString(stdout, usage)
			return err
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return &UsageError{Message: "unknown option '" + arg + "'", Help: "devctl seed --help"}
		}
	}
	if len(args) < 2 {
		return &UsageError{Message: "seed needs <space> and <source>", Help: "devctl seed --help"}
	}
	if len(args) > 2 {
		return &UsageError{Message: "seed takes only <space> and <source>", Help: "devctl seed --help"}
	}
	root, err := checkout.ReadRootFile(ctx, deps)
	if err != nil {
		return err
	}
	target, err := spaceref.Parse(args[0], root.Domain)
	if err != nil {
		return err
	}
	src, err := parseSource(args[1], root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}
	instance, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, target.Domain)
	if err != nil {
		return err
	}
	if instance.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: target.Domain, State: instance.State}
	}
	snapshots, description, err := findSource(ctx, session.Clients.S3, root.Domain, src)
	if err != nil {
		return err
	}
	if len(snapshots) == 0 {
		return &NoSourceError{Operand: args[1]}
	}
	space.Step(stdout, "source", fmt.Sprintf("%s, %d apps", description, len(snapshots)))
	prefix := space.SeedPrefix(target.Label)
	for _, item := range snapshots {
		key := prefix + item.app + "/" + item.file
		if err := session.Clients.S3.CopyObject(ctx, root.Domain, item.key, key); err != nil {
			return err
		}
		space.Step(stdout, "copy", item.app+" -> "+root.Domain+"/"+key)
	}
	remote := host.Host{Address: instance.Address, Deps: deps}
	for _, item := range snapshots {
		uri := "s3://" + root.Domain + "/" + prefix + item.app + "/" + item.file
		if _, err := remote.Sudo(ctx, "restore", "opsctl", "restore", item.app, "--from", uri); err != nil {
			return err
		}
		space.Step(stdout, "restore", "opsctl restore "+item.app+" --from "+uri)
	}
	objects, err := session.Clients.S3.ListObjects(ctx, root.Domain, prefix)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		space.Step(stdout, "clean", "nothing to delete")
		return nil
	}
	keys := make([]string, len(objects))
	for i, object := range objects {
		keys[i] = object.Key
	}
	if err := session.Clients.S3.DeleteObjects(ctx, root.Domain, keys); err != nil {
		return err
	}
	space.Step(stdout, "clean", fmt.Sprintf("%d objects deleted", len(keys)))
	return nil
}

func parseSource(operand, root string) (source, error) {
	if strings.HasSuffix(operand, "."+root) {
		label := strings.TrimSuffix(operand, "."+root)
		if !spaceref.ValidLabel(label) {
			return source{}, &NotASourceError{Operand: operand}
		}
		if label == spaceref.ReservedLabel {
			return source{}, &spaceref.ReservedLabelError{Label: label}
		}
		return source{label: label, full: true}, nil
	}
	if !spaceref.ValidLabel(operand) {
		return source{}, &NotASourceError{Operand: operand}
	}
	return source{label: operand}, nil
}

func findSource(ctx context.Context, s3 cloud.S3, root string, src source) ([]snapshot, string, error) {
	if !src.full {
		prefix := golden.Prefix(src.label)
		objects, err := s3.ListObjects(ctx, root, prefix)
		if err != nil {
			return nil, "", err
		}
		if items := newest(objects, prefix); len(items) != 0 {
			return items, "golden set " + src.label, nil
		}
		if src.label == spaceref.ReservedLabel {
			return nil, "", nil
		}
	}
	prefix := space.SnapshotPrefix(src.label)
	objects, err := s3.ListObjects(ctx, root, prefix)
	if err != nil {
		return nil, "", err
	}
	return newest(objects, prefix), "space " + src.label + "." + root, nil
}

func newest(objects []cloud.Object, prefix string) []snapshot {
	apps := make(map[string]snapshot)
	for _, object := range objects {
		if !strings.HasPrefix(object.Key, prefix) {
			continue
		}
		app, file, found := strings.Cut(strings.TrimPrefix(object.Key, prefix), "/")
		if !found || !appref.ValidName(app) || file == "" || strings.Contains(file, "/") {
			continue
		}
		if old, ok := apps[app]; !ok || file > old.file {
			apps[app] = snapshot{app: app, file: file, key: object.Key}
		}
	}
	items := make([]snapshot, 0, len(apps))
	for _, item := range apps {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].app < items[j].app })
	return items
}
