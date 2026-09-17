// Package backup provides backup, restore, replication and timer operations.
package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const (
	litestreamPath = "etc/litestream.yml"
	maximumSeconds = uint64(math.MaxInt64 / int64(time.Second))
)

type replicationSettings struct {
	region           string
	prefix           string
	databaseInterval string
	walInterval      string
}

type databaseService struct {
	name     string
	database apps.Database
}

// DatabaseFiles returns every service-relative path owned by SQLite and
// Litestream for a database declaration.
func DatabaseFiles(database apps.Database) []string {
	return []string{
		database.Path,
		database.Path + "-wal",
		database.Path + "-shm",
		path.Join(path.Dir(database.Path), "."+path.Base(database.Path)+"-litestream"),
	}
}

// Regenerate derives the shared Litestream configuration from host settings
// and discovered service manifests.
func Regenerate(ctx context.Context, env host.Env, store config.Store) (bool, error) {
	settings, services, err := regenerationInputs(ctx, env.Root, store)
	if err != nil {
		return false, err
	}
	return updateConfiguration(ctx, env.Root, renderConfiguration(env.Root, settings, services))
}

// SetupReplication generates the shared Litestream configuration and enables
// its service, restarting it only when the configuration changed.
func SetupReplication(ctx context.Context, env host.Env, store config.Store) error {
	changed, err := Regenerate(ctx, env, store)
	if err != nil {
		return err
	}
	if env.Execute == nil {
		return errors.New("setup replication: host execution is not configured")
	}
	if err := executeReplicationCommand(ctx, env, "enable litestream.service", "enable"); err != nil {
		return err
	}
	action := "start"
	if changed {
		action = "restart"
	}
	return executeReplicationCommand(ctx, env, action+" litestream.service", action)
}

func executeReplicationCommand(ctx context.Context, env host.Env, label, action string) error {
	result, err := env.Execute(ctx, host.Command{
		Name: "systemctl",
		Args: []string{action, "litestream.service"},
	})
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return err
		}
		return &host.CommandError{Label: label, Result: result, Err: err}
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result}
	}
	return nil
}

func regenerationInputs(ctx context.Context, root string, store config.Store) (replicationSettings, []databaseService, error) {
	if err := ctx.Err(); err != nil {
		return replicationSettings{}, nil, fmt.Errorf("regenerate litestream configuration: %w", err)
	}

	settings, err := readReplicationSettings(store)
	if err != nil {
		return replicationSettings{}, nil, err
	}
	services, err := discoverDatabases(root)
	if err != nil {
		return replicationSettings{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return replicationSettings{}, nil, fmt.Errorf("regenerate litestream configuration: %w", err)
	}
	return settings, services, nil
}

func updateConfiguration(ctx context.Context, root string, contents []byte) (bool, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return false, fmt.Errorf("read /%s: %w", litestreamPath, err)
	}
	defer func() { _ = filesystem.Close() }()

	existing, err := filesystem.ReadFile(litestreamPath)
	if err == nil && bytes.Equal(existing, contents) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read /%s: %w", litestreamPath, err)
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("regenerate litestream configuration: %w", err)
	}
	if err := publishConfiguration(filesystem, contents); err != nil {
		return false, fmt.Errorf("write /%s: %w", litestreamPath, err)
	}
	return true, nil
}

func readReplicationSettings(store config.Store) (replicationSettings, error) {
	var settings replicationSettings
	var err error
	settings.region, err = requiredSetting(store, "aws.region")
	if err != nil {
		return replicationSettings{}, err
	}
	settings.prefix, err = requiredSetting(store, "backup.s3_uri")
	if err != nil {
		return replicationSettings{}, err
	}
	if err := validateS3Prefix(settings.prefix); err != nil {
		return replicationSettings{}, fmt.Errorf("backup.s3_uri: %w", err)
	}
	settings.databaseInterval, err = replicationPeriod(store, "backup.service_db_seconds")
	if err != nil {
		return replicationSettings{}, err
	}
	settings.walInterval, err = replicationPeriod(store, "backup.service_wal_seconds")
	if err != nil {
		return replicationSettings{}, err
	}
	return settings, nil
}

