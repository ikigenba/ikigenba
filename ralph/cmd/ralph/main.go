// Command ralph is the loopback-only domain service behind nginx. It trusts the
// X-Owner-Email / X-Client-Id headers nginx injects after a successful
// auth_request against the dashboard's authorization server, and performs no
// token logic of its own. See internal/server for the auth contract.
//
// It boots the full chassis (config, db + migrations, logging, server) and the
// ralph domain: the session store, per-session sandbox tree, async runner, and
// the 11-tool MCP surface. On boot, after migration, it runs the runner's
// crash-recovery sweep so runs left mid-flight by a previous process are marked
// failed and their sessions returned to idle before serving.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"ralph/internal/db"
	"ralph/internal/logging"
	"ralph/internal/mcp"
	"ralph/internal/runner"
	"ralph/internal/sandbox"
	"ralph/internal/server"
	"ralph/internal/session"
)

// version is the product version, overridden at build time via -ldflags.
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "ralph:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	portDef, err := envOrInt(getenv, "RALPH_PORT", 3004)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("ralph", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	// Bind 127.0.0.1 by default and in production: nginx is the only ingress
	// and sets identity headers authoritatively. Binding a public interface
	// would let anyone connect directly and spoof X-Owner-Email — a security
	// defect. The flag exists only so tests/local runs can override deliberately.
	ip := fs.String("ip", envOr(getenv, "RALPH_IP", "127.0.0.1"), "listen address — keep loopback (env: RALPH_IP)")
	port := fs.Int("port", portDef, "listen port (env: RALPH_PORT)")
	logLevel := fs.String("log-level", envOr(getenv, "RALPH_LOG_LEVEL", "info"), "log level: debug|info|warn|error (env: RALPH_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, version)
		return nil
	}

	// RALPH_RESOURCE_ID is this service's canonical resource identifier (must be
	// byte-equal to the `resource` in the PRM doc and the dashboard's token
	// binding). RALPH_AUTH_SERVER is the dashboard authorization-server base URL
	// advertised to clients. Both have local-dev defaults; we resolve them here
	// at the boundary so nothing deeper reads the environment.
	resourceID := envOr(getenv, "RALPH_RESOURCE_ID", "http://localhost:8080/srv/ralph/mcp")
	authServer := envOr(getenv, "RALPH_AUTH_SERVER", "http://localhost:8080")
	// RALPH_DB_PATH is the SQLite database file. db.Open pins SetMaxOpenConns(1)
	// for single-writer discipline; we resolve the path here at the boundary.
	dbPath := envOr(getenv, "RALPH_DB_PATH", "./tmp/ralph.db")
	// RALPH_RUN_TTL bounds each run's wall-clock — the runaway-goroutine
	// backstop (§5.3). Parsed as a Go duration (e.g. "30m", "2h").
	runTTL, err := envOrDuration(getenv, "RALPH_RUN_TTL", 30*time.Minute)
	if err != nil {
		return err
	}

	return serve(*ip, *port, *logLevel, resourceID, authServer, dbPath, runTTL, stdout)
}

// serve runs the long-running HTTP server until interrupted.
func serve(ip string, port int, logLevel, resourceID, authServer, dbPath string, runTTL time.Duration, stdout io.Writer) error {
	level, err := logging.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	logger := logging.New(level, stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Ensure the db's parent directory exists before opening — the SQLite
	// driver will not create it, and on a fresh box/dev tree it may not yet.
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create db dir %s: %w", dir, err)
		}
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.Migrate(ctx, conn); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Wire the session domain service: store + per-session sandbox tree + the
	// run-logs base dir + the async runner. The data tree lives alongside the
	// db file. The runner's TTL bounds each run's wall-clock.
	dataDir := filepath.Join(filepath.Dir(dbPath), "data")
	sb, err := sandbox.New(filepath.Join(dataDir, "sandboxes"))
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}
	runsDir := filepath.Join(dataDir, "runs")
	store := session.NewStore(conn)
	run := runner.New(store, sb, runTTL)
	svc := session.NewService(store, sb, runsDir, run)

	// Crash-recovery sweep (§5.3): runs left 'running' by a previous process
	// are orphaned — mark them failed and return their sessions to idle before
	// serving, so the single-flight gate starts from a clean slate.
	if swept, err := run.Recover(ctx); err != nil {
		return fmt.Errorf("crash-recovery sweep: %w", err)
	} else if swept > 0 {
		logger.Warn("crash-recovery: swept orphaned runs", "count", swept)
	}

	mcpHandler := mcp.NewHandler(svc)

	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	srv, err := server.New(server.Options{
		Addr:       addr,
		Logger:     logger,
		ResourceID: resourceID,
		AuthServer: authServer,
		MCP:        mcpHandler,
	})
	if err != nil {
		return err
	}

	logger.Info("starting ralph", "addr", addr, "resource_id", resourceID, "auth_server", authServer, "db_path", dbPath, "version", version)
	return server.Run(ctx, srv, logger)
}

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

// envOrInt returns def when key is unset/empty, the parsed value when it holds
// a valid integer, and an error naming the variable otherwise — a malformed
// override fails loudly rather than silently reverting to def.
func envOrInt(getenv func(string) string, key string, def int) (int, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, v)
	}
	return n, nil
}

// envOrDuration returns def when key is unset/empty, the parsed Go duration
// when it holds a valid value, and an error naming the variable otherwise — a
// malformed override fails loudly rather than silently reverting to def.
func envOrDuration(getenv func(string) string, key string, def time.Duration) (time.Duration, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}
