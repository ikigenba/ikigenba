package prompt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"eventplane/correlation"
	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"
	"prompts/internal/calls"
	"prompts/internal/ids"
	"prompts/internal/sandbox"
	"prompts/internal/version"
)

// ValidationError wraps a config-validation failure (bad model, wrong
// provider, missing key). Distinct so the MCP layer can map it to an
// invalid-params response rather than an internal error.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func validationErrf(format string, a ...any) *ValidationError {
	return &ValidationError{msg: fmt.Sprintf(format, a...)}
}

// Runner drives a run's async lifecycle. Spawn returns immediately; the
// runner writes the run's terminal state on its own. Cancel signals an
// in-flight run by run_id; returns whether one was found.
type Runner interface {
	Spawn(run Run)
	Cancel(runID string) bool
}

// RootStarter establishes the recorded origin for a run that starts its own
// correlation chain. The composition root owns the chassis-specific work.
type RootStarter func(ctx context.Context, rootID, op string) context.Context

// Service is prompts' domain service — the only mutator of prompt/run state.
// Runs are fully concurrent: there is no prompt status and no single-flight
// gate. The MCP handler talks only to it.
type Service struct {
	store   *Store
	sandbox *sandbox.Manager
	runsDir string
	runner  Runner
	now     func() time.Time
	// Fetcher reads bytes from the dropbox mirror over loopback for Import. It is
	// field-injected at the composition root (cmd/prompts sets svc.Fetcher), not a
	// NewService parameter, so every existing NewService call site and test stays
	// untouched. nil unless wired (only Import uses it).
	Fetcher ContentFetcher
	// SubAuthAvailable probes the optional OpenAI subscription credential.
	// It is field-injected by the composition root and defaults to unavailable.
	SubAuthAvailable func() bool
	// RootStarter is the chassis seam used when a run establishes a new chain.
	// It receives the durable run id and the run:<run-id> operation name.
	// A nil seam leaves ctx unchanged for telemetry-disabled builds and tests.
	RootStarter RootStarter
	// Version is the version-plane boundary for prompt definitions. It is
	// field-injected by the composition root, like Fetcher.
	Version version.Client
}

// NewService wires the store, sandbox manager, run-logs base dir, and runner.
func NewService(store *Store, sb *sandbox.Manager, runsDir string, runner Runner) *Service {
	return &Service{
		store:            store,
		sandbox:          sb,
		runsDir:          runsDir,
		runner:           runner,
		now:              func() time.Time { return time.Now().UTC() },
		SubAuthAvailable: func() bool { return false },
	}
}

func (s *Service) startRoot(ctx context.Context, runID, op string) context.Context {
	if s.RootStarter == nil {
		return ctx
	}
	return s.RootStarter(ctx, runID, op)
}

// CallStore returns the suite-wide inference accounting store wired at the
// composition root. Inspection/reporting callers intentionally apply no owner
// scoping: identity is enforced by the transport before they reach the store.
func (s *Service) CallStore() *calls.Store {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Calls
}

func (s *Service) nowStr() string { return s.now().UTC().Format(time.RFC3339Nano) }

func (s *Service) rejectNameKeyCollision(ctx context.Context, promptID, nameKey string) error {
	existing, err := s.store.GetPromptByNameKey(ctx, nameKey)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.ID == promptID {
		return nil
	}
	return validationErrf("prompt name conflicts with existing prompt %q using key %q", existing.Name, nameKey)
}

// CreateInput is the create payload. Triggers is the optional create-time sugar:
// each canonical filter is applied via SetTrigger after the prompt row is
// inserted (same validation; an invalid one rejects the whole create).
type CreateInput struct {
	Name         string
	UserPrompt   string
	SystemPrompt string
	Config       Config
	Triggers     []TriggerSpec
}

// UpdateInput is the update payload (all fields replace).
type UpdateInput struct {
	Name         string
	UserPrompt   string
	SystemPrompt string
	Config       Config
}

var providerEnvVars = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"google":     "GEMINI_API_KEY",
	"zai":        "ZAI_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
}

