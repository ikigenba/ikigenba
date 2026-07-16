// Package db owns the repos service's embedded SQLite migrations.
package db

import (
	"embed"
	"fmt"
	"strings"

	appdb "appkit/db"
	"eventplane/consumer"
	"eventplane/outbox"
)

// migrationsFS is the service's complete, forward-only migration set.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS is the complete migration filesystem handed to the appkit chassis.
var FS = migrationsFS

// Migrations loads the ordered embedded migration set and guards the copied
// eventplane DDL against drift.
func Migrations() ([]appdb.Migration, error) {
	migrations, err := appdb.LoadMigrations(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("repos db: no embedded migrations")
	}
	if err := guardCopiedSchema(migrations, "outbox", outbox.SchemaSQL); err != nil {
		return nil, err
	}
	if err := guardCopiedSchema(migrations, "feed_offset", consumer.SchemaSQL); err != nil {
		return nil, err
	}
	return migrations, nil
}

func guardCopiedSchema(migrations []appdb.Migration, name, schema string) error {
	for i := len(migrations) - 1; i >= 0; i-- {
		migration := migrations[i]
		if !strings.Contains(migration.Name, name) {
			continue
		}
		if !strings.Contains(migration.SQL, schema) {
			return fmt.Errorf("repos db: newest %s migration %s does not contain eventplane schema verbatim", name, migration.Name)
		}
		return nil
	}
	return fmt.Errorf("repos db: no embedded %s migration", name)
}
