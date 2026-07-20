// Command prompts is the loopback-only domain service behind nginx. It trusts the
// X-Owner-Email / X-Client-Id headers nginx injects after a successful
// auth_request against the dashboard's authorization server, and performs no
// token logic of its own; nginx is the trust boundary.
//
// The uniform chassis — the fixed subcommands (serve/version/manifest/migrate/
// schema), config-from-env, the migration runner + downgrade guard, the
// loopback HTTP server + PRM + identity gate (health/reflection), and graceful
// shutdown — is owned by appkit. main.go declares only prompts' identity (the
// Spec) and wires its domain surface through the Handlers hook: the prompt
// store, per-prompt sandbox tree, async runner, the boot-time crash-recovery
// sweep, the appkit/mcp tool table (internal/mcp declares the sixteen domain
// tools; the chassis serves the transport plus health/reflection), and the
// share/www landing surface through Spec.WWW and rt.WWW() (nginx-gated at the
// edge, ungated in-process at the mount root). RESOURCE_ID / AUTH_SERVER are
// composed in-binary by appkit/config from IKIGENBA_DOMAIN + MOUNT.
//
// prompts is an LLM service: it uses agentkit (the LLM engine + tool surface) for
// the agent loop, kept strictly separate from appkit (the deploy/serve chassis).
// It is also an event-plane producer (Feed: "/feed", emitting run.succeeded and
// run.failed via the outbox on the run's terminal write) and a consumer of six
// upstreams (cron, crm, ledger, dropbox, scripts, and its own feed for
// self-chaining) declared as Spec.Consumers and run by the chassis. Its only
// secret, ANTHROPIC_API_KEY, is read env-only inside the runner/prompt domain at
// the point of use and never logged (§2.8).
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"appkit"
	"appkit/config"

	"eventplane/consumer"
	"eventplane/outbox"

	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/consume"
	"prompts/internal/db"
	"prompts/internal/inference"
	"prompts/internal/mcp"
	"prompts/internal/prompt"
	"prompts/internal/provider"
	"prompts/internal/runner"
	"prompts/internal/sandbox"

	"registry"
)

// consumerID is the stable id prompts presents on every consumer connect
// (event-protocol.md §7.1) — the literal "prompts" across all its upstream loops.
const consumerID = "prompts"

// sources is the resolved upstream producers prompts consumes (A11).
// CONSUMES=cron,crm,ledger,dropbox,scripts,prompts in etc/manifest.env mirrors
// this for the registry. The final "prompts" entry is self-chaining (A12): a
// consumer loop pointed at prompts' OWN /feed (PROMPTS_PROMPTS_FEED_URL defaults
// through the shared registry) so a prompt can fire on another prompt's
// run.succeeded/run.failed.
var sources = []string{"cron", "crm", "ledger", "dropbox", "scripts", "prompts"}

// svcRef carries the prompt service from the Handlers hook (where appkit has
// opened + migrated the DB and built the domain) to the consumer handlers, which
// are built strictly afterward by appkit's Consumers table.
// storeRef is the same hand-off for the producer outbox: the Producer hook
// (which runs AFTER Handlers, once appkit has constructed the outbox) injects it
// onto the store so the runner's terminal write emits the outcome event on the
// same tx (event-triggering decisions §3).
var (
	svcRef             *prompt.Service
	storeRef           *prompt.Store
	callsRef           *calls.Store
	callsRetentionDays int
)

func main() {
	appkit.Main(promptsSpec())
}