// ValidateConfig validates and normalizes a config for storage. getenv is
// injected so tests can exercise the key contract without mutating the process
// environment. The returned config differs only when the catalog supplies an
// omitted provider.
func ValidateConfig(c Config, getenv func(string) string, subAuthAvailable func() bool) (Config, error) {
	entry, ok := catalog.Lookup(c.Model)
	if !ok || len(entry.Offerings) == 0 {
		return Config{}, validationErrf("invalid config: unknown prompt model %q", c.Model)
	}

	if c.Provider == "" {
		c.Provider = string(entry.Offerings[0].Provider)
	}
	envVar, ok := providerEnvVars[c.Provider]
	if !ok {
		return Config{}, validationErrf("invalid config: provider %q is not supported", c.Provider)
	}
	if _, ok := catalog.Offer(c.Model, catalogProviderID(c.Provider)); !ok {
		return Config{}, validationErrf("invalid config: provider %q does not route model %q", c.Provider, c.Model)
	}

	var reasoning agentkit.ReasoningValue
	switch {
	case c.Effort != "":
		reasoning = agentkit.Level(c.Effort)
	case c.ThinkingLevel != "":
		reasoning = agentkit.Level(c.ThinkingLevel)
	case c.ThinkingBudget != nil:
		reasoning = agentkit.Budget(*c.ThinkingBudget)
	case c.Thinking != nil && !*c.Thinking:
		reasoning = agentkit.DisableReasoning()
	}
	if !reasoning.IsUnset() {
		accepted, spec, _ := catalog.Check(c.Model, catalogProviderID(c.Provider), reasoning)
		if !accepted {
			if spec == nil {
				return Config{}, validationErrf("invalid config: model %q through provider %q accepts no reasoning control", c.Model, c.Provider)
			}
			if reasoning.Disabled() && !spec.CanDisable {
				return Config{}, validationErrf("invalid config: reasoning cannot be disabled for model %q through provider %q", c.Model, c.Provider)
			}
			if len(spec.Levels) > 0 {
				return Config{}, validationErrf("invalid config: model %q through provider %q accepts reasoning levels %q", c.Model, c.Provider, spec.Levels)
			}
			return Config{}, validationErrf("invalid config: model %q through provider %q accepts reasoning budgets %d..%d and sentinels %v", c.Model, c.Provider, spec.Min, spec.Max, spec.Sentinels)
		}
	}

	switch c.Auth {
	case "", "key":
	case "sub":
		if c.Provider != "openai" {
			return Config{}, validationErrf("invalid config: auth %q requires provider %q, got %q", c.Auth, "openai", c.Provider)
		}
		if c.BaseURL != "" {
			return Config{}, validationErrf("invalid config: auth %q cannot be combined with base_url", c.Auth)
		}
		if subAuthAvailable == nil || !subAuthAvailable() {
			return Config{}, validationErrf("invalid config: auth %q requires PROMPTS_OPENAI_AUTH_PATH or the state-dir default auth.json", c.Auth)
		}
	default:
		return Config{}, validationErrf("invalid config: auth must be one of %q, %q, or %q", "", "key", "sub")
	}

	if c.Auth == "sub" {
		return c, nil
	}
	if getenv(envVar) == "" {
		return Config{}, validationErrf("invalid config: %s is not set", envVar)
	}
	return c, nil
}

// CatalogProviderID translates prompts' stable config spelling for Z.ai to
// AgentKit's vendor-namespace provider identity.
func CatalogProviderID(provider string) agentkit.ProviderID {
	return catalogProviderID(provider)
}

func catalogProviderID(provider string) agentkit.ProviderID {
	if provider == "zai" {
		return agentkit.ProviderZAI
	}
	return agentkit.ProviderID(provider)
}

