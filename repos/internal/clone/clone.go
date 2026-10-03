// Package clone builds clone addresses and shared credential guidance.
package clone

import (
	"context"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Base reads the published repos URL afresh, falling back to the request origin.
func Base(r *http.Request, servicesPath string) string {
	if list, err := services.Read(servicesPath); err == nil {
		if entry, ok := list.Find("repos"); ok && entry.URL != "" {
			return strings.TrimSuffix(entry.URL, "/")
		}
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

type baseKey struct{}

// NewContext attaches a clone base to a context.
func NewContext(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, baseKey{}, base)
}

// FromContext returns the most recently attached clone base.
func FromContext(ctx context.Context) (string, bool) {
	b, ok := ctx.Value(baseKey{}).(string)
	return b, ok
}

// URL appends the repository name and git suffix without altering its inputs.
func URL(base, name string) string { return base + "/" + name + ".git" }

// Credentials holds shared authentication guidance for git users.
type Credentials struct{ Intro, Helper, Warning string }

// Guidance builds the credential helper scoped to the base's space.
func Guidance(base string) Credentials {
	scheme, host, found := strings.Cut(base, "://")
	if !found {
		scheme, host = "https", base
	}
	host, _, _ = strings.Cut(host, "/")
	if strings.HasPrefix(host, "repos.") && len(host) > 6 {
		host = host[6:]
	}
	return Credentials{
		Intro:   "Git authenticates with your personal access token as the password; the username is ignored. Keep the token in the environment variable IKIGENBA_TOKEN and give it to git with this credential helper, which reads the variable whenever git asks:",
		Helper:  "git config --global credential." + scheme + "://*." + host + ".helper '!f() { test \"$1\" = get && printf \"username=token\\npassword=%s\\n\" \"$IKIGENBA_TOKEN\"; }; f'",
		Warning: "Or set GIT_ASKPASS to a program that prints $IKIGENBA_TOKEN. Never put the token in a remote's URL or on a command line, and never use credential.helper store: each writes it to disk in plain text.",
	}
}

// Text joins the guidance fields with blank lines.
func (c Credentials) Text() string { return c.Intro + "\n\n" + c.Helper + "\n\n" + c.Warning }