func requiredSetting(store config.Store, key string) (string, error) {
	value, err := store.Get(key)
	if err != nil {
		if errors.Is(err, config.ErrNotSet) {
			return "", fmt.Errorf("%s not set", key)
		}
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	if value == "" {
		return "", fmt.Errorf("%s not set", key)
	}
	return value, nil
}

func replicationPeriod(store config.Store, key string) (string, error) {
	value, err := store.Get(key)
	if err != nil {
		if errors.Is(err, config.ErrNotSet) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	if value == "" || value == "0" {
		return "", nil
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return "", fmt.Errorf("%s: must not exceed %d whole seconds", key, maximumSeconds)
		}
		return "", fmt.Errorf("%s: must be decimal whole seconds", key)
	}
	if seconds > maximumSeconds {
		return "", fmt.Errorf("%s: must not exceed %d whole seconds", key, maximumSeconds)
	}
	return strconv.FormatUint(seconds, 10) + "s", nil
}

func validateS3Prefix(prefix string) error {
	parsed, err := url.Parse(prefix)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "s3" || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must be an absolute s3:// URI with a nonempty bucket and no query or fragment")
	}
	for _, component := range strings.Split(parsed.Path, "/") {
		if component == "." || component == ".." {
			return errors.New("must not contain a . or .. object-prefix component")
		}
	}
	return nil
}

func discoverDatabases(root string) ([]databaseService, error) {
	services, err := apps.Discover(root)
	if err != nil {
		return nil, fmt.Errorf("discover databases: %w", err)
	}
	databases := make([]databaseService, 0, len(services))
	for _, service := range services {
		if service.ManifestError != nil {
			return nil, fmt.Errorf("service %q manifest: %w", service.Name, service.ManifestError)
		}
		if service.Manifest == nil || service.Manifest.Database == nil {
			continue
		}
		if invalidDatabaseServiceName(service.Name) {
			return nil, fmt.Errorf("service %q cannot own a replicated database", service.Name)
		}
		databases = append(databases, databaseService{name: service.Name, database: *service.Manifest.Database})
	}
	return databases, nil
}

func invalidDatabaseServiceName(name string) bool {
	return name == "" || name == "." || name == ".." || name == "host" || name == "deploy" || strings.ContainsAny(name, "/\x00")
}

func renderConfiguration(root string, settings replicationSettings, services []databaseService) []byte {
	var configuration strings.Builder
	configuration.WriteString("region: ")
	configuration.WriteString(yamlString(settings.region))
	configuration.WriteByte('\n')
	if settings.databaseInterval != "" {
		configuration.WriteString("snapshot:\n  interval: ")
		configuration.WriteString(settings.databaseInterval)
		configuration.WriteByte('\n')
	}
	if settings.walInterval != "" {
		configuration.WriteString("sync-interval: ")
		configuration.WriteString(settings.walInterval)
		configuration.WriteByte('\n')
	}
	configuration.WriteString("socket:\n  enabled: true\n  path: ")
	configuration.WriteString(yamlString(path.Join(root, "/var/run/litestream.sock")))
	configuration.WriteString("\n  permissions: 0600\n")
	configuration.WriteString("retention:\n  enabled: false\n")
	if len(services) == 0 {
		configuration.WriteString("dbs: []\n")
		return []byte(configuration.String())
	}
	configuration.WriteString("dbs:\n")
	for _, service := range services {
		databasePath := path.Join("/opt", service.name, service.database.Path)
		if root != "/" {
			databasePath = path.Join(root, databasePath)
		}
		configuration.WriteString("  - path: ")
		configuration.WriteString(yamlString(databasePath))
		configuration.WriteString("\n    replicas:\n      - url: ")
		configuration.WriteString(yamlString(strings.TrimSuffix(settings.prefix, "/") + "/" + service.name + "/"))
		configuration.WriteByte('\n')
	}
	return []byte(configuration.String())
}

func yamlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func publishConfiguration(filesystem *os.Root, contents []byte) error {
	const directory = "etc"
	var temporary string
	var file *os.File
	for range 100 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return err
		}
		temporary = path.Join(directory, fmt.Sprintf(".opsctl-litestream-%x", suffix))
		var err error
		file, err = filesystem.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if file == nil {
		return errors.New("create unique Litestream configuration staging file")
	}
	defer func() { _ = filesystem.Remove(temporary) }()
	if _, err := file.Write(contents); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Chmod(fs.FileMode(0o600)); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	return filesystem.Rename(temporary, litestreamPath)
}