// Create validates config and inserts a prompt. The sandbox is no longer
// created here — it is per-run, created at Run time keyed by run_id.
// Config.Provider is validated against the configured model and preserved.
func (s *Service) Create(ctx context.Context, ownerID, ownerEmail string, in CreateInput) (Prompt, error) {
	cfg, err := ValidateConfig(in.Config, os.Getenv, s.SubAuthAvailable)
	if err != nil {
		return Prompt{}, err
	}
	// Validate every inline trigger BEFORE inserting the prompt, so an invalid
	// binding rejects the whole create without leaving a triggerless orphan.
	for _, ts := range in.Triggers {
		if _, err := validateTrigger(ts.Filter); err != nil {
			return Prompt{}, err
		}
	}
	id := ids.NewULID()
	nameKey := NameKey(in.Name, id)
	if err := s.rejectNameKeyCollision(ctx, id, nameKey); err != nil {
		return Prompt{}, err
	}
	now := s.nowStr()
	p := Prompt{
		ID:           id,
		OwnerID:      ownerID,
		OwnerEmail:   ownerEmail,
		Name:         in.Name,
		NameKey:      nameKey,
		UserPrompt:   in.UserPrompt,
		SystemPrompt: in.SystemPrompt,
		Config:       cfg,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.InsertPrompt(ctx, p); err != nil {
		return Prompt{}, err
	}
	rollback := func() {
		_ = s.store.DeleteTriggers(ctx, p.ID)
		_ = s.store.DeletePrompt(ctx, ownerID, p.ID)
	}
	// Apply the inline triggers (already validated) via the store SetTrigger.
	for _, ts := range in.Triggers {
		source, _ := validateTrigger(ts.Filter)
		if err := s.store.SetTrigger(ctx, Trigger{
			PromptID:  p.ID,
			Source:    source,
			Filter:    ts.Filter,
			CreatedAt: now,
		}); err != nil {
			rollback()
			return Prompt{}, err
		}
	}
	if s.Version != nil {
		actor := "prompts:" + p.ID
		if err := s.Version.Create(ctx, p.NameKey, actor); err != nil {
			rollback()
			return Prompt{}, versionPlaneError("create repository", err)
		}
		files, err := definitionFiles(in.UserPrompt, in.SystemPrompt, cfg)
		if err != nil {
			rollback()
			return Prompt{}, err
		}
		if _, err := s.Version.Commit(ctx, p.NameKey, files, "create "+p.Name, actor); err != nil {
			rollback()
			return Prompt{}, versionPlaneError("commit create", err)
		}
	}
	return p, nil
}

func definitionFiles(userPrompt, systemPrompt string, cfg Config) ([]version.File, error) {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("prompt: marshal version config: %w", err)
	}
	configJSON = append(configJSON, '\n')
	files := []version.File{
		{Path: "prompt.md", Data: []byte(userPrompt)},
		{Path: "config.json", Data: configJSON},
	}
	if systemPrompt != "" {
		files = append(files, version.File{Path: "system.md", Data: []byte(systemPrompt)})
	}
	return files, nil
}

func versionPlaneError(op string, err error) error {
	return fmt.Errorf("prompt: version plane %s: %w", op, err)
}

// maxImportBytes caps an imported prompt body at 1 MiB — a prompt file above
// that is almost certainly a mistake (ADR "Asserted defaults").
const maxImportBytes = 1 << 20

// importDefaultModel is the model an imported prompt's config defaults to. A
// prompt is multi-field but a file is one blob, so import maps the file body →
// user_prompt and leaves the other fields at sane defaults (ADR Decision 5). The
// config must still validate, so import seeds an explicit provider and the
// canonical Sonnet id (the same model the describe doc shows as the example
// config) so an imported prompt is immediately runnable.
const importDefaultModel = "claude-sonnet-4-6"

// Import adopts a Dropbox-mirrored file as a prompt. It fetches the current
// mirror bytes over loopback, requires valid UTF-8 text under 1 MiB (a binary
// blob is not prompt text), maps the file body → user_prompt, derives the name
// from the path when none is given, leaves system_prompt empty, and seeds a valid
// default config (importDefaultModel) so the prompt is runnable. It upserts on
// (owner, source_path): re-importing the same path updates the same prompt
// instead of creating a duplicate (config + system_prompt keep their values on a
// re-import — a re-pull is a body refresh, not a config change). Direction is
// strictly Dropbox → prompts; nothing writes back. See
// docs/adr-dropbox-import-sync.md.
func (s *Service) Import(ctx context.Context, ownerID, ownerEmail, sourcePath, name string) (Prompt, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return Prompt{}, fmt.Errorf("%w: source_path is required", ErrValidation)
	}
	data, err := s.Fetcher.Fetch(ctx, sourcePath)
	if err != nil {
		return Prompt{}, err
	}
	if !utf8.Valid(data) {
		return Prompt{}, fmt.Errorf("%w: %q is not valid UTF-8 text (a prompt body must be text)", ErrValidation, sourcePath)
	}
	if len(data) > maxImportBytes {
		return Prompt{}, fmt.Errorf("%w: %q is %d bytes, over the 1 MiB import limit", ErrTooLarge, sourcePath, len(data))
	}
	if name == "" {
		name = path.Base(sourcePath)
	}
	// Seed a valid default config so the imported prompt is runnable. Config is
	// left as-is on a re-import (the upsert refreshes name + user_prompt only), so
	// this default only takes effect on the first import.
	cfg, err := ValidateConfig(Config{Model: importDefaultModel}, os.Getenv, s.SubAuthAvailable)
	if err != nil {
		return Prompt{}, err
	}
	var existing *Prompt
	prompts, err := s.store.ListPrompts(ctx, ownerID)
	if err != nil {
		return Prompt{}, err
	}
	for i := range prompts {
		if prompts[i].SourcePath == sourcePath {
			existing = &prompts[i]
			break
		}
	}

	if existing != nil && s.Version != nil {
		files := []version.File{{Path: "prompt.md", Data: data}}
		if _, err := s.Version.Commit(ctx, existing.NameKey, files, "import "+sourcePath, "prompts:"+existing.ID); err != nil {
			return Prompt{}, versionPlaneError("commit import", err)
		}
	}

	p, err := s.store.UpsertPromptBySource(ctx, ownerID, ownerEmail, sourcePath, name, string(data), cfg, s.nowStr())
	if err != nil {
		return Prompt{}, err
	}
	if existing != nil || s.Version == nil {
		return p, nil
	}

	p.NameKey = NameKey(p.Name, p.ID)
	if err := s.rejectNameKeyCollision(ctx, p.ID, p.NameKey); err != nil {
		_ = s.store.DeletePrompt(ctx, ownerID, p.ID)
		return Prompt{}, err
	}
	if err := s.store.UpdatePrompt(ctx, ownerID, p); err != nil {
		_ = s.store.DeletePrompt(ctx, ownerID, p.ID)
		return Prompt{}, err
	}
	actor := "prompts:" + p.ID
	if err := s.Version.Create(ctx, p.NameKey, actor); err != nil {
		_ = s.store.DeletePrompt(ctx, ownerID, p.ID)
		return Prompt{}, versionPlaneError("create import repository", err)
	}
	files, err := definitionFiles(string(data), "", cfg)
	if err != nil {
		_ = s.store.DeletePrompt(ctx, ownerID, p.ID)
		return Prompt{}, err
	}
	if _, err := s.Version.Commit(ctx, p.NameKey, files, "import "+sourcePath, actor); err != nil {
		_ = s.store.DeletePrompt(ctx, ownerID, p.ID)
		return Prompt{}, versionPlaneError("commit import", err)
	}
	return p, nil
}

