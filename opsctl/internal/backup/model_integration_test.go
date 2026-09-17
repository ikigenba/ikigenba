package backup_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestBackupModelOwnsCrossDocumentOperationsAndDependencies(t *testing.T) {
	// R-JJ94-707K
	apis := []struct {
		name string
		got  reflect.Type
		want reflect.Type
	}{
		{name: "Files", got: reflect.TypeOf(backup.Files), want: reflect.TypeFor[func(context.Context, host.Env, cloud.Env, config.Store, string) ([]backup.FileResult, error)]()},
		{name: "HostBackup", got: reflect.TypeOf(backup.HostBackup), want: reflect.TypeFor[func(context.Context, host.Env, cloud.Env, config.Store) (backup.FileResult, error)]()},
		{name: "Restore", got: reflect.TypeOf(backup.Restore), want: reflect.TypeFor[func(context.Context, host.Env, cloud.Env, config.Store, string, *time.Time, backup.NginxRegenerator) (backup.RestoreReport, error)]()},
		{name: "Retire", got: reflect.TypeOf(backup.Retire), want: reflect.TypeFor[func(context.Context, host.Env, cloud.Env, config.Store) (backup.RetireResult, error)]()},
		{name: "Regenerate", got: reflect.TypeOf(backup.Regenerate), want: reflect.TypeFor[func(context.Context, host.Env, config.Store) (bool, error)]()},
		{name: "SetupReplication", got: reflect.TypeOf(backup.SetupReplication), want: reflect.TypeFor[func(context.Context, host.Env, config.Store) error]()},
		{name: "SetupTimers", got: reflect.TypeOf(backup.SetupTimers), want: reflect.TypeFor[func(context.Context, host.Env, config.Store) error]()},
		{name: "DatabaseFiles", got: reflect.TypeOf(backup.DatabaseFiles), want: reflect.TypeFor[func(apps.Database) []string]()},
	}
	for _, api := range apis {
		if api.got != api.want {
			t.Errorf("backup.%s type = %v, want %v", api.name, api.got, api.want)
		}
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowedInternal := map[string]bool{"apps": true, "cloud": true, "config": true, "host": true}
	forbiddenImplementation := map[string]bool{"database/sql": true, "net/http": true, "os/exec": true}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Clean(entry.Name()), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if file.Name.Name != "backup" {
			t.Errorf("%s package = %q, want backup", entry.Name(), file.Name.Name)
		}
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Fatal(unquoteErr)
			}
			const internalPrefix = "github.com/ikigenba/ikigenba/opsctl/internal/"
			if strings.HasPrefix(importPath, internalPrefix) {
				dependency := strings.Split(strings.TrimPrefix(importPath, internalPrefix), "/")[0]
				if !allowedInternal[dependency] {
					t.Errorf("%s imports forbidden internal dependency %q", entry.Name(), importPath)
				}
			}
			if forbiddenImplementation[importPath] {
				t.Errorf("%s imports %q; backup must orchestrate host Litestream rather than install it or implement SQLite replication", entry.Name(), importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				t.Fatal(unquoteErr)
			}
			for _, installer := range []string{"apt", "apt-get", "dnf", "yum", "apk", "curl", "wget"} {
				if value == installer {
					t.Errorf("%s names installer command %q; Litestream installation is outside package backup", entry.Name(), value)
				}
			}
			return true
		})
	}
}
