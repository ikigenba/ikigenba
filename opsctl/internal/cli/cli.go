// Package cli is the opsctl command-line interface.
package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
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

func unknownTopLevelOption(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			break
		}
		switch arg {
		case "-h", "--help", "-V", "--version":
			continue
		default:
			return arg
		}
	}
	return ""
}

func parseTopLevel(args []string) (help, showVersion bool, rest []string, err error) {
	if option := unknownTopLevelOption(args); option != "" {
		return false, false, nil, flag.ErrHelp
	}

	fs := flag.NewFlagSet("opsctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	boolFlag(fs, "h", "help", &help)
	boolFlag(fs, "V", "version", &showVersion)
	if err = fs.Parse(args); err != nil {
		return false, false, nil, err
	}
	return help, showVersion, fs.Args(), nil
}

func boolFlag(fs *flag.FlagSet, short, long string, dest *bool) {
	fs.BoolVar(dest, short, false, "")
	fs.BoolVar(dest, long, false, "")
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
	case "backup", "cert", "host", "install", "nginx", "restart", "restore", "retire", "status", "uninstall":
		return runCommandFrame(name, args, stdout, stderr, deps)
	case "config":
		return runConfig(args, stdout, stderr, deps)
	case "dns":
		return runDNS(args, stdout, stderr, deps)
	case "init":
		return runInit(args, stdout, stderr, deps)
	case "version":
		return writeOut(stdout, version+"\n")
	default:
		writeUsageError(stderr, "unknown command '"+diagnosticArg(name)+"'")
		return exitUsage
	}
}

func isCommandHelp(args []string) bool {
	return len(args) > 0 && (args[0] == "-h" || args[0] == "--help")
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
	writeDiagnostic(stderr, errors.New(name+": operation is not implemented"))
	return exitFail
}

// writeDiagnostic keeps operation errors and captured process output on stderr.
func writeDiagnostic(stderr io.Writer, err error) {
	message, detail, _ := strings.Cut(err.Error(), "\n")
	_, _ = io.WriteString(stderr, "opsctl: "+diagnosticArg(message)+"\n")
	var commandErr *host.CommandError
	if errors.As(err, &commandErr) {
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