// List returns the owner's prompts, each as a PromptDetail carrying its derived
// RunningCount + LastRun.
func (s *Service) List(ctx context.Context, ownerID string) ([]PromptDetail, error) {
	prompts, err := s.store.ListPrompts(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]PromptDetail, 0, len(prompts))
	for _, p := range prompts {
		d, err := s.detail(ctx, p)
		if err != nil {
			return nil, err
		}
		d.UserPrompt = ""
		d.SystemPrompt = ""
		d.Config = Config{}
		out = append(out, d)
	}
	return out, nil
}

// Get returns the owner's prompt as a PromptDetail (RunningCount + LastRun).
func (s *Service) Get(ctx context.Context, ownerID, id string) (PromptDetail, error) {
	p, err := s.store.GetPrompt(ctx, ownerID, id)
	if err != nil {
		return PromptDetail{}, err
	}
	detail, err := s.detail(ctx, p)
	if err != nil {
		return PromptDetail{}, err
	}
	if s.Version == nil {
		return detail, nil
	}
	definition, err := s.Version.Read(ctx, p.NameKey, "main")
	if err != nil {
		return PromptDetail{}, versionPlaneError("read definition", err)
	}
	var cfg Config
	if err := json.Unmarshal(definition.ConfigJSON, &cfg); err != nil {
		return PromptDetail{}, versionPlaneError("parse definition config", err)
	}
	detail.UserPrompt = definition.UserPrompt
	detail.SystemPrompt = definition.SystemPrompt
	detail.Config = cfg
	return detail, nil
}

// LoadFromPrompt reads the live, owner-scoped prompt definition. The spawn path
// uses it at the point where mutable prompt state becomes a frozen run input.
func (s *Service) LoadFromPrompt(ctx context.Context, ownerID, promptID string) (Executed, error) {
	p, err := s.store.GetPrompt(ctx, ownerID, promptID)
	if err != nil {
		return Executed{}, err
	}
	if s.Version == nil {
		return Executed{Name: p.Name, UserPrompt: p.UserPrompt, SystemPrompt: p.SystemPrompt, Config: p.Config}, nil
	}
	definition, err := s.Version.Read(ctx, p.NameKey, "main")
	if err != nil {
		return Executed{}, versionPlaneError("read definition", err)
	}
	var cfg Config
	if err := json.Unmarshal(definition.ConfigJSON, &cfg); err != nil {
		return Executed{}, versionPlaneError("parse definition config", err)
	}
	return Executed{Name: p.Name, UserPrompt: definition.UserPrompt, SystemPrompt: definition.SystemPrompt, Config: cfg}, nil
}

