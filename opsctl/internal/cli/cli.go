// Package cli is the opsctl command-line interface.
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"

	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

type exitCode int

const (
	exitOK      exitCode = 0
	exitFail    exitCode = 1
	exitUsage   exitCode = 2
	exitRefused exitCode = 3
)

// usageText is the top-level usage, byte for byte from D2.
const usageText = `Usage: opsctl [options] <command> [arguments]

Operate the ikigenba platform host. Must run as root.

Commands:
  backup    back up a service's files to S3
  cert      obtain and inspect the host's certificate
  config    read and write the host configuration store
  dns       manage DNS records in the zones opsctl owns
  host      back up and restore the host's own configuration
  init      run the setup sequence behind one preflight
  install   install an app from a built file
  nginx     generate the platform's nginx configuration
  restart   restart an installed app's service
  restore   restore a service from its backups
  retire    stop every service and take the host's final backup
  status    print every installed app, its version and its state
  uninstall take an app off the host, keeping its data
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: opsctl must run as root

Run 'opsctl <command> --help' for details on a command.
`

// version is the opsctl version (vMAJOR.MINOR.PATCH), set in source.
var version = "v0.1.0"

// Deps carries what a command cannot be deterministic about.
type Deps struct {
	Root       string                                                   // filesystem root every host path is resolved under ("/" in production)
	EUID       int                                                      // effective user id of the process
	Getenv     func(key string) string                                  // process environment; nil reads as empty
	DNS        dns.Env                                                  // provider registry and resolver
	LookPath   func(file string) (string, error)                        // PATH lookup; nil uses exec.LookPath
	LookupHost func(ctx context.Context, host string) ([]string, error) // name resolution; nil uses net.DefaultResolver.LookupHost
	Execute    func(context.Context, host.Command) (host.Result, error)
	Now        func() time.Time
	Cloud      cloud.Env
}

func (d Deps) getenv(key string) string {
	if d.Getenv == nil {
		return ""
	}
	return d.Getenv(key)
}

func (d Deps) lookPath(file string) (string, error) {
	if d.LookPath == nil {
		return exec.LookPath(file)
	}
	return d.LookPath(file)
}

func (d Deps) lookupHost(ctx context.Context, host string) ([]string, error) {
	if d.LookupHost == nil {
		return net.DefaultResolver.LookupHost(ctx, host)
	}
	return d.LookupHost(ctx, host)
}

// Run executes the CLI. args are the program arguments without the program
// name; all I/O flows through the injected streams and every environmental
// dependency through deps. Run never terminates the process; it returns
// the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int {
	_ = stdin
	return int(run(args, stdout, stderr, normalizeDeps(deps)))
}

func normalizeDeps(deps Deps) Deps {
	if deps.Getenv == nil {
		deps.Getenv = func(string) string { return "" }
	}
	if deps.LookPath == nil {
		deps.LookPath = exec.LookPath
	}
	if deps.LookupHost == nil {
		deps.LookupHost = net.DefaultResolver.LookupHost
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return deps
}

func run(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	help, showVersion, rest, err := parseTopLevel(args)
	if err != nil {
		writeUsageError(stderr, "unknown option '"+diagnosticArg(unknownTopLevelOption(args))+"'")
		return exitUsage
	}
	if help {
		return writeOut(stdout, usageText)
	}
	if showVersion {
		return writeOut(stdout, version+"\n")
	}
	if len(rest) == 0 {
		writeUsageError(stderr, "no command given")
		return exitUsage
	}
	return dispatch(rest[0], rest[1:], stdout, stderr, deps)
}

type topLevelOption int

const (
	topLevelUnknown topLevelOption = iota
	topLevelHelp
	topLevelVersion
)

func classifyTopLevelOption(arg string) topLevelOption {
	switch arg {
	case "-h", "--help":
		return topLevelHelp
	case "-V", "--version":
		return topLevelVersion
	default:
		return topLevelUnknown
	}
}

func unknownTopLevelOption(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			break
		}
		if classifyTopLevelOption(arg) == topLevelUnknown {
			return arg
		}
	}
	return ""
}

func parseTopLevel(args []string) (help, showVersion bool, rest []string, err error) {
	if option := unknownTopLevelOption(args); option != "" {
		return false, false, nil, errors.New("unknown option")
	}
	for index, arg := range args {
		switch classifyTopLevelOption(arg) {
		case topLevelHelp:
			help = true
		case topLevelVersion:
			showVersion = true
		default:
			return help, showVersion, args[index:], nil
		}
	}
	return help, showVersion, nil, nil
}