func promptsSpec() appkit.Spec {
	consumers := make([]appkit.Consumer, 0, len(sources))
	for _, src := range sources {
		src := src
		consumers = append(consumers, appkit.Consumer{
			Source:        src,
			Subscriptions: consume.Subscriptions([]string{src}),
			Handler: func(rt *appkit.Router) consumer.Handler {
				logger := rt.Logger()
				fire := func(ctx context.Context, promptID, s, kind, subject, eventID string, payload []byte) error {
					_, err := svcRef.RunByEvent(ctx, promptID, s, kind, subject, eventID, payload)
					return err
				}
				return consume.Handler(fire, svcRef.PromptsForEvent, src, logger)
			},
		})
	}

	return appkit.Spec{
		App:       "prompts",
		Mount:     "/srv/prompts/",
		Port:      registry.MustPort("prompts"),
		MCP:       true,
		WWW:       true,
		Consumers: consumers,
		// prompts is ALSO an event-plane PRODUCER of two STATIC outcome types:
		// run.succeeded / run.failed, emitted in the SAME tx as a run's
		// terminal-state write. Feed mounts the /feed producer; Events is the static
		// registry (NOT a dynamic Publishes provider — the outcome types are fixed at
		// build time); Consumers emits CONSUMES; Producer injects the outbox onto the
		// store; ManifestExtras round-trips the retention config like every other
		// producer.
		Feed:   "/feed",
		Events: prompt.Events,
		ManifestExtras: []appkit.ManifestKV{
			{Key: "OUTBOX_RETENTION_DAYS", Value: "7"},
			{Key: "OUTBOX_RETENTION_MAX_ROWS", Value: "1000000"},
			{Key: "PROMPTS_CALLS_BODY_RETENTION_DAYS", Value: "30"},
			{Key: "PROMPTS_MAX_INFLIGHT_CALLS", Value: "8"},
			{Key: "PROMPTS_MAX_CONCURRENT_RUNS", Value: "8"},
		},
		Workers: []func(context.Context) error{callsBodyRetentionWorker},
		Producer: func(ob *outbox.Outbox) error {
			if storeRef == nil {
				return fmt.Errorf("prompts: Producer called before Handlers built the Store")
			}
			storeRef.Outbox = ob
			return nil
		},
		Migrations: db.FS,
		// Handlers builds prompts' domain over appkit's shared single-writer DB
		// handle, runs the boot-time crash-recovery sweep (after migrate, before
		// serving), captures the service for the consumer handlers, and mounts the
		// prompts_* MCP surface gated behind nginx-injected identity.
		Handlers: func(r *appkit.Router) error {
			return registerRoutes(r)
		},
	}
}

// registerRoutes wires prompts' domain on appkit's server. It is the seam where
// the chassis (appkit) hands off to the domain: appkit has already resolved
// config, opened, and migrated the shared single-writer DB before calling this.
func registerRoutes(rt *appkit.Router) error {
	site := rt.WWW()
	if site == nil {
		return fmt.Errorf("prompts: no WWW site on router")
	}

	conn := rt.DB()
	if conn == nil {
		return fmt.Errorf("prompts: no DB handle on router")
	}
	retentionDays, err := callsBodyRetentionConfig(os.Getenv)
	if err != nil {
		return err
	}
	callStore := calls.NewStore(conn)
	if swept, err := callStore.PruneBodies(context.Background(), callsBodyCutoff(time.Now(), retentionDays)); err != nil {
		return fmt.Errorf("prompts: calls body-retention boot sweep: %w", err)
	} else if swept > 0 {
		rt.Logger().Info("calls: pruned retained bodies", "count", swept)
	}
	callsRef = callStore
	callsRetentionDays = retentionDays
	callCap, err := config.EnvOrInt(os.Getenv, "PROMPTS_MAX_INFLIGHT_CALLS", 8)
	if err != nil || callCap < 1 {
		return fmt.Errorf("PROMPTS_MAX_INFLIGHT_CALLS must be a positive integer")
	}
	runCap, err := config.EnvOrInt(os.Getenv, "PROMPTS_MAX_CONCURRENT_RUNS", 8)
	if err != nil || runCap < 1 {
		return fmt.Errorf("PROMPTS_MAX_CONCURRENT_RUNS must be a positive integer")
	}
	gate := admit.New(callCap, runCap)
	completion := inference.NewExecutor(callStore, gate, provider.Build, os.Getenv)
	embedding := inference.NewEmbedExecutor(callStore, gate, provider.BuildEmbedder, os.Getenv)

	// PROMPTS_RUN_TTL bounds each run's wall-clock — the runaway-goroutine backstop
	// (§5.3). Parsed as a Go duration (e.g. "30m", "2h"). Read here at the domain
	// boundary, reusing appkit/config's env helper.
	runTTL, err := config.EnvOrDuration(os.Getenv, "PROMPTS_RUN_TTL", 30*time.Minute)
	if err != nil {
		return err
	}

	// PROMPTS_DB_PATH is appkit's state DB path. Sandboxes are durable state
	// beside it; runs are boot-recreated scratch under the generation cache.
	dbPath := config.EnvOr(os.Getenv, "PROMPTS_DB_PATH", "./tmp/prompts.db")
	generationPath := config.EnvOr(os.Getenv, "PROMPTS_GENERATION_PATH", filepath.Join(filepath.Dir(dbPath), "prompts.db.generation"))
	// PROMPTS_MANIFEST_ROOT is the box inventory root the runner reads at run
	// spawn to discover the suite's other loopback MCP services (Surface 2 —
	// in-run suite tools). Defaults to /opt, the on-box layout root.
	manifestRoot := config.EnvOr(os.Getenv, "PROMPTS_MANIFEST_ROOT", "/opt")
	stateDir := filepath.Dir(dbPath)
	sandboxesDir := filepath.Join(stateDir, "sandboxes")
	cacheDir := filepath.Dir(generationPath)
	runsDir := filepath.Join(cacheDir, "runs")
	if err := recreateRunsDir(runsDir); err != nil {
		return err
	}
	sb, err := sandbox.New(sandboxesDir)
	if err != nil {
		return fmt.Errorf("prompts: sandbox: %w", err)
	}

	store := prompt.NewStore(conn)
	store.Calls = callStore
	registerUIRoutes(rt, store, callStore)
	allowedPorts := make(map[int]bool, len(registry.Services))
	for _, service := range registry.Services {
		allowedPorts[service.Port] = true
	}
	dropboxBase := dropboxBaseURL(os.Getenv)
	run := runner.New(store, sb, gate, runTTL, manifestRoot, func(port int) bool { return allowedPorts[port] }, dropboxBase)
	svc := prompt.NewService(store, sb, runsDir, run)
	// Wire the dropbox loopback content fetcher for the import verb. DROPBOX_BASE_URL
	// is env-only (defaulting through the shared registry), the same
	// loopback-URL-via-env shape notify uses for its feed URLs. Field-injected so
	// NewService stays unchanged.
	svc.Fetcher = prompt.NewHTTPFetcher(dropboxBase)
	// Capture the service for the consumer Worker and the store for the Producer
	// hook (both run after Handlers; the Producer injects the outbox onto store).
	svcRef = svc
	storeRef = store

	// Crash-recovery sweep: runs left 'running' by a previous process are
	// orphaned — mark them failed before serving (touches RUNS only; prompts
	// have no status). Runs after migrate (appkit migrated the shared conn
	// before calling Handlers) and before the server begins listening.
	if swept, err := run.Recover(context.Background()); err != nil {
		return fmt.Errorf("prompts: crash-recovery sweep: %w", err)
	} else if swept > 0 {
		rt.Logger().Warn("crash-recovery: swept orphaned runs", "count", swept)
	}

	contentBase := registry.BaseURL("prompts")
	handler, err := mcp.NewHandler(svc, contentBase, rt)
	if err != nil {
		return err
	}
	rt.Handle("POST /mcp", rt.RequireIdentity(handler))
	rt.HandleLoopback("POST /complete", completion.CompleteHandler())
	rt.HandleLoopback("POST /embed", embedding.EmbedHandler())
	rt.HandleLoopback("GET /run-content", svc.RunContentHandler())
	return nil
}