// LoadFromRun reads a run's frozen prompt definition directly from input/.
// Authorization is intentionally outside this package-level loader.
func LoadFromRun(runsDir, runID string) (Executed, error) {
	inputDir := filepath.Join(runsDir, runID, "input")
	read := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(inputDir, name))
		if err != nil {
			return nil, fmt.Errorf("prompt: read run %s: %w", name, err)
		}
		return b, nil
	}
	name, err := os.ReadFile(filepath.Join(inputDir, "name.txt"))
	if err != nil && !os.IsNotExist(err) {
		return Executed{}, fmt.Errorf("prompt: read run name.txt: %w", err)
	}
	userPrompt, err := read("user_prompt.txt")
	if err != nil {
		return Executed{}, err
	}
	systemPrompt, err := read("system_prompt.txt")
	if err != nil {
		return Executed{}, err
	}
	configJSON, err := read("config.json")
	if err != nil {
		return Executed{}, err
	}
	var cfg Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return Executed{}, fmt.Errorf("prompt: parse run config: %w", err)
	}
	return Executed{Name: string(name), UserPrompt: string(userPrompt), SystemPrompt: string(systemPrompt), Config: cfg}, nil
}

// detail enriches a Prompt with its derived run summary (RunningCount +
// LastRun) — the shared body of Get/List.
func (s *Service) detail(ctx context.Context, p Prompt) (PromptDetail, error) {
	running, err := s.store.RunningCount(ctx, p.ID)
	if err != nil {
		return PromptDetail{}, err
	}
	last, err := s.store.GetLatestRun(ctx, p.ID)
	if err != nil {
		return PromptDetail{}, err
	}
	return PromptDetail{Prompt: p, RunningCount: running, LastRun: last}, nil
}

// Update edits name/user_prompt/system_prompt/config. ALWAYS allowed (no
// single-flight); re-validates config.
func (s *Service) Update(ctx context.Context, ownerID, id string, in UpdateInput) (Prompt, error) {
	p, err := s.store.GetPrompt(ctx, ownerID, id)
	if err != nil {
		return Prompt{}, err
	}
	cfg, err := ValidateConfig(in.Config, os.Getenv, s.SubAuthAvailable)
	if err != nil {
		return Prompt{}, err
	}
	nameKey := NameKey(in.Name, p.ID)
	if err := s.rejectNameKeyCollision(ctx, p.ID, nameKey); err != nil {
		return Prompt{}, err
	}
	if s.Version != nil && nameKey != p.NameKey {
		if err := s.Version.Rename(ctx, p.NameKey, nameKey); err != nil {
			return Prompt{}, versionPlaneError("rename repository", err)
		}
	}
	if s.Version != nil {
		files := make([]version.File, 0, 3)
		if in.UserPrompt != p.UserPrompt {
			files = append(files, version.File{Path: "prompt.md", Data: []byte(in.UserPrompt)})
		}
		if !reflect.DeepEqual(cfg, p.Config) {
			configJSON, err := json.Marshal(cfg)
			if err != nil {
				return Prompt{}, fmt.Errorf("prompt: marshal version config: %w", err)
			}
			files = append(files, version.File{Path: "config.json", Data: append(configJSON, '\n')})
		}
		if in.SystemPrompt != p.SystemPrompt {
			file := version.File{Path: "system.md", Data: []byte(in.SystemPrompt)}
			if in.SystemPrompt == "" {
				file.Data = nil
				file.Delete = true
			}
			files = append(files, file)
		}
		if len(files) > 0 {
			if _, err := s.Version.Commit(ctx, nameKey, files, "update "+in.Name, "prompts:"+p.ID); err != nil {
				return Prompt{}, versionPlaneError("commit update", err)
			}
		}
	}

	p.Name = in.Name
	p.NameKey = nameKey
	p.UserPrompt = in.UserPrompt
	p.SystemPrompt = in.SystemPrompt
	p.Config = cfg
	p.UpdatedAt = s.nowStr()

	if err := s.store.UpdatePrompt(ctx, ownerID, p); err != nil {
		return Prompt{}, err
	}
	return p, nil
}

// Delete removes ONLY the prompt row and the prompt's trigger(s). It deliberately
// leaves the prompt's runs, their on-disk run
// directories (output.jsonl, input/, sandbox/), and any emitted outbox rows in
// place — a run stays owner-addressable by run_id (via the run's denormalized
// owner_email) after its prompt is gone. ALWAYS allowed (no single-flight).
func (s *Service) Delete(ctx context.Context, ownerID, id string) error {
	p, err := s.store.GetPrompt(ctx, ownerID, id)
	if err != nil {
		return err
	}
	if s.Version != nil {
		if err := s.Version.Archive(ctx, p.NameKey); err != nil {
			return versionPlaneError("archive repository", err)
		}
	}
	// Row first (owner-scoped): if this fails nothing else is touched.
	if err := s.store.DeletePrompt(ctx, ownerID, id); err != nil {
		return err
	}
	// Remove the prompt's trigger(s) explicitly — there is no FK cascade.
	if err := s.store.DeleteTriggers(ctx, id); err != nil {
		return err
	}
	return nil
}

