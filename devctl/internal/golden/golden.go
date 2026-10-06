// Package golden captures a space's snapshots as a named golden set.
package golden

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const usage = `Usage: devctl golden <subcommand> [arguments]

Keep a space's data as a named golden set, one snapshot per app under
golden/<set>/ in the backup bucket, for 'devctl seed' to give to any space.
A golden set carries no secrets.

Subcommands:
  capture <space> <set>   snapshot every app on <space> and make the snapshots golden set <set>

Run 'devctl golden <subcommand> --help' for details.
`

// UsageError reports invalid command syntax.
type UsageError struct {
	Message string
	Help    string
}

func (e *UsageError) Error() string { return e.Message }

// ExitCode returns the usage exit status.
func (e *UsageError) ExitCode() int { return 2 }

// Detail returns the command's usage hint.
func (e *UsageError) Detail() string { return "see '" + e.Help + "' for usage" }

// NoAppsError reports a space with no apps to capture.
type NoAppsError struct{ Domain string }

func (e *NoAppsError) Error() string { return "'" + e.Domain + "' has no apps to capture" }

// ExitCode returns the capture failure status.
func (e *NoAppsError) ExitCode() int { return 1 }

// ReportError reports an invalid snapshot report line.
type ReportError struct{ Line string }

func (e *ReportError) Error() string {
	return "snapshot: unexpected line from opsctl snapshot: '" + e.Line + "'"
}

// ExitCode returns the capture failure status.
func (e *ReportError) ExitCode() int { return 1 }

// Prefix returns the bucket prefix for a golden set.
func Prefix(set string) string { return "golden/" + set + "/" }

// Run captures every app of a running space into a golden set.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			_, err := io.WriteString(stdout, usage)
			return err
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usageError("unknown option '" + arg + "'")
		}
	}
	if len(args) == 0 {
		return usageError("golden needs <subcommand>")
	}
	if args[0] != "capture" {
		return usageError("unknown subcommand '" + args[0] + "'")
	}
	if len(args) < 3 {
		return usageError("golden capture needs <space> and <set>")
	}
	if len(args) > 3 {
		return usageError("golden capture takes only <space> and <set>")
	}
	if !spaceref.ValidLabel(args[2]) {
		return &spaceref.InvalidLabelError{Operand: args[2]}
	}
	root, err := checkout.ReadRootFile(ctx, deps.Defaults())
	if err != nil {
		return err
	}
	ref, err := spaceref.Parse(args[1], root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}
	found, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, ref.Domain)
	if err != nil {
		return err
	}
	if found.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: ref.Domain, State: found.State}
	}
	remote := host.Host{Address: found.Address, Deps: deps}
	status, err := remote.Sudo(ctx, "", "opsctl", "status")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status.Stdout) == "" {
		return &NoAppsError{Domain: ref.Domain}
	}
	output, err := remote.Sudo(ctx, "snapshot", "opsctl", "snapshot")
	if err != nil {
		return err
	}
	reports, err := readReports(output.Stdout, root.Domain, ref.Label)
	if err != nil {
		return err
	}
	space.Step(stdout, "snapshot", fmt.Sprintf("opsctl snapshot, %d apps", len(reports)))
	written := make(map[string]bool, len(reports))
	for _, report := range reports {
		key := Prefix(args[2]) + report.app + "/" + report.file
		if err := session.Clients.S3.CopyObject(ctx, root.Domain, report.source, key); err != nil {
			return err
		}
		written[key] = true
		space.Step(stdout, "copy", report.app+" -> "+root.Domain+"/"+key)
	}
	objects, err := session.Clients.S3.ListObjects(ctx, root.Domain, Prefix(args[2]))
	if err != nil {
		return err
	}
	var obsolete []string
	for _, object := range objects {
		if !written[object.Key] {
			obsolete = append(obsolete, object.Key)
		}
	}
	detail := "nothing to delete"
	if len(obsolete) > 0 {
		if err := session.Clients.S3.DeleteObjects(ctx, root.Domain, obsolete); err != nil {
			return err
		}
		detail = fmt.Sprintf("%d objects deleted", len(obsolete))
	}
	space.Step(stdout, "prune", detail)
	return nil
}

func usageError(message string) *UsageError {
	return &UsageError{Message: message, Help: "devctl golden --help"}
}

type report struct{ app, file, source string }

func readReports(text, bucket, label string) ([]report, error) {
	var reports []report
	seen := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		app, rest, ok := strings.Cut(line, ": ok (")
		if !ok || !appref.ValidName(app) || seen[app] || !strings.HasSuffix(rest, ")") {
			return nil, &ReportError{Line: line}
		}
		uri, detail, ok := strings.Cut(strings.TrimSuffix(rest, ")"), ", ")
		prefix := "s3://" + bucket + "/" + space.SnapshotPrefix(label) + app + "/"
		file, validPrefix := strings.CutPrefix(uri, prefix)
		if !ok || detail == "" || !validPrefix || file == "" || strings.ContainsAny(file, "/, ") {
			return nil, &ReportError{Line: line}
		}
		seen[app] = true
		reports = append(reports, report{app: app, file: file, source: strings.TrimPrefix(uri, "s3://"+bucket+"/")})
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].app < reports[j].app })
	return reports, nil
}
