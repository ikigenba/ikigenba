// Command dropbox is the loopback-only mirror daemon + event-plane producer
// behind nginx. It trusts the stable X-Owner-Id key and X-Client-Id headers
// nginx injects after a successful auth_request against the dashboard's
// authorization server; X-Owner-Email / X-Owner-Name / X-Owner-Picture ride
// along as display values. It performs no token logic of its own. See
// appkit/server for the auth contract.
//
// The uniform chassis — the fixed subcommands (serve/version/manifest/migrate/
// schema), config-from-env, the migration runner + downgrade guard, the
// loopback HTTP server + PRM + identity gate, and the /feed producer mount — is
// owned by appkit. main.go declares only dropbox's identity (the Spec) and wires
// its domain surface (the bare MCP tools, the private /content route, the
// file.* producer, and the background sync engine) through the Spec hooks.
// RESOURCE_ID / AUTH_SERVER are composed in-binary by appkit/config from
// IKIGENBA_DOMAIN + MOUNT (was the deleted bin/build run-wrapper's job).
//
// dropbox differs from the passive crm/ledger producers: it carries a background
// sync engine (the longpoll → continue → apply loop, "the heart"). That loop runs
// for the whole serve lifecycle, so it is wired through appkit's Workers seam —
// appkit launches it on the serve context, a SIGTERM cancels it alongside the
// server, and a structural fault returning from it brings the server down too
// (event-protocol.md decision 11). The static Dropbox OAuth credentials are
// read here from the environment, while the rotating refresh token is read from
// state/. None are logged (§2.8), and appkit never touches them.
package main

import (
	"appkit"
	"appkit/config"
	"context"
	"dropbox/internal/db"
	"dropbox/internal/dropbox"
	"dropbox/internal/mcp"
	"eventplane/outbox"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"registry"
	"strconv"
	"strings"
	"time"
)

func main() {
	runtime := &dropboxRuntime{}
	appkit.Main(appkit.Spec{
		App:        "dropbox",
		Mount:      "/srv/dropbox/",
		Port:       registry.MustPort("dropbox"),
		MCP:        true,
		WWW:        true,
		Feed:       "/feed",
		Migrations: db.FS,
		Events:     dropbox.Events,
		ManifestExtras: []appkit.ManifestKV{
			{Key: "OUTBOX_RETENTION_DAYS", Value: "7"},
			{Key: "OUTBOX_RETENTION_MAX_ROWS", Value: "1000000"},
			{Key: "DROPBOX_LONGPOLL_TIMEOUT", Value: "480"},
			{Key: "DROPBOX_MAX_ENTRY_RETRIES", Value: "5"},
		},
		Health:   runtime.health,
		Handlers: runtime.handlers,
		Producer: runtime.producer,
		Workers:  []func(context.Context) error{runtime.runEngine, runtime.runUploader},
	})
}

type dropboxRuntime struct {
	svc         *dropbox.Service
	engine      *dropbox.Engine
	contentBase string
	rt          *appkit.Router
}

func (d *dropboxRuntime) health(context.Context) (map[string]any, error) {
	if d.svc == nil {
		return nil, fmt.Errorf("dropbox: Health reporter ran before Handlers built the Service")
	}
	info, err := d.svc.Health("", "")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"mirror_bytes":               info.MirrorBytes,
		"disk_free_bytes":            info.DiskFreeBytes,
		"disk_total_bytes":           info.DiskTotalBytes,
		"failed_files":               info.FailedFiles,
		"pending_uploads":            info.PendingUploads,
		"failed_uploads":             info.FailedUploads,
		"oldest_pending_age_seconds": info.OldestPendingAgeSeconds,
	}, nil
}

func (d *dropboxRuntime) handlers(r *appkit.Router) error {
	d.rt = r
	conn := r.DB()
	if conn == nil {
		return fmt.Errorf("dropbox: no DB handle on router")
	}
	port, err := config.EnvOrInt(os.Getenv, "DROPBOX_PORT", registry.MustPort("dropbox"))
	if err != nil {
		return err
	}
	d.contentBase = "http://" + config.EnvOr(os.Getenv, "DROPBOX_IP", "127.0.0.1") + ":" + strconv.Itoa(port)
	mirrorPath, err := resolveMirrorPath(os.Getenv)
	if err != nil {
		return err
	}
	mirror, err := dropbox.NewMirror(mirrorPath)
	if err != nil {
		return fmt.Errorf("mirror: %w", err)
	}
	refreshToken, refreshTokenPath, err := readRefreshToken(os.Getenv)
	if err != nil {
		return err
	}
	cfg := dropbox.Config{
		AppKey:           os.Getenv("DROPBOX_APP_KEY"),
		AppSecret:        os.Getenv("DROPBOX_APP_SECRET"),
		RefreshToken:     refreshToken,
		RefreshTokenPath: refreshTokenPath,
		AppFolderRoot:    os.Getenv("DROPBOX_APP_FOLDER_ROOT"),
	}
	cfg.LongpollTimeoutSeconds, err = config.EnvOrInt(os.Getenv, "DROPBOX_LONGPOLL_TIMEOUT", 480)
	if err != nil {
		return err
	}
	maxEntryRetries, err := config.EnvOrInt(os.Getenv, "DROPBOX_MAX_ENTRY_RETRIES", 5)
	if err != nil {
		return err
	}
	rpcClient, longpollClient, sourceClient := outboundClients(r)
	client := dropbox.NewClient(cfg, rpcClient, longpollClient)
	d.svc = dropbox.NewService(conn)
	d.svc.Mirror = mirror
	d.svc.Client = client
	d.svc.ContentBase = d.contentBase
	d.svc.Logger = r.Logger()
	startRoot := func(ctx context.Context, origin string) context.Context {
		rootCtx, _ := r.Recorder().StartRoot(ctx, origin, nil)
		return rootCtx
	}
	d.svc.StartRoot = startRoot
	d.engine = dropbox.NewEngine(d.svc, dropbox.EngineOptions{
		Client: client, Logger: r.Logger(), MaxEntryRetries: maxEntryRetries, StartRoot: startRoot,
	})
	return d.mountHandlers(sourceClient)
}