// Run starts a run for the owner's prompt. It is ALWAYS accepted — there is no
// single-flight gate, so any number of runs of one prompt may execute
// concurrently, each in its own run-scoped sandbox (keyed by run_id).
func (s *Service) Run(ctx context.Context, ownerID, id string) (Run, error) {
	p, err := s.store.GetPrompt(ctx, ownerID, id)
	if err != nil {
		return Run{}, err
	}
	return s.startRun(ctx, p, "", "", "", "", nil)
}

// materializeInput pins the prompt's changeable execution inputs to
// runs/<run_id>/input/ BEFORE spawn: user_prompt.txt, system_prompt.txt (empty
// file when none), and config.json (the resolved Config). Once written, this is
// the record of exactly what the run executes — the runner reads from here, so
// editing or deleting the prompt mid-run cannot change what the run runs. For an
// event-triggered run it also writes event.json (the triggering event envelope);
// that file is absent on a manual run.
func (s *Service) materializeInput(run Run, executed Executed, payload []byte) error {
	inputDir := filepath.Join(s.runsDir, run.ID, "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return fmt.Errorf("prompt: create run input dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "name.txt"), []byte(executed.Name), 0o644); err != nil {
		return fmt.Errorf("prompt: write name: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "user_prompt.txt"), []byte(executed.UserPrompt), 0o644); err != nil {
		return fmt.Errorf("prompt: write user_prompt: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "system_prompt.txt"), []byte(executed.SystemPrompt), 0o644); err != nil {
		return fmt.Errorf("prompt: write system_prompt: %w", err)
	}
	cfgJSON, err := json.Marshal(executed.Config)
	if err != nil {
		return fmt.Errorf("prompt: marshal run config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "config.json"), cfgJSON, 0o644); err != nil {
		return fmt.Errorf("prompt: write config: %w", err)
	}
	if run.TriggerSource != "" {
		env := eventEnvelope{Source: run.TriggerSource, Kind: run.TriggerKind, Subject: run.TriggerSubject, EventID: run.TriggerEventID, Payload: json.RawMessage(payload)}
		if len(env.Payload) == 0 {
			env.Payload = json.RawMessage("null")
		}
		b, err := json.Marshal(env)
		if err != nil {
			return fmt.Errorf("prompt: marshal run event: %w", err)
		}
		if err := os.WriteFile(filepath.Join(inputDir, "event.json"), b, 0o644); err != nil {
			return fmt.Errorf("prompt: write event: %w", err)
		}
	}
	return nil
}

// startRun is the shared run-start path for Run (manual) and RunByEvent (event).
// It builds the run row (denormalizing owner_email / prompt_name and the
// trigger context), materializes input/ from the prompt's CURRENT definition,
// inserts the row, creates the durable run-scoped sandbox, and hands off to the
// runner — which executes from disk, never from p.
type eventEnvelope struct {
	Source  string          `json:"source"`
	Kind    string          `json:"kind"`
	Subject string          `json:"subject"`
	EventID string          `json:"event_id"`
	Payload json.RawMessage `json:"payload"`
}

func (s *Service) startRun(ctx context.Context, p Prompt, source, kind, subject, eventID string, payload []byte) (Run, error) {
	executed, err := s.LoadFromPrompt(ctx, p.OwnerID, p.ID)
	if err != nil {
		return Run{}, err
	}
	runID := ids.NewULID()
	corrID := correlation.FromContext(ctx)
	if corrID == "" {
		corrID = runID
		ctx = s.startRoot(ctx, runID, "run:"+runID)
	}
	run := Run{
		ID:             runID,
		CorrelationID:  corrID,
		PromptID:       p.ID,
		OwnerID:        p.OwnerID,
		OwnerEmail:     p.OwnerEmail,
		PromptName:     p.Name,
		Status:         RunRunning,
		StartedAt:      s.nowStr(),
		LogPath:        filepath.Join(s.runsDir, runID, "output.jsonl"),
		TriggerSource:  source,
		TriggerKind:    kind,
		TriggerSubject: subject,
		TriggerEventID: eventID,
	}
	// Pin the execution inputs to disk BEFORE the row exists / spawn happens.
	if err := s.materializeInput(run, executed, payload); err != nil {
		return Run{}, err
	}
	if err := s.store.InsertRun(ctx, run); err != nil {
		return Run{}, err
	}
	// Create the durable run-scoped sandbox (state/sandboxes/<run_id>/sandbox)
	// before spawn.
	if err := s.sandbox.Create(runID); err != nil {
		return Run{}, fmt.Errorf("prompt: create run sandbox: %w", err)
	}
	s.runner.Spawn(run)
	return run, nil
}

// RunByEvent is the event-triggered run path: it starts a run for a prompt by id
// WITHOUT owner scoping (the trigger linkage, set by the owner, is the
// authority — there is no caller identity on an event). It otherwise mirrors
// Run: ALWAYS accepted (no single-flight), inserts the run row, creates the
// run-scoped sandbox, and hands off to the runner. ErrNotFound if the prompt
// is gone.
//
// source / evType / eventID are the trigger context (the upstream producer, the
// fired event type, and the upstream event id that fired this run); they are
// persisted on the Run row and FinishRun reads them back to populate the
// run.succeeded / run.failed outcome payload. All empty for a manual run (see
// Run, which passes "" / "" / ""). payload is the upstream producer's raw event
// body; it is pinned to the run's input/event.json (inside the canonical event
// envelope) for an event-triggered run.
func (s *Service) RunByEvent(ctx context.Context, id, source, kind string, args ...any) (Run, error) {
	var subject, eventID string
	var payload []byte
	switch len(args) {
	case 2:
		eventID, _ = args[0].(string)
		payload = eventPayload(args[1])
	case 3:
		subject, _ = args[0].(string)
		eventID, _ = args[1].(string)
		payload = eventPayload(args[2])
	default:
		return Run{}, fmt.Errorf("%w: invalid event arguments", ErrValidation)
	}
	p, err := s.store.GetPromptByID(ctx, id)
	if err != nil {
		return Run{}, err
	}
	return s.startRun(ctx, p, source, kind, subject, eventID, payload)
}

func eventPayload(v any) []byte {
	switch b := v.(type) {
	case []byte:
		return b
	case json.RawMessage:
		return []byte(b)
	default:
		return nil
	}
}

// readLines reads up to limit lines of the file at path starting at 1-based
// offset, mirroring sandbox.Read's line-slice semantics (offset<=0 → from
// start, limit<=0 → no limit).
func readLines(path string, offset, limit int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("prompt: run log not found")
		}
		return "", fmt.Errorf("prompt: open run log: %w", err)
	}
	defer f.Close()

	if offset <= 0 {
		offset = 1
	}
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	written := 0
	for sc.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if limit > 0 && written >= limit {
			break
		}
		b.WriteString(sc.Text())
		b.WriteByte('\n')
		written++
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("prompt: read run log: %w", err)
	}
	return b.String(), nil
}

