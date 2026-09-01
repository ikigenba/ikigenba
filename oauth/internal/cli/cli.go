// Package cli provides the in-process entry point for the OAuth CLI.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
	"github.com/ikigenba/ikigenba/oauth/internal/callback"
	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
	"github.com/ikigenba/ikigenba/oauth/internal/options"
)

type exitCode int

const (
	exitSuccess exitCode = 0
	exitFailure exitCode = 1
	exitUsage   exitCode = 2
)

// Deps carries the non-deterministic dependencies used by a login.
type Deps struct {
	Launcher   browser.Launcher
	Entropy    io.Reader
	HTTPClient *http.Client
	Listen     callback.ListenFunc
}

// Run executes the CLI and returns its process exit code without terminating
// the calling process.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int {
	return int(run(ctx, args, stdout, stderr, deps))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	validated, code, proceed := prepare(args, stdout, stderr)
	if !proceed {
		return code
	}

	server, err := callback.Listen(deps.Listen, validated.Port)
	if err != nil {
		return reportFailure(stderr, err)
	}
	defer func() { _ = server.Close() }()

	client, session, code, proceed := authorize(validated, server, stderr, deps)
	if !proceed {
		return code
	}
	result, code, proceed := waitForCallback(ctx, validated, server, session, stderr)
	if !proceed {
		return code
	}

	return exchange(ctx, validated, client, session, result, stdout, stderr, deps.HTTPClient)
}

func prepare(args []string, stdout, stderr io.Writer) (options.Options, exitCode, bool) {
	flags, err := options.ParseFlags(args)
	if errors.Is(err, options.ErrHelp) {
		_, _ = io.WriteString(stdout, options.Usage())

		return options.Options{}, exitSuccess, false
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n%s", strconv.Quote(err.Error()), options.Usage())

		return options.Options{}, exitUsage, false
	}
	if flags.Version {
		_, _ = io.WriteString(stdout, version+"\n")

		return options.Options{}, exitSuccess, false
	}
	validated, err := flags.Validate()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n%s", strconv.Quote(err.Error()), options.Usage())

		return options.Options{}, exitUsage, false
	}

	return validated, exitSuccess, true
}

func authorize(
	validated options.Options,
	server *callback.Server,
	stderr io.Writer,
	deps Deps,
) (oauth.Client, oauth.Session, exitCode, bool) {
	redirectURI := "http://" +
		net.JoinHostPort(validated.CallbackHost, strconv.Itoa(server.Port())) +
		validated.CallbackPath
	client := oauth.Client{
		AuthURL:      validated.AuthURL,
		TokenURL:     validated.TokenURL,
		ClientID:     validated.ClientID,
		ClientSecret: validated.ClientSecret,
		RedirectURI:  redirectURI,
		Scope:        validated.Scope,
	}
	session, err := oauth.NewSession(deps.Entropy)
	if err != nil {
		return oauth.Client{}, oauth.Session{}, reportFailure(stderr, err), false
	}
	authorizeURL := client.AuthorizeURL(session, validated.AuthParams)
	_, _ = fmt.Fprintln(stderr, authorizeURL)
	if !validated.NoBrowser {
		if err := deps.Launcher.Open(authorizeURL); err != nil {
			return oauth.Client{}, oauth.Session{}, reportFailure(stderr, err), false
		}
	}

	return client, session, exitSuccess, true
}

func waitForCallback(
	ctx context.Context,
	validated options.Options,
	server *callback.Server,
	session oauth.Session,
	stderr io.Writer,
) (callback.Result, exitCode, bool) {
	waitCtx, cancelWait := context.WithTimeout(ctx, validated.Timeout)
	defer cancelWait()
	result, err := server.Wait(waitCtx, validated.CallbackPath, session.State)
	if err != nil {
		return callback.Result{}, reportFailure(stderr, err), false
	}

	return result, exitSuccess, true
}

func exchange(
	ctx context.Context,
	validated options.Options,
	client oauth.Client,
	session oauth.Session,
	result callback.Result,
	stdout, stderr io.Writer,
	httpClient *http.Client,
) exitCode {
	tokens, err := client.Exchange(
		ctx,
		httpClient,
		session,
		result.Code,
		validated.TokenParams,
		validated.TokenHeaders,
	)
	if err != nil {
		return reportFailure(stderr, err)
	}
	// Phase 8 owns observable stdout-write failure handling.
	// llm-lint:ignore discarded-output-write-error
	_, _ = stdout.Write(tokens)

	return exitSuccess
}

func reportFailure(stderr io.Writer, err error) exitCode {
	_, _ = fmt.Fprintf(stderr, "error: %s\n", strconv.Quote(err.Error()))

	return exitFailure
}
