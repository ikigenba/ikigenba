// Command wiki is the loopback-only domain service behind nginx. It trusts the
// X-Owner-Email / X-Client-Id headers nginx injects after a successful
// auth_request against the dashboard's authorization server, and performs no
// token logic of its own. See internal/server for the auth contract.
//
// This is the scaffold wiki service (Task 2.1): it boots the chassis (config,
// db + migrations, logging, server) and exposes a single MCP tool, wiki_whoami,
// the end-to-end auth proof. The database connection is opened and migrated but
// not yet read by any tool — it is the wired seam where real wiki domain logic
// (the agentic ingest core, the search store) attaches in later phases. wiki is
// NOT an event-plane producer in Phase 1, so there is no outbox / /feed wiring.
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
	"strconv"
	"syscall"

	"wiki/internal/db"
	"wiki/internal/logging"
	"wiki/internal/mcp"
	"wiki/internal/server"
)

// version is the product version, overridden at build time via -ldflags.
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "wiki:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	portDef, err := envOrInt(getenv, "WIKI_PORT", 3006)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("wiki", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	// Bind 127.0.0.1 by default and in production: nginx is the only ingress
	// and sets identity headers authoritatively. Binding a public interface
	// would let anyone connect directly and spoof X-Owner-Email — a security
	// defect. The flag exists only so tests/local runs can override deliberately.
	ip := fs.String("ip", envOr(getenv, "WIKI_IP", "127.0.0.1"), "listen address — keep loopback (env: WIKI_IP)")
	port := fs.Int("port", portDef, "listen port (env: WIKI_PORT)")
	logLevel := fs.String("log-level", envOr(getenv, "WIKI_LOG_LEVEL", "info"), "log level: debug|info|warn|error (env: WIKI_LOG_LEVEL)")
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

	// WIKI_RESOURCE_ID is this service's canonical resource identifier (must be
	// byte-equal to the `resource` in the PRM doc and the dashboard's token
	// binding). WIKI_AUTH_SERVER is the dashboard authorization-server base URL
	// advertised to clients. Both have local-dev defaults; we resolve them here
	// at the boundary so nothing deeper reads the environment.
	resourceID := envOr(getenv, "WIKI_RESOURCE_ID", "http://localhost:8080/srv/wiki/mcp")
	authServer := envOr(getenv, "WIKI_AUTH_SERVER", "http://localhost:8080")
	// WIKI_DB_PATH is the SQLite database file. db.Open pins SetMaxOpenConns(1)
	// for single-writer discipline; we resolve the path here at the boundary.
	dbPath := envOr(getenv, "WIKI_DB_PATH", "./tmp/wiki.db")

	return serve(*ip, *port, *logLevel, resourceID, authServer, dbPath, stdout)
}

// serve runs the long-running HTTP server until interrupted.
func serve(ip string, port int, logLevel, resourceID, authServer, dbPath string, stdout io.Writer) error {
	level, err := logging.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	logger := logging.New(level, stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.Migrate(ctx, conn); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	mcpHandler := mcp.NewHandler()

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

	logger.Info("starting wiki", "addr", addr, "resource_id", resourceID, "auth_server", authServer, "db_path", dbPath, "version", version)
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