// --- First-class, run_id-addressed run readers (A7) ---
//
// These are owner-scoped via the RUN's denormalized owner id, not via its
// prompt — so a run remains readable after its prompt has been deleted.

// runForOwner loads a run by run_id and enforces that it belongs to the caller
// via the run's denormalized owner_email. ErrNotFound when the run is missing or
// owned by another caller — identical to the prompt-not-owned semantics, so a
// foreign run is indistinguishable from an absent one.
func (s *Service) runForOwner(ctx context.Context, ownerID, runID string) (Run, error) {
	r, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	if r.OwnerID != ownerID {
		return Run{}, ErrNotFound
	}
	return r, nil
}

// RunList returns the owner's runs for promptID, newest first. The runs rows
// enforce ownership directly, so a deleted or unknown prompt yields its
// surviving owned runs or an empty list without consulting the prompts table.
func (s *Service) RunList(ctx context.Context, ownerID, promptID string, correlationID ...string) ([]Run, error) {
	return s.store.ListRunsByPromptForOwner(ctx, ownerID, promptID, correlationID...)
}

// RunExecuted returns the frozen prompt definition for an owner-scoped run.
func (s *Service) RunExecuted(ctx context.Context, ownerID, runID string) (Executed, error) {
	if _, err := s.runForOwner(ctx, ownerID, runID); err != nil {
		return Executed{}, err
	}
	return LoadFromRun(s.runsDir, runID)
}

// RunGet returns a single run by run_id, owner-scoped via the run's owner_email
// (works after the run's prompt has been deleted).
func (s *Service) RunGet(ctx context.Context, ownerID, runID string) (Run, error) {
	return s.runForOwner(ctx, ownerID, runID)
}

// RunOutput returns up to limit lines of a run's output.jsonl, starting at
// 1-based offset (same line-slice semantics as sandbox.Read). Owner-scoped via
// the run's owner_email; survives a deleted prompt.
func (s *Service) RunOutput(ctx context.Context, ownerID, runID string, offset, limit int) (string, error) {
	r, err := s.runForOwner(ctx, ownerID, runID)
	if err != nil {
		return "", err
	}
	return readLines(r.LogPath, offset, limit)
}