func writeOut(w io.Writer, s string) exitCode {
	if _, err := io.WriteString(w, s); err != nil {
		return exitFail
	}
	return exitOK
}

func writeUsageError(stderr io.Writer, message string) {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl --help' for usage\n")
}

// diagnosticArg keeps a user-supplied token on one diagnostic line.
func diagnosticArg(s string) string {
	if !strings.ContainsAny(s, "\n\r") {
		return s
	}
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

func dispatch(name string, args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	switch name {
	case "host", "restore", "retire":
		return runCommandFrame(name, args, stdout, stderr, deps)
	case "backup":
		return runBackup(args, stdout, stderr, deps)
	case "cert":
		return runCert(args, stdout, stderr, deps)
	case "config":
		return runConfig(args, stdout, stderr, deps)
	case "dns":
		return runDNS(args, stdout, stderr, deps)
	case "init":
		return runInit(args, stdout, stderr, deps)
	case "install":
		return runInstall(args, stdout, stderr, deps)
	case "nginx":
		return runNginx(args, stdout, stderr, deps)
	case "restart", "uninstall":
		return runLifecycleAction(name, args, stdout, stderr, deps)
	case "status":
		return runStatus(args, stdout, stderr, deps)
	case "version":
		return writeOut(stdout, version+"\n")
	default:
		writeUsageError(stderr, "unknown command '"+diagnosticArg(name)+"'")
		return exitUsage
	}
}

func isCommandHelp(args []string) bool {
	return len(args) > 0 && classifyTopLevelOption(args[0]) == topLevelHelp
}

func requireRoot(deps Deps, stderr io.Writer) exitCode {
	if deps.EUID == 0 {
		return exitOK
	}
	_, _ = io.WriteString(stderr, "opsctl: must run as root\n")
	return exitRefused
}

// runCommandFrame provides grammar for actions whose domain is not built yet.
func runCommandFrame(name string, args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		return writeOut(stdout, "Usage: opsctl "+name+" [arguments]\n")
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	writeDiagnostic(stderr, errors.New(diagnosticArg(name)+": operation is not implemented"))
	return exitFail
}

// writeDiagnostic keeps operation errors and captured process output on stderr.
func writeDiagnostic(stderr io.Writer, err error) {
	message, detail, _ := strings.Cut(err.Error(), "\n")
	_, _ = io.WriteString(stderr, "opsctl: "+diagnosticArg(message)+"\n")
	commandErrors := joinedCommandErrors(err)
	if len(commandErrors) > 1 {
		_, _ = io.WriteString(stderr, "\n")
		for _, commandErr := range commandErrors {
			_, _ = io.WriteString(stderr, diagnosticArg(commandErr.Error())+"\n")
			quoteCapture(stderr, commandErr.Result.Stdout)
			quoteCapture(stderr, commandErr.Result.Stderr)
		}
		return
	}
	if len(commandErrors) == 1 {
		commandErr := commandErrors[0]
		if len(commandErr.Result.Stdout)+len(commandErr.Result.Stderr) == 0 {
			return
		}
		_, _ = io.WriteString(stderr, "\n")
		quoteCapture(stderr, commandErr.Result.Stdout)
		quoteCapture(stderr, commandErr.Result.Stderr)
		return
	}
	if detail != "" {
		_, _ = io.WriteString(stderr, "\n"+strings.TrimLeft(detail, "\n"))
		if !strings.HasSuffix(detail, "\n") {
			_, _ = io.WriteString(stderr, "\n")
		}
	}
}

// joinedCommandErrors returns the command failure from an ordinary error, or
// one failure from each branch when an operation retained multiple failures.
func joinedCommandErrors(err error) []*host.CommandError {
	for current := err; current != nil; {
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			var commandErrors []*host.CommandError
			for _, branch := range joined.Unwrap() {
				var commandErr *host.CommandError
				if errors.As(branch, &commandErr) {
					commandErrors = append(commandErrors, commandErr)
				}
			}
			return commandErrors
		}
		unwrapper, ok := current.(interface{ Unwrap() error })
		if !ok {
			break
		}
		current = unwrapper.Unwrap()
	}
	var commandErr *host.CommandError
	if errors.As(err, &commandErr) {
		return []*host.CommandError{commandErr}
	}
	return nil
}

func quoteCapture(stderr io.Writer, capture []byte) {
	for len(capture) > 0 {
		line, rest, found := bytes.Cut(capture, []byte("\n"))
		_, _ = io.WriteString(stderr, "> ")
		_, _ = stderr.Write(line)
		_, _ = io.WriteString(stderr, "\n")
		if !found {
			return
		}
		capture = rest
	}
}
