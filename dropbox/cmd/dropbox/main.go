// Command dropbox is the loopback-only mirror daemon + event-plane producer
// behind nginx. It trusts the X-Owner-Email / X-Client-Id headers nginx injects
// after a successful auth_request against the dashboard's authorization server,
// and performs no token logic of its own. See internal/server for the auth
// contract.
//
// This is the Phase 0 scaffold (PLAN.md §10): it boots the full chassis (config,
// db + migrations, logging, outbox producer, server) and exposes the two MCP
// tools dropbox_whoami / dropbox_health. The Dropbox sync engine, the content
// endpoint, and event emission are wired in later phases — this is the chassis
// seam they attach to, the same way ledger wired internal/ledger.
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

	"dropbox/internal/db"
	"dropbox/internal/dropbox"
	"dropbox/internal/logging"
	"dropbox/internal/mcp"
	"dropbox/internal/server"

	"eventplane/outbox"
)

// version is the product version, overridden at build time via -ldflags.
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "dropbox:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	portDef, err := envOrInt(getenv, "DROPBOX_PORT", 3005)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("dropbox", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	// Bind 127.0.0.1 by default and in production: nginx is the only ingress
	// and sets identity headers authoritatively. Binding a public interface
	// would let anyone connect directly and spoof X-Owner-Email — a security
	// defect. The flag exists only so tests/local runs can override deliberately.
	ip := fs.String("ip", envOr(getenv, "DROPBOX_IP", "127.0.0.1"), "listen address — keep loopback (env: DROPBOX_IP)")
	port := fs.Int("port", portDef, "listen port (env: DROPBOX_PORT)")
	logLevel := fs.String("log-level", envOr(getenv, "DROPBOX_LOG_LEVEL", "info"), "log level: debug|info|warn|error (env: DROPBOX_LOG_LEVEL)")
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

	// DROPBOX_RESOURCE_ID is this service's canonical resource identifier (must be
	// byte-equal to the `resource` in the PRM doc and the dashboard's token
	// binding). DROPBOX_AUTH_SERVER is the dashboard authorization-server base URL
	// advertised to clients. Both have local-dev defaults; we resolve them here
	// at the boundary so nothing deeper reads the environment.
	resourceID := envOr(getenv, "DROPBOX_RESOURCE_ID", "http://localhost:8080/srv/dropbox/mcp")
	authServer := envOr(getenv, "DROPBOX_AUTH_SERVER", "http://localhost:8080")
	// DROPBOX_DB_PATH is the SQLite database file. db.Open pins SetMaxOpenConns(1)
	// for single-writer discipline; we resolve the path here at the boundary.
	dbPath := envOr(getenv, "DROPBOX_DB_PATH", "./tmp/dropbox.db")
	// DROPBOX_GENERATION_PATH is the event-plane generation/epoch token sidecar
	// (event-protocol.md §9.3). It MUST live outside the DB file so a file-level
	// restore does not roll it back; default is the DB path plus ".generation".
	genPath := envOr(getenv, "DROPBOX_GENERATION_PATH", dbPath+".generation")
	// Event-plane retention knobs (§11.3). Zero means "use the library default"
	// (7 days / 1,000,000 rows). These are SHARED event-plane knobs and keep their
	// OUTBOX_ prefix — they are not DROPBOX_* and must not be renamed.
	retentionDays, err := envOrInt(getenv, "OUTBOX_RETENTION_DAYS", 0)
	if err != nil {
		return err
	}
	retentionMaxRows, err := envOrInt(getenv, "OUTBOX_RETENTION_MAX_ROWS", 0)
	if err != nil {
		return err
	}

	// Dropbox sync config (PLAN.md §2/§9). The three secrets arrive via
	// .envrc/direnv in dev or the launcher on the box; they are read here at the
	// boundary and passed into the client — never logged.
	cfg := dropbox.Config{
		AppKey:        getenv("DROPBOX_APP_KEY"),
		AppSecret:     getenv("DROPBOX_APP_SECRET"),
		RefreshToken:  getenv("DROPBOX_REFRESH_TOKEN"),
		AppFolderRoot: getenv("DROPBOX_APP_FOLDER_ROOT"),
	}
	cfg.LongpollTimeoutSeconds, err = envOrInt(getenv, "DROPBOX_LONGPOLL_TIMEOUT", 480)
	if err != nil {
		return err
	}
	mirrorPath := envOr(getenv, "DROPBOX_MIRROR_PATH", "./tmp/mirror")
	maxEntryRetries, err := envOrInt(getenv, "DROPBOX_MAX_ENTRY_RETRIES", 5)
	if err != nil {
		return err
	}

	return serve(serveConfig{
		ip: *ip, port: *port, logLevel: *logLevel,
		resourceID: resourceID, authServer: authServer,
		dbPath: dbPath, genPath: genPath,
		retentionDays: retentionDays, retentionMaxRows: retentionMaxRows,
		dropboxCfg: cfg, mirrorPath: mirrorPath, maxEntryRetries: maxEntryRetries,
		stdout: stdout,
	})
}

