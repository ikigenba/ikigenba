package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSalesFunnelMigrationPreservesRowsAndRemapsLegacyValues(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	base, err := migrationsFS.ReadFile("migrations/002_crm.sql")
	if err != nil {
		t.Fatalf("read base CRM migration: %v", err)
	}
	if _, err := conn.Exec(string(base)); err != nil {
		t.Fatalf("apply base CRM migration: %v", err)
	}
	seedLegacySalesFunnel(t, conn)

	migration, err := migrationsFS.ReadFile("migrations/20260817170129_sales_funnel_vocabularies.sql")
	if err != nil {
		t.Fatalf("read sales-funnel migration: %v", err)
	}
	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("begin migration transaction: %v", err)
	}
	if _, err := tx.Exec(string(migration)); err != nil {
		tx.Rollback()
		t.Fatalf("apply sales-funnel migration: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit sales-funnel migration: %v", err)
	}

	wantCounts := map[string]int{
		"contacts": 4, "deals": 6,
		"contact_emails": 4, "contact_phones": 4, "contact_tags": 4,
		"deal_contacts": 6, "interactions": 1, "tasks": 1,
	}
	for table, want := range wantCounts {
		var got int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		// R-9PGI-7Z2T
		if got != want {
			t.Fatalf("%s rows = %d, want %d", table, got, want)
		}
	}

	assertValueCounts(t, conn, "contacts", "lifecycle", map[string]int{"prospect": 3, "customer": 1})
	assertValueCounts(t, conn, "deals", "stage", map[string]int{
		"contacted": 1, "interested": 1, "proposal": 2, "won": 1, "lost": 1,
	})
	rows, err := conn.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign key check: %v", err)
	}
	defer rows.Close()
	// R-9PGI-7Z2T
	if rows.Next() {
		var table, parent string
		var rowID, fkID int
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			t.Fatalf("scan foreign key violation: %v", err)
		}
		t.Fatalf("foreign key violation after migration: table=%s row=%d parent=%s fk=%d", table, rowID, parent, fkID)
	}
}

func seedLegacySalesFunnel(t *testing.T, conn *sql.DB) {
	t.Helper()
	const stamp = "2026-08-17T12:00:00Z"
	if _, err := conn.Exec(`INSERT INTO organizations
		(id, name, created_at, updated_at) VALUES ('org-1', 'Legacy Org', ?, ?)`, stamp, stamp); err != nil {
		t.Fatalf("seed organization: %v", err)
	}
	for i, lifecycle := range []string{"subscriber", "lead", "opportunity", "customer"} {
		id := "contact-" + lifecycle
		deleted := any(nil)
		if i == 2 {
			deleted = stamp
		}
		if _, err := conn.Exec(`INSERT INTO contacts
			(id, display_name, org_id, lifecycle, created_at, updated_at, deleted_at)
			VALUES (?, ?, 'org-1', ?, ?, ?, ?)`, id, lifecycle, lifecycle, stamp, stamp, deleted); err != nil {
			t.Fatalf("seed contact %s: %v", lifecycle, err)
		}
		if _, err := conn.Exec(`INSERT INTO contact_emails
			(id, contact_id, email, is_primary, created_at, deleted_at) VALUES (?, ?, ?, 1, ?, ?)`,
			"email-"+lifecycle, id, lifecycle+"@example.test", stamp, deleted); err != nil {
			t.Fatalf("seed email %s: %v", lifecycle, err)
		}
		if _, err := conn.Exec(`INSERT INTO contact_phones
			(id, contact_id, phone, is_primary, created_at, deleted_at) VALUES (?, ?, ?, 1, ?, ?)`,
			"phone-"+lifecycle, id, "+1415555012"+string(rune('3'+i)), stamp, deleted); err != nil {
			t.Fatalf("seed phone %s: %v", lifecycle, err)
		}
		if _, err := conn.Exec(`INSERT INTO contact_tags
			(id, contact_id, tag, created_at, deleted_at) VALUES (?, ?, ?, ?, ?)`,
			"tag-"+lifecycle, id, "tag-"+lifecycle, stamp, deleted); err != nil {
			t.Fatalf("seed tag %s: %v", lifecycle, err)
		}
	}

	stages := []string{"lead", "qualified", "proposal", "negotiation", "won", "lost"}
	contacts := []string{"subscriber", "lead", "opportunity", "customer"}
	for i, stage := range stages {
		id := "deal-" + stage
		deleted := any(nil)
		if stage == "lost" {
			deleted = stamp
		}
		if _, err := conn.Exec(`INSERT INTO deals
			(id, name, org_id, stage, currency, created_at, updated_at, deleted_at)
			VALUES (?, ?, 'org-1', ?, 'USD', ?, ?, ?)`, id, stage, stage, stamp, stamp, deleted); err != nil {
			t.Fatalf("seed deal %s: %v", stage, err)
		}
		if _, err := conn.Exec(`INSERT INTO deal_contacts
			(id, deal_id, contact_id, role, created_at, deleted_at) VALUES (?, ?, ?, 'participant', ?, ?)`,
			"participant-"+stage, id, "contact-"+contacts[i%len(contacts)], stamp, deleted); err != nil {
			t.Fatalf("seed participant %s: %v", stage, err)
		}
	}
	if _, err := conn.Exec(`INSERT INTO interactions
		(id, kind, body, occurred_at, contact_id, deal_id, created_at, updated_at)
		VALUES ('interaction-1', 'note', 'legacy', ?, 'contact-lead', 'deal-lead', ?, ?)`, stamp, stamp, stamp); err != nil {
		t.Fatalf("seed interaction: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO tasks
		(id, title, status, contact_id, deal_id, created_at, updated_at)
		VALUES ('task-1', 'legacy', 'open', 'contact-customer', 'deal-won', ?, ?)`, stamp, stamp); err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func assertValueCounts(t *testing.T, conn *sql.DB, table, column string, want map[string]int) {
	t.Helper()
	rows, err := conn.Query(`SELECT ` + column + `, COUNT(*) FROM ` + table + ` GROUP BY ` + column)
	if err != nil {
		t.Fatalf("group %s.%s: %v", table, column, err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			t.Fatalf("scan %s.%s: %v", table, column, err)
		}
		got[value] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s.%s: %v", table, column, err)
	}
	// R-9PGI-7Z2T
	if len(got) != len(want) {
		t.Fatalf("%s.%s value counts = %v, want %v", table, column, got, want)
	}
	for value, count := range want {
		// R-9PGI-7Z2T
		if got[value] != count {
			t.Fatalf("%s.%s %q count = %d, want %d (all=%v)", table, column, value, got[value], count, got)
		}
	}
}