// RunCancel signals the in-flight run by run_id. Owner-scoped via the run's
// owner_email. Idempotent: a run that is not in flight is not an error (the
// foreground may race the run's own completion).
func (s *Service) RunCancel(ctx context.Context, ownerID, runID string) error {
	_, _, err := s.cancelRun(ctx, ownerID, runID)
	return err
}

func (s *Service) cancelRun(ctx context.Context, ownerID, runID string) (Run, bool, error) {
	r, err := s.runForOwner(ctx, ownerID, runID)
	if err != nil {
		return Run{}, false, err
	}
	return r, s.runner.Cancel(r.ID), nil
}

// RunDelete removes one owner-scoped run completely. Database rows are removed
// before files; a live run is first signalled through the cancellation seam and
// must be retried after its writer exits.
func (s *Service) RunDelete(ctx context.Context, ownerID, runID string) error {
	r, cancelled, err := s.cancelRun(ctx, ownerID, runID)
	if errors.Is(err, ErrNotFound) {
		// runForOwner deliberately makes foreign and absent runs indistinguishable.
		// Only the truly absent case is idempotent; a foreign row remains not found.
		_, lookupErr := s.store.GetRun(ctx, runID)
		if errors.Is(lookupErr, ErrNotFound) {
			return nil
		}
		if lookupErr != nil {
			return lookupErr
		}
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if cancelled {
		return fmt.Errorf("prompt: run %q is being cancelled; retry run_delete", r.ID)
	}
	if err := s.store.DeleteRun(ctx, r.ID); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.runsDir, r.ID)); err != nil {
		return fmt.Errorf("prompt: remove run directory: %w", err)
	}
	return nil
}

// RunFsList lists the entries under path in a run's durable sandbox. Owner-
// scoped via the run's owner_email; survives a deleted prompt.
func (s *Service) RunFsList(ctx context.Context, ownerID, runID, path string) ([]sandbox.Entry, error) {
	r, err := s.runForOwner(ctx, ownerID, runID)
	if err != nil {
		return nil, err
	}
	entries, err := s.sandbox.List(r.ID, path)
	if err != nil {
		return nil, translateSandboxError(err)
	}
	return entries, nil
}

// RunFsRead returns up to limit lines of the file at path in a run's sandbox,
// starting at 1-based offset. Owner-scoped via the run's owner_email; survives a
// deleted prompt.
func (s *Service) RunFsRead(ctx context.Context, ownerID, runID, path string, offset, limit int) (string, error) {
	r, err := s.runForOwner(ctx, ownerID, runID)
	if err != nil {
		return "", err
	}
	contents, err := s.sandbox.Read(r.ID, path, offset, limit)
	if err != nil {
		return "", translateSandboxError(err)
	}
	return contents, nil
}

func translateSandboxError(err error) error {
	switch {
	case errors.Is(err, sandbox.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case errors.Is(err, sandbox.ErrPathEscape):
		return fmt.Errorf("%w: %v", ErrValidation, err)
	default:
		return err
	}
}

// SetTrigger attaches one canonical filter to the owner's prompt. Loading the
// prompt first enforces ownership (ErrNotFound if not owned).
func (s *Service) SetTrigger(ctx context.Context, ownerID, id, filter string) (Trigger, error) {
	if _, err := s.store.GetPrompt(ctx, ownerID, id); err != nil {
		return Trigger{}, err
	}
	source, err := validateTrigger(filter)
	if err != nil {
		return Trigger{}, err
	}
	t := Trigger{
		PromptID:  id,
		Source:    source,
		Filter:    filter,
		CreatedAt: s.nowStr(),
	}
	if err := s.store.SetTrigger(ctx, t); err != nil {
		return Trigger{}, err
	}
	return t, nil
}

// ClearTrigger removes one canonical filter from the owner's prompt. Loading
// the prompt first enforces ownership. A missing binding returns ErrNotFound.
func (s *Service) ClearTrigger(ctx context.Context, ownerID, id, filter string) error {
	if _, err := s.store.GetPrompt(ctx, ownerID, id); err != nil {
		return err
	}
	return s.store.ClearTrigger(ctx, id, filter)
}

// PromptsForEvent returns every prompt id whose binding matches (source, evType)
// — the event→prompts fan-out the consumer runs on each arrival. It is NOT
// owner-scoped: a fired event has no caller identity, and the trigger linkage
// (set by the owner) is the authority. A thin passthrough to the store.
func (s *Service) PromptsForEvent(ctx context.Context, source, key string) ([]string, error) {
	return s.store.PromptsForEvent(ctx, source, key)
}

// TriggerSources returns the static known-producer set (A12), for set_trigger
// validation.
func (s *Service) TriggerSources() []string {
	return triggerSources()
}