const (
	uiPageSize        = 50
	uiBodyInlineLimit = 64 << 10
)

type promptBrowser interface {
	BrowsePrompts(context.Context, prompt.BrowseFilter) ([]prompt.Prompt, int, error)
	BrowseRuns(context.Context, prompt.BrowseFilter) ([]prompt.Run, int, error)
}

type promptDetailBrowser interface {
	GetPromptByID(context.Context, string) (prompt.Prompt, error)
	GetRun(context.Context, string) (prompt.Run, error)
	ListTriggers(context.Context, string) ([]prompt.Trigger, error)
}

type callBrowser interface {
	ListByGroup(context.Context, string) ([]calls.Row, error)
	Get(context.Context, string) (calls.Row, error)
}

type uiStore interface {
	promptBrowser
	promptDetailBrowser
}

type uiChrome struct {
	Service string
	Version string
}

type promptsPageData struct {
	uiChrome
	Prompts []prompt.Prompt
	Q       string
	Page    int
	Pages   int
	PrevURL string
	NextURL string
}

type runListRow struct {
	ID         string
	PromptID   string
	PromptName string
	Status     string
	OwnerEmail string
	StartedAt  string
	Duration   string
	Trigger    string
}

type runsPageData struct {
	uiChrome
	Runs     []runListRow
	Q        string
	Status   string
	PromptID string
	Page     int
	Pages    int
	PrevURL  string
	NextURL  string
}

type promptPageData struct {
	uiChrome
	Prompt     prompt.Prompt
	ConfigJSON string
	Triggers   []prompt.Trigger
}

type uiCallBody struct {
	Present   bool
	Content   string
	Truncated bool
	RawURL    string
}