func (d *dropboxRuntime) mountHandlers(sourceClient *http.Client) error {
	rt := d.rt
	rt.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := rt.WWW().Render(w, "landing.html", struct {
			Service string
			Version string
		}{
			Service: rt.Service(), Version: rt.Version(),
		}); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	}))
	allowed := registrySourcePorts()
	handler, err := mcp.NewHandler(d.svc, func(port int) bool { return allowed[port] }, sourceClient, rt)
	if err != nil {
		return err
	}
	rt.Handle("POST /mcp", rt.RequireIdentity(handler))
	mountLoopbackRoutes(rt, d.svc)
	return nil
}

func (d *dropboxRuntime) producer(ob *outbox.Outbox) error {
	if d.svc == nil {
		return fmt.Errorf("dropbox: Producer called before Handlers built the Service")
	}
	d.svc.Outbox = dropbox.NewOutboxProducer(ob, d.contentBase)
	return nil
}

func (d *dropboxRuntime) runEngine(ctx context.Context) error {
	if d.engine == nil {
		return fmt.Errorf("dropbox: Workers ran before Handlers built the engine")
	}
	return d.engine.Run(ctx)
}

func (d *dropboxRuntime) runUploader(ctx context.Context) error {
	if d.svc == nil {
		return fmt.Errorf("dropbox: Workers ran before Handlers built the Service")
	}
	return d.svc.RunUploader(ctx)
}

type outboundClientFactory interface {
	HTTPClient(time.Duration) *http.Client
}

func outboundClients(rt outboundClientFactory) (rpc, longpoll, source *http.Client) {
	rpc = rt.HTTPClient(100 * time.Second)
	longpoll = rt.HTTPClient(dropbox.LongpollClientTimeout())
	source = rt.HTTPClient(5 * time.Second)
	source.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return rpc, longpoll, source
}

func mountLoopbackRoutes(rt *appkit.Router, svc *dropbox.Service) {
	rt.HandleLoopback("GET /content", svc.ContentHandler())
	rt.HandleLoopback("PUT /content", svc.WriteHandler())
	rt.HandleLoopback("DELETE /content", svc.WriteHandler())
	rt.HandleLoopback("POST /mkdir", svc.MkdirHandler())
	rt.HandleLoopback("POST /move", svc.MoveHandler())
	rt.HandleLoopback("GET /list", svc.ListHandler())
	rt.HandleLoopback("GET /stat", svc.StatHandler())
}

// registrySourcePorts is the composition-root confinement seam for MCP source
// references: only ports owned by registered suite services are fetchable.
func registrySourcePorts() map[int]bool {
	allowed := map[int]bool{}
	for _, service := range registry.Services {
		allowed[service.Port] = true
	}
	return allowed
}

func resolveMirrorPath(getenv func(string) string) (string, error) {
	cfg, err := config.Resolve("dropbox", "/srv/dropbox/", registry.MustPort("dropbox"), getenv)
	if err != nil {
		return "", fmt.Errorf("resolve dropbox config: %w", err)
	}
	return defaultMirrorPath(getenv, cfg.DBPath), nil
}

func readRefreshToken(getenv func(string) string) (token, path string, err error) {
	root := strings.TrimSpace(getenv("IKIGENBA_ROOT"))
	if root == "" {
		return "", "", fmt.Errorf("dropbox: cannot read DROPBOX_REFRESH_TOKEN: IKIGENBA_ROOT is empty")
	}
	path = filepath.Join(root, "dropbox", "state", "DROPBOX_REFRESH_TOKEN")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", path, fmt.Errorf("dropbox: read DROPBOX_REFRESH_TOKEN from %s: %w", path, err)
	}
	return string(b), path, nil
}

func defaultMirrorPath(getenv func(string) string, dbPath string) string {
	if mirrorPath := getenv("DROPBOX_MIRROR_PATH"); mirrorPath != "" {
		return mirrorPath
	}
	return filepath.Join(filepath.Dir(dbPath), "mirror")
}