// serveConfig bundles the resolved boundary config for serve.
type serveConfig struct {
	ip                              string
	port                            int
	logLevel                        string
	resourceID, authServer          string
	dbPath, genPath                 string
	retentionDays, retentionMaxRows int
	dropboxCfg                      dropbox.Config
	mirrorPath                      string
	maxEntryRetries                 int
	stdout                          io.Writer
}

// serve runs the long-running HTTP server until interrupted.
func serve(c serveConfig) error {
	ip, port, logLevel := c.ip, c.port, c.logLevel
	resourceID, authServer := c.resourceID, c.authServer
	dbPath, genPath := c.dbPath, c.genPath
	retentionDays, retentionMaxRows := c.retentionDays, c.retentionMaxRows
	stdout := c.stdout

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

	// Event-plane producer (event-protocol.md). New runs the §5.3 startup probe
	// (it crashes us here if a second concurrent write tx is not refused) and
	// loads/mints the generation token. The outbox shares dropbox's single-writer
	// SQLite handle so a file lifecycle event commits atomically with the index
	// write once the sync engine lands (Phase 4).
	ob, err := outbox.New(conn, outbox.Options{
		Source:           "dropbox",
		DBPath:           dbPath,
		GenerationPath:   genPath,
		Logger:           logger,
		RetentionDays:    retentionDays,
		RetentionMaxRows: int64(retentionMaxRows),
	})
	if err != nil {
		return fmt.Errorf("event plane: %w", err)
	}
	go ob.StartRetention(ctx)

	addr := net.JoinHostPort(ip, strconv.Itoa(port))

	// The loopback /content base URL stamped into event content_url values
	// (PLAN.md §5). It is the service's own loopback address; consumers fetch
	// bytes there. The route itself lands in Phase 5 — the Service.Content
	// resolution logic and the URL builder exist now.
	contentBase := "http://" + addr

	// Wire the Dropbox sync subsystem (PLAN.md §2/§8): the mirror, the API client,
	// the outbox producer (event-plane), and the Service that owns the atomic
	// {index change + outbox event} tx. The engine goroutine runs the longpoll →
	// continue → apply loop under the server's lifecycle context.
	mirror, err := dropbox.NewMirror(c.mirrorPath)
	if err != nil {
		return fmt.Errorf("mirror: %w", err)
	}
	client := dropbox.NewClient(c.dropboxCfg, nil)

	dropboxSvc := dropbox.NewService(conn)
	dropboxSvc.Mirror = mirror
	dropboxSvc.Client = client
	dropboxSvc.Outbox = dropbox.NewOutboxProducer(ob, contentBase)

	engine := dropbox.NewEngine(dropboxSvc, dropbox.EngineOptions{
		Client:          client,
		Logger:          logger,
		MaxEntryRetries: c.maxEntryRetries,
	})
	go func() {
		if err := engine.Run(ctx); err != nil {
			logger.Error("dropbox sync engine exited", "err", err.Error())
		}
	}()

	mcpHandler := mcp.NewHandler(dropboxSvc)
	srv, err := server.New(server.Options{
		Addr:       addr,
		Logger:     logger,
		ResourceID: resourceID,
		AuthServer: authServer,
		MCP:        mcpHandler,
		Feed:       ob.FeedHandler(),
		Content:    dropboxSvc.ContentHandler(),
	})
	if err != nil {
		return err
	}

	logger.Info("starting dropbox", "addr", addr, "resource_id", resourceID, "auth_server", authServer, "db_path", dbPath, "generation", ob.Generation(), "version", version)
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