type uiCallRow struct {
	calls.Row
	Request  uiCallBody
	Response uiCallBody
}

type runPageData struct {
	uiChrome
	Run       prompt.Run
	UsageJSON string
	Calls     []uiCallRow
}

type notFoundPageData struct {
	uiChrome
	Title   string
	Message string
}

func registerUIRoutes(rt *appkit.Router, store uiStore, callStore callBrowser) {
	rt.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "ui/")
		w.WriteHeader(http.StatusSeeOther)
	})
	rt.HandleFunc("GET /ui/{$}", promptsListHandler(rt, store))
	rt.HandleFunc("GET /ui/runs", runsListHandler(rt, store))
	rt.HandleFunc("GET /ui/prompts/{id}", promptDetailHandler(rt, store))
	rt.HandleFunc("GET /ui/runs/{id}", runDetailHandler(rt, store, callStore))
	rt.HandleFunc("GET /ui/calls/{id}/raw", rawCallBodyHandler(callStore))
}

func promptDetailHandler(rt *appkit.Router, store promptDetailBrowser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row, err := store.GetPromptByID(r.Context(), r.PathValue("id"))
		if errors.Is(err, prompt.ErrNotFound) {
			renderUINotFound(rt, w, "Prompt not found", "This prompt does not exist or was deleted.")
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		triggers, err := store.ListTriggers(r.Context(), row.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		config, err := json.MarshalIndent(row.Config, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := promptPageData{
			uiChrome:   uiChrome{Service: rt.Service(), Version: rt.Version()},
			Prompt:     row,
			ConfigJSON: string(config),
			Triggers:   triggers,
		}
		if err := rt.WWW().Render(w, "ui-prompt.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func runDetailHandler(rt *appkit.Router, store promptDetailBrowser, callStore callBrowser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run, err := store.GetRun(r.Context(), r.PathValue("id"))
		if errors.Is(err, prompt.ErrNotFound) {
			renderUINotFound(rt, w, "Run not found", "This run does not exist.")
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows, err := callStore.ListByGroup(r.Context(), run.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		shown := make([]uiCallRow, 0, len(rows))
		for _, row := range rows {
			shown = append(shown, uiCallRow{
				Row:      row,
				Request:  inlineCallBody(row.ID, "request", row.RequestBody),
				Response: inlineCallBody(row.ID, "response", row.ResponseBody),
			})
		}
		data := runPageData{
			uiChrome: uiChrome{Service: rt.Service(), Version: rt.Version()},
			Run:      run, UsageJSON: prettyJSON(run.UsageJSON), Calls: shown,
		}
		if err := rt.WWW().Render(w, "ui-run.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func rawCallBodyHandler(store callBrowser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		side := r.URL.Query().Get("side")
		if side != "request" && side != "response" {
			http.Error(w, "side must be request or response", http.StatusBadRequest)
			return
		}
		row, err := store.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "call not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body := row.RequestBody
		if side == "response" {
			body = row.ResponseBody
		}
		if body == nil {
			http.Error(w, "body pruned by retention or no body recorded", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(*body))
	}
}

func inlineCallBody(callID, side string, body *string) uiCallBody {
	if body == nil {
		return uiCallBody{}
	}
	if len(*body) > uiBodyInlineLimit {
		return uiCallBody{
			Present: true, Content: (*body)[:uiBodyInlineLimit], Truncated: true,
			RawURL: "/srv/prompts/ui/calls/" + url.PathEscape(callID) + "/raw?side=" + side,
		}
	}
	return uiCallBody{Present: true, Content: prettyJSON(*body)}
}

func prettyJSON(value string) string {
	if value == "" {
		return ""
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(value), "", "  "); err != nil {
		return value
	}
	return out.String()
}

func renderUINotFound(rt *appkit.Router, w http.ResponseWriter, title, message string) {
	w.WriteHeader(http.StatusNotFound)
	_ = rt.WWW().Render(w, "ui-404.html", notFoundPageData{
		uiChrome: uiChrome{Service: rt.Service(), Version: rt.Version()},
		Title:    title, Message: message,
	})
}

func promptsListHandler(rt *appkit.Router, store promptBrowser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := queryPage(r)
		q := r.URL.Query().Get("q")
		rows, total, err := store.BrowsePrompts(r.Context(), prompt.BrowseFilter{
			Q: q, Limit: uiPageSize, Offset: (page - 1) * uiPageSize,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		pages := pageCount(total)
		data := promptsPageData{
			uiChrome: uiChrome{Service: rt.Service(), Version: rt.Version()},
			Prompts:  rows, Q: q, Page: page, Pages: pages,
		}
		data.PrevURL, data.NextURL = pagerURLs("/srv/prompts/ui/", r.URL.Query(), page, pages)
		if err := rt.WWW().Render(w, "ui-prompts.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func runsListHandler(rt *appkit.Router, store promptBrowser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := queryPage(r)
		query := r.URL.Query()
		filter := prompt.BrowseFilter{
			Q: query.Get("q"), Status: query.Get("status"), PromptID: query.Get("prompt_id"),
			Limit: uiPageSize, Offset: (page - 1) * uiPageSize,
		}
		runs, total, err := store.BrowseRuns(r.Context(), filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows := make([]runListRow, 0, len(runs))
		for _, run := range runs {
			rows = append(rows, runListRow{
				ID: run.ID, PromptID: run.PromptID, PromptName: run.PromptName,
				Status: run.Status, OwnerEmail: run.OwnerEmail, StartedAt: run.StartedAt,
				Duration: runDuration(run.StartedAt, run.EndedAt), Trigger: runTrigger(run.TriggerSource, run.TriggerKind),
			})
		}
		pages := pageCount(total)
		data := runsPageData{
			uiChrome: uiChrome{Service: rt.Service(), Version: rt.Version()},
			Runs:     rows, Q: filter.Q, Status: filter.Status, PromptID: filter.PromptID,
			Page: page, Pages: pages,
		}
		data.PrevURL, data.NextURL = pagerURLs("/srv/prompts/ui/runs", query, page, pages)
		if err := rt.WWW().Render(w, "ui-runs.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func queryPage(r *http.Request) int {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func pageCount(total int) int {
	pages := (total + uiPageSize - 1) / uiPageSize
	if pages < 1 {
		return 1
	}
	return pages
}

func pagerURLs(path string, query url.Values, page, pages int) (string, string) {
	pageURL := func(n int) string {
		q := url.Values{}
		for key, values := range query {
			if key == "page" {
				continue
			}
			for _, value := range values {
				q.Add(key, value)
			}
		}
		q.Set("page", strconv.Itoa(n))
		return path + "?" + q.Encode()
	}
	var prev, next string
	if page > 1 {
		prev = pageURL(page - 1)
	}
	if page < pages {
		next = pageURL(page + 1)
	}
	return prev, next
}

func runDuration(startedAt, endedAt string) string {
	if endedAt == "" {
		return ""
	}
	started, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return ""
	}
	ended, err := time.Parse(time.RFC3339Nano, endedAt)
	if err != nil || ended.Before(started) {
		return ""
	}
	return ended.Sub(started).Round(time.Second).String()
}

func runTrigger(source, kind string) string {
	if source == "" {
		return ""
	}
	if kind == "" {
		return source
	}
	return strings.TrimSpace(source) + " / " + strings.TrimSpace(kind)
}

func dropboxBaseURL(getenv func(string) string) string {
	return config.EnvOr(getenv, "DROPBOX_BASE_URL", registry.BaseURL("dropbox"))
}

func recreateRunsDir(runsDir string) error {
	if runsDir == "" || runsDir == "." || runsDir == string(os.PathSeparator) {
		return fmt.Errorf("prompts: invalid runs dir %q", runsDir)
	}
	if err := os.RemoveAll(runsDir); err != nil {
		return fmt.Errorf("prompts: recreate runs dir: remove %s: %w", runsDir, err)
	}
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return fmt.Errorf("prompts: recreate runs dir: mkdir %s: %w", runsDir, err)
	}
	return nil
}

func callsBodyRetentionConfig(getenv func(string) string) (int, error) {
	raw := config.EnvOr(getenv, "PROMPTS_CALLS_BODY_RETENTION_DAYS", "30")
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 {
		return 0, fmt.Errorf("PROMPTS_CALLS_BODY_RETENTION_DAYS must be a non-negative integer, got %q", raw)
	}
	return days, nil
}

func callsBodyCutoff(now time.Time, days int) time.Time {
	return now.UTC().AddDate(0, 0, -days)
}

func callsBodyRetentionWorker(ctx context.Context) error {
	if callsRef == nil {
		return fmt.Errorf("prompts: calls retention worker started before Handlers built the Store")
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if _, err := callsRef.PruneBodies(ctx, callsBodyCutoff(now, callsRetentionDays)); err != nil {
				return fmt.Errorf("prompts: calls body-retention sweep: %w", err)
			}
		}
	}
}
