package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const (
	helpCommand = "devctl deploy --help"
	helpText    = `Usage: devctl --account <name> deploy <domain> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
it from there. The app and tag (v<semver>) are read from the file name.
`
)

type invocation struct {
	domain  string
	file    string
	app     string
	version string
	path    string
}

// Run executes a deploy command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	invocation, help, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if help {
		_, _ = fmt.Fprint(stdout, helpText)
		return nil
	}
	invocation, err = validateFile(invocation, deps.Dir)
	if err != nil {
		return err
	}
	manifest, err := inspectArchive(ctx, invocation, deps.Defaults())
	if err != nil {
		return err
	}
	space.Step(stdout, "file", invocation.app+" "+invocation.version)

	acct, err := account.Open(ctx, deps, profile)
	if err != nil {
		return err
	}
	targetSpace, err := acct.Space(ctx, invocation.domain)
	if err != nil {
		return err
	}
	if targetSpace.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: invocation.domain, State: targetSpace.State}
	}

	held, err := secrets.Names(ctx, acct, invocation.domain, invocation.app)
	if err != nil {
		return err
	}
	heldSet := make(map[string]struct{}, len(held))
	for _, name := range held {
		heldSet[name] = struct{}{}
	}
	required := make(map[string]struct{}, len(manifest.Secrets))
	for _, name := range manifest.Secrets {
		required[name] = struct{}{}
	}
	missing := make([]string, 0)
	for name := range required {
		if _, ok := heldSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return &MissingSecretsError{App: invocation.app, Domain: invocation.domain, Profile: profile, Names: missing}
	}
	space.Step(stdout, "secrets", fmt.Sprintf("%d keys", len(required)))

	artifact, err := os.ReadFile(invocation.path)
	if err != nil {
		return err
	}
	filename := filepath.Base(invocation.file)
	key := ObjectKey(invocation.domain, filename)
	bucket := acct.Properties.BackupBucket
	if err := acct.Clients.S3.PutObject(ctx, bucket, key, bytes.NewReader(artifact), int64(len(artifact))); err != nil {
		return err
	}
	space.Step(stdout, "upload", "-> "+bucket+"/"+key)

	uri := "s3://" + bucket + "/" + key
	if _, err := (host.Host{Address: targetSpace.Address, Deps: deps}).Sudo(ctx, "install", "opsctl", "install", uri); err != nil {
		return err
	}
	space.Step(stdout, "install", "opsctl installed "+invocation.app)
	return nil
}

// ObjectKey returns the deploy object key for an artifact basename.
func ObjectKey(domain, filename string) string {
	return domain + "/deploy/" + filename
}

func parseInvocation(args []string) (invocation, bool, error) {
	for _, argument := range args {
		if argument == "--help" || argument == "-h" {
			return invocation{}, true, nil
		}
	}
	for _, argument := range args {
		if strings.HasPrefix(argument, "-") {
			return invocation{}, false, usage("unknown option '" + argument + "'")
		}
	}
	if len(args) < 2 {
		return invocation{}, false, usage("deploy needs <domain> and <file>")
	}
	if len(args) > 2 {
		return invocation{}, false, usage("deploy takes only <domain> and <file>")
	}
	return invocation{domain: args[0], file: args[1]}, false, nil
}

func validateFile(value invocation, dir string) (invocation, error) {
	path := value.file
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return invocation{}, &NoFileError{Path: value.file}
	}

	app, version, err := appref.ParseFile(filepath.Base(value.file))
	if err != nil {
		return invocation{}, &FileError{
			Path:   value.file,
			Reason: "name is not <app>-v<semver>.tar.xz",
		}
	}
	value.app = app
	value.version = version
	value.path = path
	return value, nil
}

func inspectArchive(ctx context.Context, value invocation, deps seam.Deps) (checkout.Manifest, error) {
	list := seam.Cmd{
		Path: "tar",
		Args: []string{"-t", "-J", "-f", value.file},
		Dir:  deps.Dir,
	}
	result, err := runArchiveCommand(ctx, deps, list)
	if err != nil {
		return checkout.Manifest{}, err
	}
	members := make(map[string]struct{})
	for line := range strings.SplitSeq(string(result.Stdout), "\n") {
		if line != "" {
			members[line] = struct{}{}
		}
	}
	if _, ok := members[checkout.ManifestFile]; !ok {
		return checkout.Manifest{}, &FileError{Path: value.file, Reason: "no " + checkout.ManifestFile + " in the archive"}
	}

	extract := seam.Cmd{
		Path: "tar",
		Args: []string{"-x", "-J", "-O", "-f", value.file, checkout.ManifestFile},
		Dir:  deps.Dir,
	}
	result, err = runArchiveCommand(ctx, deps, extract)
	if err != nil {
		return checkout.Manifest{}, err
	}
	manifest, err := checkout.DecodeManifest(bytes.NewReader(result.Stdout))
	if err != nil {
		return checkout.Manifest{}, &FileError{Path: value.file, Reason: checkout.ManifestFile + ": " + err.Error()}
	}
	if manifest.App != value.app {
		return checkout.Manifest{}, &FileError{Path: value.file, Reason: "manifest app does not match file name"}
	}
	if _, ok := members["bin/"+manifest.App]; !ok {
		return checkout.Manifest{}, &FileError{Path: value.file, Reason: "no bin/" + manifest.App + " in the archive"}
	}
	return manifest, nil
}

func runArchiveCommand(ctx context.Context, deps seam.Deps, command seam.Cmd) (seam.Result, error) {
	result, err := deps.Exec(ctx, command)
	if err != nil {
		return seam.Result{}, fmt.Errorf("%s: %w", command.Path, err)
	}
	if result.ExitCode != 0 {
		label := strings.Join(append([]string{command.Path}, command.Args...), " ")
		return seam.Result{}, &ProcessError{Label: label, Status: result.ExitCode, Stderr: string(result.Stderr)}
	}
	return result, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: helpCommand}
}
