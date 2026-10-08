package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"sort"
)

// Status writes the applied, pending, and unknown migration versions without changing the database.
func Status(ctx context.Context, cfg Config, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := validatePath(cfg.Path)
	if err != nil {
		return err
	}
	migrations, err := loadMigrations(cfg.Migrations)
	if err != nil {
		return err
	}
	exists, err := inspectPath(path)
	if err != nil {
		return err
	}
	applied := make(map[int]string)
	if exists {
		uri := url.URL{Scheme: "file", Path: path}
		query := url.Values{"mode": {"ro"}}
		uri.RawQuery = query.Encode()
		conn, err := sql.Open("sqlite", uri.String())
		if err != nil {
			return fmt.Errorf("open database status: %w", err)
		}
		defer func() { _ = conn.Close() }()
		applied, err = appliedVersions(ctx, conn)
		if err != nil {
			return err
		}
	}
	versions := make([]int, 0, len(migrations)+len(applied))
	for _, item := range migrations {
		versions = append(versions, item.version)
	}
	for version := range applied {
		if version < 1 || version > len(migrations) {
			versions = append(versions, version)
		}
	}
	sort.Ints(versions)
	for _, version := range versions {
		timestamp, exists := applied[version]
		var line string
		switch {
		case version < 1 || version > len(migrations):
			line = fmt.Sprintf("%04d unknown %s\n", version, timestamp)
		case exists:
			line = fmt.Sprintf("%04d applied %s\n", version, timestamp)
		default:
			line = fmt.Sprintf("%04d pending\n", version)
		}
		if _, err := io.WriteString(w, line); err != nil {
			return fmt.Errorf("write database status: %w", err)
		}
	}
	return nil
}
