// Command consent is the one-time Gmail OAuth consent CLI (gmail-connector-plan.md
// P1 / decisions §2). It is run ONCE, by hand, during the BOOTSTRAP SITTING — as
// a `! go run ./cmd/consent` human-in-the-loop step — to mint the durable
// GMAIL_REFRESH_TOKEN.
//
// It performs the desktop-client loopback authorization-code flow against Google:
// read GMAIL_CLIENT_ID / GMAIL_CLIENT_SECRET from the environment, start a
// transient loopback HTTP server on 127.0.0.1:<ephemeral> (desktop OAuth clients
// get implicit loopback-redirect support — no redirect URI to register), build
// the auth URL with the full mail scope + offline access + forced consent, open
// the browser via xdg-open, capture the returned authorization code, exchange it
// at Google's token endpoint, and write the refresh token DIRECTLY to
// ~/.secrets/GMAIL_REFRESH_TOKEN with mode 0600.
//
// CRITICAL secrets discipline: this tool NEVER prints, logs, or otherwise emits
// the access token or the refresh token value. It prints only a masked
// confirmation (first 4 + last 4 chars of the refresh token), so it is safe to
// run under a `! <command>` whose stdout lands in an agent transcript — the
// secret never enters the agent's context. It also never echoes the client
// secret. Stdlib only; no third-party OAuth library.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	authEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenEndpoint = "https://oauth2.googleapis.com/token"
	mailScope     = "https://mail.google.com/"
)

func main() {
	if err := run(); err != nil {
		// Errors are safe to print: they never carry token values (we are careful
		// below not to embed any secret in an error). Exit non-zero so the operator
		// sees the failure clearly.
		fmt.Fprintln(os.Stderr, "consent: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	clientID := os.Getenv("GMAIL_CLIENT_ID")
	clientSecret := os.Getenv("GMAIL_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("GMAIL_CLIENT_ID and GMAIL_CLIENT_SECRET must be set in the environment (see the bootstrap sitting runbook)")
	}

	// Bind an ephemeral loopback port for the redirect target. The desktop OAuth
	// client accepts any 127.0.0.1 redirect implicitly, so we pick a free port at
	// runtime and tell Google about it via the redirect_uri.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("bind loopback listener: %w", err)
	}
	defer ln.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	// A random state value to bind the callback to this request (CSRF guard).
	state, err := randomState()
	if err != nil {
		return fmt.Errorf("generate state: %w", err)
	}

	authURL := buildAuthURL(clientID, redirectURI, state)

	// The callback delivers the authorization code (or an error) over this channel.
	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if gotState := q.Get("state"); gotState != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			resCh <- result{err: fmt.Errorf("state mismatch on callback")}
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, "authorization denied: "+e, http.StatusBadRequest)
			resCh <- result{err: fmt.Errorf("authorization denied: %s", e)}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			resCh <- result{err: fmt.Errorf("callback missing authorization code")}
			return
		}
		// The browser tab can close; the code never reaches the agent transcript.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "Authorization received. You can close this tab and return to the terminal.")
		resCh <- result{code: code}
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	fmt.Println("Opening your browser to authorize gmail (full mailbox scope).")
	fmt.Println("If it does not open automatically, visit:")
	fmt.Println("  " + authURL)
	openBrowser(authURL)

	// Wait for the callback (with a generous timeout for the manual click-through,
	// including the one-time "unverified app" warning).
	var code string
	select {
	case r := <-resCh:
		if r.err != nil {
			return r.err
		}
		code = r.code
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for authorization callback (5m)")
	}

	refreshToken, err := exchangeCode(clientID, clientSecret, code, redirectURI)
	if err != nil {
		return err
	}
	if refreshToken == "" {
		return fmt.Errorf("token response carried no refresh_token (ensure access_type=offline and prompt=consent; the app must be in production status)")
	}

	if err := writeRefreshToken(refreshToken); err != nil {
		return err
	}

	// Masked confirmation only — NEVER the token value.
	fmt.Printf("wrote GMAIL_REFRESH_TOKEN (%s)\n", mask(refreshToken))
	return nil
}

// buildAuthURL builds the Google authorization URL for the offline-access
// loopback flow: full mail scope, offline access (so a refresh token is issued),
// and forced consent (so the refresh token is re-issued even on re-consent).
func buildAuthURL(clientID, redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", mailScope)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	return authEndpoint + "?" + q.Encode()
}

// exchangeCode posts the authorization code to Google's token endpoint and
// returns ONLY the refresh token. The access token in the same response is
// intentionally discarded and never surfaced.
func exchangeCode(clientID, clientSecret, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", redirectURI)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		RefreshToken     string `json:"refresh_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode token response (status %d): %w", resp.StatusCode, err)
	}
	if body.Error != "" {
		// The error/description are Google's diagnostic strings, not secrets.
		return "", fmt.Errorf("token endpoint returned %s: %s", body.Error, body.ErrorDescription)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}
	return body.RefreshToken, nil
}

// writeRefreshToken writes the refresh token to ~/.secrets/GMAIL_REFRESH_TOKEN
// with mode 0600, creating ~/.secrets (0700) if needed. The file is written
// atomically-enough for a one-time manual step (truncate + write).
func writeRefreshToken(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	secretsDir := filepath.Join(home, ".secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", secretsDir, err)
	}
	path := filepath.Join(secretsDir, "GMAIL_REFRESH_TOKEN")
	// O_CREATE|O_TRUNC|O_WRONLY with 0600; if the file already exists with looser
	// perms, tighten it explicitly.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if _, err := f.WriteString(token); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// openBrowser best-effort opens the URL via xdg-open. A failure is non-fatal —
// the URL is also printed for manual use.
func openBrowser(rawURL string) {
	cmd := exec.Command("xdg-open", rawURL)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}

// randomState returns a random state token using crypto/rand. It is non-secret
// CSRF material; only its unpredictability matters.
func randomState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// mask returns a non-revealing fingerprint of a secret: first 4 + last 4 chars,
// or **** when it is too short to mask safely. Mirrors bin/push-secrets.
func mask(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}
