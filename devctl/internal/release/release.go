// Package release installs and activates complete suite artifacts on a host.
package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

// ReleasesDir is the host directory holding suite releases.
const ReleasesDir = "/opt/ikigenba/releases"

// Folder returns the absolute host directory of a release.
func Folder(sha string) string { return ReleasesDir + "/" + sha }

// Opsctl returns the absolute path of a release's operator binary.
func Opsctl(sha string) string { return Folder(sha) + "/opsctl/bin/opsctl" }

// Label retains tag labels and omits hexadecimal commit operands.
func Label(rev string) string {
	if len(rev) >= 4 && len(rev) <= 40 {
		hex := true
		for i := range len(rev) {
			if (rev[i] < '0' || rev[i] > '9') && (rev[i] < 'a' || rev[i] > 'f') {
				hex = false
				break
			}
		}
		if hex {
			return ""
		}
	}
	return rev
}

// NotCommitError reports an operand that resolves to no local commit.
type NotCommitError struct{ Rev string }

func (e *NotCommitError) Error() string { return "'" + e.Rev + "' is not a commit" }

// ExitCode reports a preflight failure.
func (e *NotCommitError) ExitCode() int { return 2 }

// Resolve returns the local commit and the operand's release label.
func Resolve(ctx context.Context, c *checkout.Checkout, rev string) (string, string, error) {
	sha, ok, err := c.ResolveCommit(ctx, rev)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", &NotCommitError{Rev: rev}
	}
	return sha, Label(rev), nil
}

// Put copies and atomically unpacks a release artifact on a host.
func Put(ctx context.Context, h host.Host, stdout io.Writer, file, sha string) error {
	output, err := h.Run(ctx, "copy", "mktemp")
	if err != nil {
		return err
	}
	remote := strings.TrimRight(output.Stdout, "\r\n")
	if remote == "" {
		return fmt.Errorf("mktemp returned an empty path")
	}
	if err := h.Copy(ctx, "copy", file, remote); err != nil {
		_, _ = h.Run(context.WithoutCancel(ctx), "copy", "rm", "-f", remote)
		return err
	}
	space.Step(stdout, "copy", fmt.Sprintf("%s -> %s", filepath.Base(file), h.Address))
	return unpack(ctx, h, stdout, remote, sha)
}

func unpack(ctx context.Context, h host.Host, stdout io.Writer, remote, sha string) error {
	temp := ""
	cleanup := func(first error) error {
		if temp != "" {
			_, _ = h.Sudo(context.WithoutCancel(ctx), "unpack", "rm", "-rf", temp)
		}
		_, err := h.Run(context.WithoutCancel(ctx), "unpack", "rm", "-f", remote)
		if first != nil {
			return first
		}
		return err
	}
	if _, err := h.Sudo(ctx, "unpack", "mkdir", "-p", "-m", "0755", ReleasesDir); err != nil {
		return cleanup(err)
	}
	_, err := h.Sudo(ctx, "unpack", "test", "-e", Folder(sha))
	present := err == nil
	if err != nil {
		var commandErr *host.CommandError
		if !errors.As(err, &commandErr) || commandErr.Status != 1 {
			return cleanup(err)
		}
	}
	if !present {
		out, err := h.Sudo(ctx, "unpack", "mktemp", "-d", ReleasesDir+"/.unpack.XXXXXXXXXX")
		if err != nil {
			return cleanup(err)
		}
		candidate := strings.TrimRight(out.Stdout, "\r\n")
		suffix, ok := strings.CutPrefix(candidate, ReleasesDir+"/.unpack.")
		if !ok || suffix == "" || strings.Contains(suffix, "/") {
			return cleanup(fmt.Errorf("mktemp returned invalid directory %q", candidate))
		}
		temp = candidate
		if _, err := h.Sudo(ctx, "unpack", "tar", "-x", "-J", "--no-same-owner", "-f", remote, "-C", temp); err != nil {
			return cleanup(err)
		}
		if _, err := h.Sudo(ctx, "unpack", "mv", "-T", temp+"/"+sha, Folder(sha)); err != nil {
			return cleanup(err)
		}
		if _, err := h.Sudo(ctx, "unpack", "rmdir", temp); err != nil {
			return cleanup(err)
		}
		temp = ""
	}
	if err := cleanup(nil); err != nil {
		return err
	}
	detail := Folder(sha)
	if present {
		detail += " already present, kept"
	}
	space.Step(stdout, "unpack", detail)
	return nil
}

// Activate streams the release operator's activation output.
func Activate(ctx context.Context, h host.Host, stdout io.Writer, sha, label string) error {
	args := []string{Opsctl(sha), "activate", sha}
	if label != "" {
		args = append(args, label)
	}
	return h.StreamSudo(ctx, stdout, "activate", args...)
}
