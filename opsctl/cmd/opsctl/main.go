// Command opsctl is the ikigai on-box platform CLI (PLAN §1.3). It implements the
// deploy-critical verbs install / rollback / prune over the versioned release-dir
// + atomic-symlink layout (PLAN §1.4) plus the box-provisioning verbs init-box
// (box-global substrate) and setup (per-app provisioning) (PLAN §D1). It runs on
// the box only (operators SSH in and run `sudo opsctl …`); all filesystem ops are
// rooted at OPSCTL_ROOT (default /opt) — and the system-config tree at
// OPSCTL_SYSROOT (default /) — so the core is fully testable against temp dirs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"opsctl/internal/opsctl"
)

// reorderArgs moves flag tokens ahead of positional tokens so the standard
// flag package — which stops scanning at the first non-flag token — accepts
// flags written AFTER positionals (e.g. `opsctl install ledger v0.1.0
// --artifact X`, the form bin/deploy emits, and `opsctl setup ledger --port N`).
// A bare `--` terminates flag scanning: everything after it is positional and is
// passed through verbatim. A flag that takes a separate value is detected by the
// known set of value-taking flags so the value token is not mistaken for a
// positional.
func reorderArgs(args []string, valueFlags map[string]bool) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			// If this flag takes a separate value (`--artifact X`, not `--artifact=X`)
			// pull the next token along as its value.
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(a, "=") && valueFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

const usage = `opsctl — ikigai on-box platform CLI

usage:
  opsctl init-box --default-app <app> --domain <d> --port <n> \
                  --apex-block <path> [--email <e>] [--skip-cert]
                                                     box-global substrate (apex block, /_authn,
                                                     conf.d/locations/, cert, renew timer)
  opsctl setup <app> [--port <n>] [--fragment <path>]
                                                     per-app provisioning (user, /opt/<app> tree,
                                                     systemd unit enabled-not-started, nginx fragment)
  opsctl install <app> <version> --artifact <path>   ship a version live (atomic swap)
  opsctl rollback <app> [version]                     repoint current to a prior release
  opsctl prune <app> [--keep N]                       bound on-box release history

env:
  OPSCTL_ROOT     install base (default /opt) — the /opt/<app> tree
  OPSCTL_SYSROOT  system-config base (default /) — the /etc + /var tree
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	root := os.Getenv("OPSCTL_ROOT")
	verb := os.Args[1]
	args := os.Args[2:]
	ctx := context.Background()

	var err error
	switch verb {
	case "init-box":
		err = cmdInitBox(ctx, root, args)
	case "setup":
		err = cmdSetup(ctx, root, args)
	case "install":
		err = cmdInstall(ctx, root, args)
	case "rollback":
		err = cmdRollback(ctx, root, args)
	case "prune":
		err = cmdPrune(ctx, root, args)
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "opsctl: unknown verb %q\n\n%s", verb, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "opsctl: %v\n", err)
		os.Exit(1)
	}
}

func cmdInstall(ctx context.Context, root string, args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	artifact := fs.String("artifact", "", "path to the static linux/amd64 artifact (required)")
	if err := fs.Parse(reorderArgs(args, map[string]bool{"artifact": true})); err != nil {
		return err
	}
	pos := fs.Args()
	if len(pos) != 2 {
		return fmt.Errorf("usage: opsctl install <app> <version> --artifact <path>")
	}
	if *artifact == "" {
		return fmt.Errorf("install: --artifact is required")
	}
	return opsctl.New(root).Install(ctx, pos[0], pos[1], *artifact)
}

func cmdRollback(ctx context.Context, root string, args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	pos := fs.Args()
	if len(pos) < 1 || len(pos) > 2 {
		return fmt.Errorf("usage: opsctl rollback <app> [version]")
	}
	target := ""
	if len(pos) == 2 {
		target = pos[1]
	}
	return opsctl.New(root).Rollback(ctx, pos[0], target)
}

func cmdPrune(ctx context.Context, root string, args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	keep := fs.Int("keep", opsctl.DefaultKeep, "number of recent releases to retain")
	if err := fs.Parse(reorderArgs(args, map[string]bool{"keep": true})); err != nil {
		return err
	}
	pos := fs.Args()
	if len(pos) != 1 {
		return fmt.Errorf("usage: opsctl prune <app> [--keep N]")
	}
	o := opsctl.New(root)
	o.Keep = *keep
	return o.Prune(ctx, pos[0])
}

func cmdInitBox(ctx context.Context, root string, args []string) error {
	fs := flag.NewFlagSet("init-box", flag.ContinueOnError)
	defaultApp := fs.String("default-app", "dashboard", "the apex/DEFAULT app name")
	domain := fs.String("domain", "", "apex domain, e.g. ai.metaspot.org (required)")
	port := fs.Int("port", 3000, "the apex app's loopback port")
	email := fs.String("email", "", "certbot email for HTTP-01 cert issuance")
	apexBlock := fs.String("apex-block", "", "path to the apex nginx server block source (required)")
	skipCert := fs.Bool("skip-cert", false, "do not issue a TLS cert (stage the block only)")
	if err := fs.Parse(reorderArgs(args, map[string]bool{
		"default-app": true, "domain": true, "port": true, "email": true, "apex-block": true,
	})); err != nil {
		return err
	}
	if *domain == "" {
		return fmt.Errorf("init-box: --domain is required")
	}
	block, err := opsctl.LoadApexBlockFile(*apexBlock)
	if err != nil {
		return err
	}
	return opsctl.New(root).InitBox(ctx, opsctl.InitBoxOptions{
		DefaultApp: *defaultApp,
		Domain:     *domain,
		Port:       *port,
		Email:      *email,
		ApexBlock:  block,
		SkipCert:   *skipCert,
	})
}

func cmdSetup(ctx context.Context, root string, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	port := fs.Int("port", 0, "the service's loopback port (substituted for __PORT__ in the fragment)")
	fragment := fs.String("fragment", "", "path to the service's nginx location fragment source (omit for a worker)")
	if err := fs.Parse(reorderArgs(args, map[string]bool{"port": true, "fragment": true})); err != nil {
		return err
	}
	pos := fs.Args()
	if len(pos) != 1 {
		return fmt.Errorf("usage: opsctl setup <app> [--port N] [--fragment <path>]")
	}
	frag, err := opsctl.LoadFragmentFile(*fragment)
	if err != nil {
		return err
	}
	return opsctl.New(root).Setup(ctx, opsctl.SetupOptions{
		App:      pos[0],
		Port:     *port,
		Fragment: frag,
	})
}
